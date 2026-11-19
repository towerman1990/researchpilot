package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	provider "researchpilot/internal/model"
)

func (a *apiServer) revokeModel(w http.ResponseWriter, r *http.Request, tenant string) {
	// Lock the same user row as config creation to serialize save/revoke.
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback()
	var user string
	if err = tx.QueryRowContext(r.Context(), "SELECT id FROM users WHERE id=? FOR UPDATE", tenant).Scan(&user); err != nil {
		internal(w, err)
		return
	}
	if _, err = tx.ExecContext(r.Context(), "UPDATE model_configs SET revoked=true WHERE tenant_id=? AND is_trial=false", tenant); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		internal(w, err)
		return
	}
	reply(w, 200, map[string]bool{"configured": false, "revoked": true})
}

func (a *apiServer) probeModel(w http.ResponseWriter, r *http.Request, tenant string) {
	if !a.allow(w, r, "model-probe", tenant, quota("RATE_MODEL_PROBE", 3)) {
		return
	}
	var version uint32
	if err := a.db.QueryRowContext(r.Context(), "SELECT version FROM model_configs WHERE tenant_id=? AND revoked=false AND is_trial=false ORDER BY version DESC LIMIT 1", tenant).Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 409, "model_not_configured")
		} else {
			internal(w, err)
		}
		return
	}
	config, err := loadModel(r.Context(), a.db, tenant, version)
	if err != nil {
		problem(w, 503, "model_configuration_unavailable")
		return
	}
	client := modelHTTPClient()
	if a.modelHTTP != nil {
		client = a.modelHTTP
	}
	defer client.CloseIdleConnections()
	p, err := providerFor(client, config)
	if err != nil {
		problem(w, 422, "invalid_model_config")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	capabilities := map[string]bool{}
	failures := map[string]string{}
	run := func(name string, req provider.Request, stream bool, check func(provider.Response) bool) {
		callCtx, stop := context.WithTimeout(ctx, 10*time.Second)
		defer stop()
		var out provider.Response
		var callErr error
		if stream {
			out, callErr = p.Stream(callCtx, req, nil)
		} else {
			out, callErr = p.Generate(callCtx, req)
		}
		capabilities[name] = callErr == nil && check(out)
		if !capabilities[name] {
			failures[name] = "unsupported_or_unavailable"
			var status *provider.HTTPError
			if errors.As(callErr, &status) {
				failures[name] = status.Error()
			}
		}
	}
	basic := modelRequest(config, "Return JSON with ok=true.")
	basic.MaxTokens = 128
	basic.ResponseFormat = nil
	run("chat", basic, false, func(out provider.Response) bool { return out.Text != "" })
	structured := basic
	structured.ResponseFormat = map[string]string{"type": "json_object"}
	run("json_object", structured, false, func(out provider.Response) bool {
		var obj map[string]any
		return json.Unmarshal([]byte(out.Text), &obj) == nil && obj != nil
	})
	native := basic
	native.ResponseFormat = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "probe", "strict": true, "schema": map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]string{"type": "boolean"}}, "required": []string{"ok"}, "additionalProperties": false}}}
	run("json_schema", native, false, func(out provider.Response) bool {
		var obj struct {
			OK bool `json:"ok"`
		}
		return json.Unmarshal([]byte(out.Text), &obj) == nil && obj.OK
	})
	tool := basic
	tool.Messages = []provider.Message{{Role: "user", Content: "Call probe with value=ok."}}
	tool.Tools = []provider.Tool{{Type: "function", Function: provider.Function{Name: "probe", Description: "Capability probe only; never executed", Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)}}}
	tool.ToolChoice = map[string]any{"type": "function", "function": map[string]string{"name": "probe"}}
	validTool := func(out provider.Response) bool {
		if len(out.Calls) != 1 || out.Calls[0].Function.Name != "probe" {
			return false
		}
		var args struct {
			Value string `json:"value"`
		}
		return json.Unmarshal([]byte(out.Calls[0].Function.Arguments), &args) == nil && args.Value == "ok"
	}
	run("tools", tool, false, validTool)
	run("stream_tools", tool, true, validTool)
	// Probes never execute requested tools or expose upstream bodies, keys or raw text.
	reply(w, 200, map[string]any{"version": version, "capabilities": capabilities, "errors": failures})
}

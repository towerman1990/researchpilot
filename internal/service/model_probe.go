package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	provider "researchpilot/internal/model"
)

func (s Models) Probe(ctx context.Context, tenant string) (map[string]any, error) {
	version, err := s.Store.ActiveModelVersion(ctx, tenant)
	if err != nil {
		return nil, err
	}
	if version == 0 {
		return nil, ErrModelNotConfigured
	}
	config, err := LoadModel(ctx, s.Store, tenant, version)
	if err != nil {
		return nil, ErrModelUnavailable
	}
	if s.HTTPClient == nil {
		return nil, ErrModelUnavailable
	}
	p, err := ProviderFor(s.HTTPClient, config)
	if err != nil {
		return nil, ErrInvalidModelConfig
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
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
	basic := ModelRequest(config, "Return JSON with ok=true.")
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
	return map[string]any{"version": version, "capabilities": capabilities, "errors": failures}, nil
}

package service

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"researchpilot/internal/common/identity"
	"researchpilot/internal/persistence"
	"researchpilot/internal/repository"
)

type probeTransport func(*http.Request) (*http.Response, error)

func (f probeTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type probeClientTransport struct {
	http.RoundTripper
	idleClosed bool
}

func (t *probeClientTransport) CloseIdleConnections() { t.idleClosed = true }

func TestModelProbeUsesRedactedCapabilitiesIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" || os.Getenv("MODEL_MASTER_KEY_FILE") == "" {
		t.Skip("MySQL and model master key are required")
	}
	ctx := t.Context()
	db, err := persistence.OpenMySQL(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := repository.New(db)
	if err != nil {
		t.Fatal(err)
	}
	tenant := identity.New(16)
	if err := store.CreateUser(ctx, tenant, tenant+"@example.test", []byte("unused")); err != nil {
		t.Fatal(err)
	}
	transport := probeTransport(func(req *http.Request) (*http.Response, error) {
		var input struct {
			Stream bool
			Tools  []any
		}
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		body := `{"choices":[{"message":{"content":"{\"ok\":true}"},"finish_reason":"stop"}]}`
		if len(input.Tools) > 0 {
			body = `{"choices":[{"message":{"tool_calls":[{"id":"probe-id","type":"function","function":{"name":"probe","arguments":"{\"value\":\"ok\"}"}}]},"finish_reason":"tool_calls"}]}`
		}
		if input.Stream {
			body = "data: " + `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"probe-id","type":"function","function":{"name":"probe","arguments":"{\"value\":\"ok\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	shared := &probeClientTransport{RoundTripper: transport}
	models := Models{Store: store, HTTPClient: &http.Client{Transport: shared}}
	const key = "fixture-model-key"
	if _, err := models.Save(ctx, tenant, ModelInput{BaseURL: "https://api.deepseek.com", Model: "fixture", APIKey: key}); err != nil {
		t.Fatal(err)
	}
	result, err := models.Probe(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if shared.idleClosed {
		t.Fatal("probe closed connections owned by the injected client")
	}
	capabilities, ok := result["capabilities"].(map[string]bool)
	if !ok || len(capabilities) != 5 {
		t.Fatal("missing probe capabilities")
	}
	for name, supported := range capabilities {
		if !supported {
			t.Error("probe failed", name)
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), key) {
		t.Fatal("probe leaked key")
	}
}

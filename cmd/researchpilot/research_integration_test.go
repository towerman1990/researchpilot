package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResearchIntegration(t *testing.T) {
	db := integrationDB(t)
	keyPath := os.Getenv("MODEL_MASTER_KEY_FILE")
	if keyPath == "" {
		t.Skip("MODEL_MASTER_KEY_FILE not set")
	}
	d := newTestTask(t, db)
	api := apiServer{db: db}
	request := httptest.NewRequest("PUT", "/api/v1/model-config", strings.NewReader(`{"base_url":"https://api.deepseek.com","model":"deepseek-flash","api_key":"test-only-api-key"}`))
	response := httptest.NewRecorder()
	api.modelConfig(response, request, d.Tenant)
	if response.Code != 200 {
		t.Fatalf("config status=%d body=%s", response.Code, response.Body.String())
	}
	var encrypted []byte
	if err := db.QueryRow("SELECT key_cipher FROM model_configs WHERE tenant_id=?", d.Tenant).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encrypted), "test-only-api-key") {
		t.Fatal("API key stored in plaintext")
	}
	if _, err := db.Exec("UPDATE research_tasks SET model_version=1 WHERE id=?", d.Task); err != nil {
		t.Fatal(err)
	}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := "<html><body>Official fixture documents durable research execution.</body></html>"
		if req.URL.Host == "api.deepseek.com" {
			raw, _ := io.ReadAll(req.Body)
			content := `{"steps":[{"tool":"fetch_page","url":"https://example.com/docs"}]}`
			if strings.Contains(string(raw), "findings") {
				var request struct{ Messages []struct{ Content string } }
				if err := json.Unmarshal(raw, &request); err != nil {
					t.Fatal(err)
				}
				ids := regexp.MustCompile(`"source_id":"([a-f0-9]{32})"`).FindStringSubmatch(request.Messages[1].Content)
				if len(ids) != 2 {
					t.Fatal("model prompt lacks persisted evidence")
				}
				encoded, _ := json.Marshal(map[string]any{"findings": []map[string]any{{"text": "The evidence supports durable execution.", "sources": []string{ids[1]}}}, "limitations": []string{"Fixture evidence."}})
				content = string(encoded)
			}
			encoded, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": content}, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 100}})
			body = string(encoded)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	client := &http.Client{Transport: transport}
	runner := runner{db: db, modelHTTP: client, toolsHTTP: client}
	state, ok, err := runner.claim(context.Background(), d)
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err = runner.executeDurable(context.Background(), d, state); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = db.QueryRow("SELECT status FROM research_tasks WHERE id=?", d.Task).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "SUCCEEDED" {
		t.Fatal(status)
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM report_citations WHERE task_id=?", d.Task).Scan(&count); err != nil || count != 1 {
		t.Fatalf("citations=%d err=%v", count, err)
	}
	var used, reserved int
	if err = db.QueryRow("SELECT tokens_used,tokens_reserved FROM research_tasks WHERE id=?", d.Task).Scan(&used, &reserved); err != nil || used != 200 || reserved != 0 {
		t.Fatalf("accounting=%d %d %v", used, reserved, err)
	}
}

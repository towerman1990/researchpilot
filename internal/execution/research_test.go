package execution

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	researchtools "researchpilot/internal/tools/research"
)

func TestDNSPolicyFailureChargesNoTokensIntegration(t *testing.T) {
	db := integrationDB(t)
	e, _ := claimTest(t, db)
	if os.Getenv("MODEL_MASTER_KEY_FILE") == "" {
		t.Skip("MODEL_MASTER_KEY_FILE not set")
	}
	a := apiServer{db: db}
	req := httptest.NewRequest("PUT", "/model-config", strings.NewReader(`{"base_url":"https://api.deepseek.com","model":"fixture","api_key":"fixture-model-key"}`))
	rec := httptest.NewRecorder()
	a.modelConfig(rec, req, e.Tenant)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	config, err := loadModel(context.Background(), db, e.Tenant, 1)
	if err != nil {
		t.Fatal(err)
	}
	e.Runner.modelHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("private_address_forbidden") })}
	ctx := context.WithValue(context.Background(), modelCallKey{}, "preflight")
	_, err = e.generate(ctx, config, "fixture")
	if err == nil || err.Error() != "model_dns_policy_denied" {
		t.Fatal(err)
	}
	var used, reserved uint32
	var code, status string
	var charged uint32
	if err = db.QueryRow("SELECT tokens_used,tokens_reserved FROM research_tasks WHERE id=?", e.Task).Scan(&used, &reserved); err != nil || used != 0 || reserved != 0 {
		t.Fatal(used, reserved, err)
	}
	if err = db.QueryRow("SELECT error_code,status,charged_tokens FROM model_attempts WHERE task_id=?", e.Task).Scan(&code, &status, &charged); err != nil || code != "model_dns_policy_denied" || status != "ACTUAL" || charged != 0 {
		t.Fatal(code, status, charged, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResearchIntegration(t *testing.T) { researchFixture(t, true, false) }

func TestInsufficientEvidenceIntegration(t *testing.T) { researchFixture(t, false, false) }

func TestCoverageJSONRepairIntegration(t *testing.T) { researchFixture(t, true, true) }

func researchFixture(t *testing.T, sufficient bool, malformed bool) {
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
			if strings.Contains(string(raw), "Assess evidence coverage") {
				coverage, _ := json.Marshal(map[string]any{"sufficient": sufficient, "gaps": []string{"Missing direct support"}})
				content = string(coverage)
				if malformed {
					content = `{"sufficient":true,"gaps":["A "quoted" note"]}`
				}
			}
			if strings.Contains(string(raw), "Repair coverage JSON") {
				content = `{"sufficient":true,"gaps":["A quoted note"]}`
			}
			encoded, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": content}, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 100}})
			body = string(encoded)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	client := &http.Client{Transport: transport}
	worker := Runner{store: executionStore(t, db), modelHTTP: client, registry: researchtools.NewRegistry(client)}
	state, ok, err := worker.claim(context.Background(), d)
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err = worker.executeDurable(context.Background(), d, state); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = db.QueryRow("SELECT status FROM research_tasks WHERE id=?", d.Task).Scan(&status); err != nil {
		t.Fatal(err)
	}
	expectedStatus := "SUCCEEDED"
	expectedUsage := 300
	if malformed {
		expectedUsage += 100
	}
	if !sufficient {
		expectedStatus = "PARTIAL"
		expectedUsage = 600
	}
	if status != expectedStatus {
		t.Fatal(status)
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM report_citations WHERE task_id=?", d.Task).Scan(&count); err != nil || count != 1 {
		t.Fatalf("citations=%d err=%v", count, err)
	}
	var used, reserved int
	if err = db.QueryRow("SELECT tokens_used,tokens_reserved FROM research_tasks WHERE id=?", d.Task).Scan(&used, &reserved); err != nil || used != expectedUsage || reserved != 0 {
		t.Fatalf("accounting=%d %d %v", used, reserved, err)
	}
}

func TestRepeatedURLCannotCrowdOutComplementaryEvidence(t *testing.T) {
	sources := []evidence{{ID: "shutdown", URL: "https://pkg.go.dev/net/http#Server.Shutdown", Excerpt: "Shutdown closes listeners and drains active connections."}}
	for i := 0; i < 6; i++ {
		sources = append(sources, evidence{ID: fmtSeq(uint64(i + 1)), URL: "https://pkg.go.dev/net/http", Excerpt: strings.Repeat("Server headers ", 100) + fmtSeq(uint64(i))})
	}
	selected := promptEvidence(sources, "Compare timeouts and shutdown https://pkg.go.dev/net/http", 16000, 4)
	found := false
	generic := 0
	for _, s := range selected {
		if s.ID == "shutdown" {
			found = true
		}
		if s.URL == "https://pkg.go.dev/net/http" {
			generic++
		}
	}
	if !found || generic != 1 {
		t.Fatal("complementary source lost", selected)
	}
}

func TestStageErrorKeepsDurableFailureClassification(t *testing.T) {
	cause := errors.New("insufficient_evidence")
	_, err := Run(t.Context(), 0, func(context.Context, int) (int, error) {
		return 0, cause
	}, func(context.Context, int) (int, error) {
		t.Error("later stage ran after a fatal error")
		return 0, nil
	})
	if !errors.Is(err, cause) || !permanentFailure(err) || safeFailureCode(err) != cause.Error() {
		t.Fatalf("stage error lost its durable failure code: %v", err)
	}
}

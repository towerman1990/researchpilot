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

	"researchpilot/internal/repository/model"
	taskservice "researchpilot/internal/task"
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

func TestResearchIntegration(t *testing.T) { researchFixture(t, true, false, false) }

func TestInsufficientEvidenceIntegration(t *testing.T) { researchFixture(t, false, false, false) }

func TestCoverageJSONRepairIntegration(t *testing.T) { researchFixture(t, true, true, false) }

func TestPlanReviewIntegration(t *testing.T) { researchFixture(t, true, false, true) }
func researchFixture(t *testing.T, sufficient, malformed, review bool) {
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
	if review {
		actions := taskservice.TaskActions{Store: executionStore(t, db)}
		created, err := actions.Submit(t.Context(), d.Tenant, "plan-review-test", "Reviewable research goal", "", true)
		if err != nil {
			t.Fatal(err)
		}
		d.Task = created.ID
	} else if _, err := db.Exec("UPDATE research_tasks SET model_version=1 WHERE id=?", d.Task); err != nil {
		t.Fatal(err)
	}
	var fetchedURL string
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
		} else {
			fetchedURL = req.URL.String()
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
	if review {
		snapshot, err := worker.store.ReadTask(t.Context(), d.Tenant, d.Task)
		if err != nil || snapshot.Status != "AWAITING_APPROVAL" || snapshot.ReviewPlan == nil || fetchedURL != "" {
			t.Fatalf("expected a durable pause before fetching evidence: %+v %v %q", snapshot, err, fetchedURL)
		}
		if _, claimed, err := worker.claim(t.Context(), d); err != nil || claimed {
			t.Fatalf("unapproved plan accepted a replayed worker delivery: %v %v", claimed, err)
		}
		actions := taskservice.TaskActions{Store: worker.store}
		edited := json.RawMessage(strings.ReplaceAll(*snapshot.ReviewPlan, "/docs", "/edited"))
		if _, err := actions.ApprovePlan(t.Context(), d.Tenant, d.Task, snapshot.Seq-1, edited); !errors.Is(err, taskservice.ErrPlanNotAwaiting) {
			t.Fatalf("stale review was accepted: %v", err)
		}
		foreign := newTestTask(t, db)
		if _, err := actions.ApprovePlan(t.Context(), foreign.Tenant, d.Task, snapshot.Seq, edited); !errors.Is(err, taskservice.ErrTaskNotFound) {
			t.Fatalf("foreign review was accepted: %v", err)
		}
		approved, err := actions.ApprovePlan(t.Context(), d.Tenant, d.Task, snapshot.Seq, edited)
		if err != nil || approved.Status != "QUEUED" {
			t.Fatalf("confirming edit: %+v %v", approved, err)
		}
		var pending int
		if err := db.QueryRow("SELECT COUNT(*) FROM task_outbox WHERE task_id=? AND generation=2", d.Task).Scan(&pending); err != nil || pending != 1 {
			t.Fatalf("approved task missing new outbox generation: %v", err)
		}
		d.Generation++
		state, ok, err = worker.claim(t.Context(), d)
		if err != nil || !ok {
			t.Fatalf("claim approved plan: %v %v", ok, err)
		}
		if err := worker.executeDurable(t.Context(), d, state); err != nil || fetchedURL != "https://example.com/edited" {
			t.Fatalf("approved edit not used: %q %v", fetchedURL, err)
		}
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

func TestReportFollowUpIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := t.Context()
	t.Setenv("TRIAL_MODEL_ENABLED", "false")
	t.Setenv("MODEL_MASTER_KEY_FILE", testMaster(t))
	parent, foreign := newTestTask(t, db), newTestTask(t, db)
	store := executionStore(t, db)
	actions := taskservice.TaskActions{Store: store}
	const key = "follow-up-idempotency"
	const question = "What does the original report mean by fencing?"
	if _, err := actions.Submit(ctx, parent.Tenant, key, question, parent.Task, false); !errors.Is(err, taskservice.ErrReportNotReady) {
		t.Fatalf("follow-up accepted before report: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE research_tasks SET status='PARTIAL' WHERE id=?", parent.Task); err != nil {
		t.Fatal(err)
	}
	const background = "Original finding about lease fencing"
	if _, err := db.ExecContext(ctx, "INSERT INTO research_reports(tenant_id,task_id,markdown) VALUES(?,?,?)", parent.Tenant, parent.Task, background); err != nil {
		t.Fatal(err)
	}
	if _, err := actions.Submit(ctx, foreign.Tenant, key, question, parent.Task, false); !errors.Is(err, taskservice.ErrTaskNotFound) {
		t.Fatalf("foreign report did not return not found: %v", err)
	}
	legacy, err := store.ReadTask(ctx, parent.Tenant, parent.Task)
	if err != nil || legacy.ParentTaskID != nil {
		t.Fatalf("existing task has an unexpected parent: %v", err)
	}
	api := apiServer{db: db}
	req := httptest.NewRequest("PUT", "/model-config", strings.NewReader("{\"base_url\":\"https://api.deepseek.com\",\"model\":\"fixture\",\"api_key\":\"fixture-follow-up-key\"}"))
	rec := httptest.NewRecorder()
	api.modelConfig(rec, req, parent.Tenant)
	if rec.Code != 200 {
		t.Fatalf("fixture model configuration failed: %d", rec.Code)
	}
	child, err := actions.Submit(ctx, parent.Tenant, key, question, parent.Task, false)
	if err != nil || child.ParentTaskID == nil || *child.ParentTaskID != parent.Task || child.Goal != question {
		t.Fatalf("submitted follow-up: %+v %v", child, err)
	}
	replay, err := actions.Submit(ctx, parent.Tenant, key, question, strings.ToUpper(parent.Task), false)
	if err != nil || replay.ID != child.ID {
		t.Fatalf("replayed follow-up created different task: %v", err)
	}
	if _, err := actions.Submit(ctx, parent.Tenant, key, question, "", false); !errors.Is(err, taskservice.ErrIdempotencyConflict) {
		t.Fatalf("goal-only request matched follow-up digest: %v", err)
	}
	runner := Runner{store: store}
	d := delivery{Tenant: parent.Tenant, Task: child.ID, Generation: 1}
	row, claimed, err := runner.claim(ctx, d)
	if err != nil || !claimed {
		t.Fatalf("claiming child: %v %v", claimed, err)
	}
	exec := execution{Runner: runner, delivery: d, epoch: row.epoch}
	state, err := exec.prepareResearch(ctx, &researchState{task: row})
	if err != nil || !strings.Contains(state.parentContext, background) ||
		!strings.Contains(state.parentContext, "only with evidence archived for this task") {
		t.Fatalf("follow-up context was not loaded as background: %v", err)
	}
}

func TestResearchPreferencesIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := t.Context()
	t.Setenv("TRIAL_MODEL_ENABLED", "false")
	t.Setenv("MODEL_MASTER_KEY_FILE", testMaster(t))
	owner, other := newTestTask(t, db), newTestTask(t, db)
	store := executionStore(t, db)
	actions := taskservice.TaskActions{Store: store}
	original := model.ResearchPreferences{Sources: "official", Detail: "brief", Structure: "bullets"}
	updated := model.ResearchPreferences{Sources: "github", Detail: "detailed", Structure: "conclusion_first"}

	if err := actions.SavePreferences(ctx, owner.Tenant, model.ResearchPreferences{Sources: "invalid"}); !errors.Is(err, taskservice.ErrInvalidPreferences) {
		t.Fatalf("unexpected result for invalid preference: %v", err)
	}
	if err := actions.SavePreferences(ctx, owner.Tenant, original); err != nil {
		t.Fatal(err)
	}
	if saved, err := actions.Preferences(ctx, owner.Tenant); err != nil || saved != original {
		t.Fatalf("saved preferences: %+v %v", saved, err)
	}
	if saved, err := actions.Preferences(ctx, other.Tenant); err != nil || saved != (model.ResearchPreferences{}) {
		t.Fatalf("foreign tenant saw preferences: %+v %v", saved, err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/model-config", strings.NewReader("{\"base_url\":\"https://api.deepseek.com\",\"model\":\"fixture\",\"api_key\":\"fixture-preferences-key\"}"))
	(&apiServer{db: db}).modelConfig(rec, req, owner.Tenant)
	if rec.Code != 200 {
		t.Fatalf("fixture model configuration failed: %d", rec.Code)
	}
	created, err := actions.Submit(ctx, owner.Tenant, "prefs-original", "First task", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := actions.SavePreferences(ctx, owner.Tenant, updated); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE research_tasks SET status='SUCCEEDED' WHERE id=?", owner.Task); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO research_reports(tenant_id,task_id,markdown) VALUES(?,?,?)", owner.Tenant, owner.Task, "Original report"); err != nil {
		t.Fatal(err)
	}
	followUp, err := actions.Submit(ctx, owner.Tenant, "prefs-followup", "Follow-up task", owner.Task, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := actions.SavePreferences(ctx, owner.Tenant, model.ResearchPreferences{}); err != nil {
		t.Fatal(err)
	}
	cleared, err := actions.Submit(ctx, owner.Tenant, "prefs-cleared", "Default settings", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if saved, err := actions.Preferences(ctx, owner.Tenant); err != nil || saved != (model.ResearchPreferences{}) {
		t.Fatalf("cleared preferences: %+v %v", saved, err)
	}

	runner := Runner{store: store}
	for _, tc := range []struct {
		id   string
		want model.ResearchPreferences
	}{{created.ID, original}, {followUp.ID, updated}, {cleared.ID, model.ResearchPreferences{}}} {
		d := delivery{Tenant: owner.Tenant, Task: tc.id, Generation: 1}
		row, claimed, err := runner.claim(ctx, d)
		if err != nil || !claimed {
			t.Fatalf("claim preference task: %v %v", claimed, err)
		}
		exec := execution{Runner: runner, delivery: d, epoch: row.epoch}
		state, err := exec.prepareResearch(ctx, &researchState{task: row})
		if err != nil || state.preferences != tc.want {
			t.Fatalf("preferences snapshot differs for task %s: %v", tc.id, err)
		}
	}
	if !strings.Contains(sourcePreference(original), "current research goal take precedence") ||
		!strings.Contains(reportPreferences(original), "value bullets") {
		t.Fatal("worker prompts omit saved preferences or task precedence")
	}
}

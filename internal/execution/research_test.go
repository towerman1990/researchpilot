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
	"time"

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

func TestResearchIntegration(t *testing.T) {
	researchFixture(t, researchFixtureOptions{sufficient: true})
}

func TestUnseededSearchResearchIntegration(t *testing.T) {
	t.Setenv("BRAVE_SEARCH_API_KEY", "fixture-search-key")
	researchFixture(t, researchFixtureOptions{sufficient: true, webSearch: true})
}

func TestCompactSynthesisIntegration(t *testing.T) {
	researchFixture(t, researchFixtureOptions{sufficient: true, compactSynthesis: true})
}

func TestInsufficientEvidenceIntegration(t *testing.T) { researchFixture(t, researchFixtureOptions{}) }

func TestCoverageJSONRepairIntegration(t *testing.T) {
	researchFixture(t, researchFixtureOptions{sufficient: true, malformed: true})
}

func TestPlanReviewIntegration(t *testing.T) {
	researchFixture(t, researchFixtureOptions{sufficient: true, reviewPlan: true})
}

func TestCitationReviewIntegration(t *testing.T) {
	researchFixture(t, researchFixtureOptions{sufficient: true, reviewCitations: true})
}

type researchFixtureOptions struct {
	sufficient, malformed, reviewPlan, reviewCitations, compactSynthesis, webSearch bool
}

func researchFixture(t *testing.T, options researchFixtureOptions) {
	sufficient, malformed, review := options.sufficient, options.malformed, options.reviewPlan
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
	if review || options.reviewCitations {
		actions := taskservice.TaskActions{Store: executionStore(t, db)}
		submit := taskservice.SubmitOptions{ReviewPlan: review, ReviewCitations: options.reviewCitations}
		created, err := actions.Submit(t.Context(), d.Tenant, "plan-review-test", "Reviewable research goal", "", submit)
		if err != nil {
			t.Fatal(err)
		}
		d.Task = created.ID
		if options.reviewCitations {
			submit.ReviewCitations = false
			if _, err := actions.Submit(t.Context(), d.Tenant, "plan-review-test", "Reviewable research goal", "", submit); !errors.Is(err, taskservice.ErrIdempotencyConflict) {
				t.Fatalf("changing citation review should change the idempotency payload: %v", err)
			}
		}
	} else if _, err := db.Exec("UPDATE research_tasks SET model_version=1 WHERE id=?", d.Task); err != nil {
		t.Fatal(err)
	}
	var fetchedURL string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := "<html><body>Official fixture documents durable research execution.</body></html>"
		if req.URL.Host == "api.deepseek.com" {
			raw, _ := io.ReadAll(req.Body)
			used := 100
			content := `{"steps":[{"tool":"fetch_page","url":"https://example.com/docs"}]}`
			if options.webSearch {
				if strings.Contains(string(raw), "Create a technical research plan") && strings.Contains(string(raw), "https://docs.example.org/research-run") {
					t.Fatal("planning fixture unexpectedly has a source URL")
				}
				content = `{"steps":[{"tool":"web_search","query":"durable research execution reference"}]}`
			}
			if strings.Contains(string(raw), "You are the citation reviewer") {
				content = `{"supported":[true,false]}`
			} else if strings.Contains(string(raw), "findings") {
				var request struct{ Messages []struct{ Content string } }
				if err := json.Unmarshal(raw, &request); err != nil {
					t.Fatal(err)
				}
				if options.compactSynthesis && len(request.Messages[1].Content) > 7500 {
					t.Fatal("full synthesis prompt was sent despite the budget limit")
				}
				ids := regexp.MustCompile(`"source_id":"([a-f0-9]{32})"`).FindStringSubmatch(request.Messages[1].Content)
				if len(ids) != 2 {
					t.Fatal("model prompt lacks persisted evidence")
				}
				findings := []map[string]any{{"text": "The evidence supports durable execution.", "sources": []string{ids[1]}}}
				if options.reviewCitations {
					findings = append(findings, map[string]any{"text": "Unsupported fixture claim", "sources": []string{ids[1]}})
				}
				encoded, _ := json.Marshal(map[string]any{"findings": findings, "limitations": []string{"Fixture evidence."}})
				content = string(encoded)
			}
			if strings.Contains(string(raw), "Assess evidence coverage") {
				if options.compactSynthesis {
					used = 25000
				}
				coverage, _ := json.Marshal(map[string]any{"sufficient": sufficient, "gaps": []string{"Missing direct support"}})
				content = string(coverage)
				if malformed {
					content = `{"sufficient":true,"gaps":["A "quoted" note"]}`
				}
			}
			if strings.Contains(string(raw), "Repair coverage JSON") {
				content = `{"sufficient":true,"gaps":["A quoted note"]}`
			}
			encoded, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": content}, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": used, "prompt_tokens": used - 30, "completion_tokens": 30}})
			body = string(encoded)
		} else if req.URL.Host == "api.search.brave.com" {
			if !options.webSearch || req.URL.Path != "/res/v1/web/search" || req.URL.Query().Get("q") != "durable research execution reference" || req.Header.Get("X-Subscription-Token") != "fixture-search-key" {
				t.Fatalf("unexpected search request to %s", req.URL)
			}
			body = `{"web":{"results":[{"title":"Unreadable document","url":"https://example.com/unavailable","description":"Unverified search snippet."},{"title":"Official fixture","url":"https://docs.example.org/research-run","description":"Snippet is not report evidence."}]}}`
		} else {
			if req.Header.Get("X-Subscription-Token") != "" {
				t.Fatal("search credential forwarded to a source")
			}
			if options.webSearch && req.URL.Path == "/unavailable" {
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Request: req}, nil
			}
			fetchedURL = req.URL.String()
			if options.compactSynthesis {
				body = "<main>" + strings.Repeat("Official fixture documents durable research execution. ", 240) + "</main>"
			}
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
	if options.compactSynthesis {
		expectedUsage = 25200
	}
	if malformed {
		expectedUsage += 100
	}
	if !sufficient {
		expectedStatus = "PARTIAL"
		expectedUsage = 600
	}
	if options.reviewCitations {
		expectedStatus = "PARTIAL"
		expectedUsage += 100
		report, err := worker.store.ReadReport(t.Context(), d.Tenant, d.Task)
		if err != nil || !strings.Contains(report, "The evidence supports durable execution.") || strings.Contains(report, "Unsupported fixture claim") {
			t.Fatalf("cited findings were not filtered correctly: %q %v", report, err)
		}
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
	if options.webSearch {
		sources, err := worker.store.EvaluationSources(t.Context(), d.Tenant, d.Task)
		if err != nil || len(sources) != 1 || fetchedURL != "https://docs.example.org/research-run" ||
			sources[0].URL != fetchedURL || !strings.Contains(sources[0].Excerpt, "Official fixture documents durable research execution.") ||
			strings.Contains(sources[0].Excerpt, "Snippet is not report evidence.") {
			t.Fatalf("expected only the cited original page, got %+v (%v)", sources, err)
		}
	}
	if options.reviewCitations {
		tasks := taskservice.Tasks{Store: worker.store}
		rows, err := tasks.ModelUsage(t.Context(), d.Tenant, "7", time.Now())
		if err != nil || len(rows) != 1 || rows[0].BaseURL != "https://api.deepseek.com" ||
			rows[0].Model != "deepseek-flash" || rows[0].InputTokens != 280 || rows[0].OutputTokens != 120 || rows[0].UnsplitTokens != 0 {
			t.Fatalf("wrong split usage: %+v %v", rows, err)
		}
		foreign := newTestTask(t, db)
		if rows, err := tasks.ModelUsage(t.Context(), foreign.Tenant, "7", time.Now()); err != nil || len(rows) != 0 {
			t.Fatalf("foreign account received model usage: %+v %v", rows, err)
		}
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
	if _, err := actions.Submit(ctx, parent.Tenant, key, question, parent.Task, taskservice.SubmitOptions{}); !errors.Is(err, taskservice.ErrReportNotReady) {
		t.Fatalf("follow-up accepted before report: %v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE research_tasks SET status='PARTIAL' WHERE id=?", parent.Task); err != nil {
		t.Fatal(err)
	}
	const background = "Original finding about lease fencing"
	if _, err := db.ExecContext(ctx, "INSERT INTO research_reports(tenant_id,task_id,markdown) VALUES(?,?,?)", parent.Tenant, parent.Task, background); err != nil {
		t.Fatal(err)
	}
	if _, err := actions.Submit(ctx, foreign.Tenant, key, question, parent.Task, taskservice.SubmitOptions{}); !errors.Is(err, taskservice.ErrTaskNotFound) {
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
	child, err := actions.Submit(ctx, parent.Tenant, key, question, parent.Task, taskservice.SubmitOptions{})
	if err != nil || child.ParentTaskID == nil || *child.ParentTaskID != parent.Task || child.Goal != question {
		t.Fatalf("submitted follow-up: %+v %v", child, err)
	}
	replay, err := actions.Submit(ctx, parent.Tenant, key, question, strings.ToUpper(parent.Task), taskservice.SubmitOptions{})
	if err != nil || replay.ID != child.ID {
		t.Fatalf("replayed follow-up created different task: %v", err)
	}
	if _, err := actions.Submit(ctx, parent.Tenant, key, question, "", taskservice.SubmitOptions{}); !errors.Is(err, taskservice.ErrIdempotencyConflict) {
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
	created, err := actions.Submit(ctx, owner.Tenant, "prefs-original", "First task", "", taskservice.SubmitOptions{ReviewPlan: true})
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
	followUp, err := actions.Submit(ctx, owner.Tenant, "prefs-followup", "Follow-up task", owner.Task, taskservice.SubmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := actions.SavePreferences(ctx, owner.Tenant, model.ResearchPreferences{}); err != nil {
		t.Fatal(err)
	}
	cleared, err := actions.Submit(ctx, owner.Tenant, "prefs-cleared", "Default settings", "", taskservice.SubmitOptions{})
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

func TestRepositorySourceTakesPriorityOverDirectoryListing(t *testing.T) {
	sources := []evidence{
		{ID: "dir", Title: "Repository directory / at a commit", URL: "https://github.com/example/project/tree/commit", Excerpt: "Files shown:\nrecoverer.go\n"},
		{ID: "manifest", Title: "Dependency manifest go.mod at a commit", URL: "https://github.com/example/project/blob/commit/go.mod", Excerpt: "module example/project"},
		{ID: "source", Title: "Repository file recoverer.go", URL: "https://github.com/example/project/blob/commit/recoverer.go", Excerpt: "Recover expired leases and retry."},
	}
	if got := previousRepositoryListings(sources); !strings.Contains(got, "recoverer.go") ||
		!strings.Contains(got, sources[0].URL) || strings.Contains(got, "Recover expired") {
		t.Fatalf("unexpected replan context: %s", got)
	}
	if got := promptEvidence(sources, "Explain task recovery", 16000, 1); len(got) != 1 || got[0].ID != "source" {
		t.Fatalf("selected evidence: %+v", got)
	}
}

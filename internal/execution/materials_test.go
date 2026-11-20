package execution

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	taskservice "researchpilot/internal/task"
	researchtools "researchpilot/internal/tools/research"
)

func TestUploadedMaterialsIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := t.Context()
	t.Setenv("TRIAL_MODEL_ENABLED", "false")
	if os.Getenv("MODEL_MASTER_KEY_FILE") == "" {
		t.Setenv("MODEL_MASTER_KEY_FILE", testMaster(t))
	}
	owner, foreign := newTestTask(t, db), newTestTask(t, db)
	store := executionStore(t, db)
	actions := taskservice.TaskActions{Store: store}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/model-config", strings.NewReader("{\"base_url\":\"https://api.deepseek.com\",\"model\":\"fixture\",\"api_key\":\"fixture-upload-key\"}"))
	(&apiServer{db: db}).modelConfig(rec, req, owner.Tenant)
	if rec.Code != 200 {
		t.Fatalf("fixture model configuration failed: %d", rec.Code)
	}

	const key, goal = "material-test-key", "Summarize the retention policy in the attached note."
	file := taskservice.Material{Name: "policy.md", Content: "The fixture retention period is 30 days."}
	created, err := actions.Submit(ctx, owner.Tenant, key, goal, "", false, file)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := actions.Submit(ctx, owner.Tenant, key, goal, "", false, file)
	if err != nil || replay.ID != created.ID {
		t.Fatalf("matching upload did not replay its task: %v", err)
	}
	changed := file
	changed.Content += " Changed."
	if _, err := actions.Submit(ctx, owner.Tenant, key, goal, "", false, changed); !errors.Is(err, taskservice.ErrIdempotencyConflict) {
		t.Fatalf("changed material accepted with the same key: %v", err)
	}
	if _, err := actions.Submit(ctx, owner.Tenant, "invalid-material", goal, "", false, taskservice.Material{Name: "file.exe", Content: "invalid"}); !errors.Is(err, taskservice.ErrInvalidMaterials) {
		t.Fatalf("invalid file accepted: %v", err)
	}
	sources, err := store.UploadedSources(ctx, owner.Tenant, created.ID)
	if err != nil || len(sources) != 1 || sources[0].Excerpt != file.Content || sources[0].Type != "upload" {
		t.Fatalf("material not archived: %v", err)
	}
	source := sources[0]
	if _, err := store.ReadSource(ctx, foreign.Tenant, created.ID, source.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("foreign account could read material: %v", err)
	}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.deepseek.com" {
			t.Fatalf("uploaded evidence sufficient; unexpected external tool request to %s", req.URL.Host)
		}
		body, _ := io.ReadAll(req.Body)
		if !strings.Contains(string(body), file.Content) {
			t.Fatal("model did not receive the uploaded evidence excerpt")
		}
		var reply string
		switch {
		case strings.Contains(string(body), "Assess evidence coverage"):
			reply = "{\"sufficient\":true,\"gaps\":[]}"
		case strings.Contains(string(body), "Every finding must cite"):
			result, _ := json.Marshal(map[string]any{"findings": []map[string]any{{"text": file.Content, "sources": []string{source.ID}}}})
			reply = string(result)
		default:
			t.Fatal("unexpected model work before synthesis")
		}
		raw, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": reply}, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 50}})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: req}, nil
	})}
	worker := Runner{store: store, modelHTTP: client, registry: researchtools.NewRegistry(client)}
	d := delivery{Tenant: owner.Tenant, Task: created.ID, Generation: 1}
	row, claimed, err := worker.claim(ctx, d)
	if err != nil || !claimed {
		t.Fatalf("could not claim upload task: %v %v", claimed, err)
	}
	if err := worker.executeDurable(ctx, d, row); err != nil {
		t.Fatal(err)
	}
	report, err := store.ReadReport(ctx, owner.Tenant, created.ID)
	if err != nil || !strings.Contains(report, source.URL) || !strings.Contains(report, file.Content) {
		t.Fatalf("report did not cite the uploaded material: %v", err)
	}
}

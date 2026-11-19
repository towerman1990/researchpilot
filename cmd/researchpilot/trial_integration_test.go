package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTrialQuotaIntegration(t *testing.T) {
	db := integrationDB(t)
	if err := migrate(t.Context(), db, env("MIGRATIONS_DIR", filepath.Join("..", "..", "migrations"))); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRIAL_MODEL_ENABLED", "true")
	t.Setenv("MODEL_MASTER_KEY_FILE", testMaster(t))
	keyPath := filepath.Join(t.TempDir(), "trial-key")
	if err := os.WriteFile(keyPath, []byte("test-only-trial-key"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRIAL_MODEL_API_KEY_FILE", keyPath)
	tenant := identifier(16)
	if _, err := db.Exec("INSERT INTO users(id,email,password_hash) VALUES(?,?,?)", tenant, tenant+"@example.test", "unused"); err != nil {
		t.Fatal(err)
	}
	api := &apiServer{db: db}
	create := func(key string) (int, string) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/research/tasks", strings.NewReader(`{"goal":"test trial"}`))
		req.Header.Set("Idempotency-Key", key)
		result := httptest.NewRecorder()
		api.createTask(result, req, tenant)
		return result.Code, result.Body.String()
	}
	code, body := create("trial-idempotency-key")
	if code != 202 {
		t.Fatalf("first task: %d %s", code, body)
	}
	var first snapshot
	if err := json.Unmarshal([]byte(body), &first); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 15)
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); c, _ := create(fmt.Sprintf("concurrent-trial-%02d", i)); codes <- c }(i)
	}
	wg.Wait()
	close(codes)
	accepted, rejected := 0, 0
	for c := range codes {
		if c == 202 {
			accepted++
		} else if c == 409 {
			rejected++
		} else {
			t.Errorf("unexpected status %d", c)
		}
	}
	if accepted != 9 || rejected != 6 {
		t.Fatalf("accepted=%d rejected=%d", accepted, rejected)
	}
	code, body = create("trial-idempotency-key")
	var replay snapshot
	if code != 202 || json.Unmarshal([]byte(body), &replay) != nil || replay.ID != first.ID {
		t.Fatalf("idempotent replay: %d %s", code, body)
	}
	code, body = create("after-trial-limit")
	if code != 409 || !strings.Contains(body, "trial_exhausted") {
		t.Fatalf("exhaustion: %d %s", code, body)
	}
	var trialVersion uint32
	if err := db.QueryRow("SELECT model_version FROM research_tasks WHERE tenant_id=? AND id=?", tenant, first.ID).Scan(&trialVersion); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadModel(t.Context(), db, tenant, trialVersion)
	if err != nil || loaded.Key != "test-only-trial-key" {
		t.Fatalf("trial snapshot cannot be decrypted: %v", err)
	}
	var used int
	if err := db.QueryRow("SELECT COUNT(*) FROM trial_usage WHERE tenant_id=?", tenant).Scan(&used); err != nil || used != 10 {
		t.Fatalf("trial count %d: %v", used, err)
	}
	get := httptest.NewRecorder()
	api.modelConfig(get, httptest.NewRequest(http.MethodGet, "/api/v1/model-config", nil), tenant)
	if !strings.Contains(get.Body.String(), `"configured":false`) || strings.Contains(get.Body.String(), "test-only") {
		t.Fatalf("trial secret leaked as personal config: %s", get.Body.String())
	}
	own := httptest.NewRecorder()
	api.modelConfig(own, httptest.NewRequest(http.MethodPut, "/api/v1/model-config", strings.NewReader(`{"base_url":"https://api.deepseek.com","model":"test","api_key":"test-personal-key"}`)), tenant)
	if own.Code != 200 {
		t.Fatalf("save own key: %d %s", own.Code, own.Body.String())
	}
	code, body = create("after-personal-key")
	if code != 202 {
		t.Fatalf("own key after quota: %d %s", code, body)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM trial_usage WHERE tenant_id=?", tenant).Scan(&used); err != nil || used != 10 {
		t.Fatalf("own key consumed quota: %d %v", used, err)
	}
}

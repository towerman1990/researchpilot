package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func claimTest(t *testing.T, db *sql.DB) (execution, taskRow) {
	t.Helper()
	d := newTestTask(t, db)
	r := runner{db: db, tool: stubTool{}, model: stubProvider{}}
	state, ok, err := r.claim(context.Background(), d)
	if err != nil || !ok {
		t.Fatal("claim", err)
	}
	return execution{runner: r, delivery: d, epoch: state.epoch}, state
}
func TestPartialAndExpiredEventsIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	e, _ := claimTest(t, db)
	saved, err := e.saveSources(ctx, []evidence{{Title: "Official fixture", URL: "https://go.dev/doc/", Excerpt: "Archived evidence only."}})
	if err != nil || len(saved) != 1 {
		t.Fatal(err)
	}
	if err = e.failed(ctx, errBudget); err != nil {
		t.Fatal(err)
	}
	var status, kind, markdown string
	if err = db.QueryRow("SELECT t.status,r.kind,r.markdown FROM research_tasks t JOIN research_reports r ON r.task_id=t.id AND r.tenant_id=t.tenant_id WHERE t.id=?", e.Task).Scan(&status, &kind, &markdown); err != nil || status != "PARTIAL" || kind != "partial" || !strings.Contains(markdown, "尚未形成完整研究结论") {
		t.Fatal(status, kind, err)
	}
	// Finishing again is idempotent and cannot create a duplicate report.
	if err = e.failed(ctx, errBudget); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE research_tasks SET finished_at=UTC_TIMESTAMP(6)-INTERVAL 31 DAY WHERE id=?", e.Task); err != nil {
		t.Fatal(err)
	}
	if err = pruneEvents(ctx, db); err != nil {
		t.Fatal(err)
	}
	api := apiServer{db: db}
	snapshot, err := api.readTask(ctx, e.Tenant, e.Task)
	if err != nil || snapshot.MinSeq != snapshot.Seq+1 {
		t.Fatal(snapshot, err)
	}
	req := httptest.NewRequest("GET", "/events?after=0", nil)
	req.SetPathValue("id", e.Task)
	recorder := httptest.NewRecorder()
	api.events(recorder, req, e.Tenant)
	if !strings.Contains(recorder.Body.String(), "event: reset") || strings.Contains(recorder.Body.String(), "id:") {
		t.Fatal(recorder.Body.String())
	}
	// Current cursor on a pruned terminal task closes without resetting/replaying.
	req = httptest.NewRequest("GET", "/events?after="+fmtSeq(snapshot.Seq), nil)
	req.SetPathValue("id", e.Task)
	recorder = httptest.NewRecorder()
	api.events(recorder, req, e.Tenant)
	if strings.Contains(recorder.Body.String(), "event: reset") {
		t.Fatal("valid floor reset")
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM research_sources WHERE task_id=?", e.Task).Scan(&count); err != nil || count != 1 {
		t.Fatal("retention removed sources", err)
	}
}
func TestInterruptedAttemptAndCancellationFenceIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	e, _ := claimTest(t, db)
	interrupted := errors.New("simulated crash after request")
	if _, err := e.checkpoint(ctx, "crash", func(context.Context) (string, error) { return "", interrupted }); !errors.Is(err, interrupted) {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE research_tasks SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=?", e.Task); err != nil {
		t.Fatal(err)
	}
	if err := e.runner.recoverTask(ctx, e.delivery); err != nil {
		t.Fatal(err)
	}
	d := e.delivery
	d.Generation++
	state, ok, err := e.runner.claim(ctx, d)
	if err != nil || !ok {
		t.Fatal(err)
	}
	next := execution{runner: e.runner, delivery: d, epoch: state.epoch}
	if _, err = next.checkpoint(ctx, "crash", func(context.Context) (string, error) { return "saved", nil }); err != nil {
		t.Fatal(err)
	}
	var abandoned int
	if err = db.QueryRow("SELECT COUNT(*) FROM execution_attempts WHERE task_id=? AND step_key='crash' AND status='ABANDONED'", e.Task).Scan(&abandoned); err != nil || abandoned != 1 {
		t.Fatal("attempt not abandoned", err)
	}
	if _, err = db.Exec("UPDATE research_tasks SET status='CANCELLING' WHERE id=?", e.Task); err != nil {
		t.Fatal(err)
	}
	if err = next.mutate(ctx, func(*sql.Tx, taskRow) error { t.Error("cancelled completion entered"); return nil }); !errors.Is(err, errLeaseLost) {
		t.Fatal(err)
	}
	if err = next.failed(ctx, context.Canceled); err != nil {
		t.Fatal(err)
	}
}
func testMaster(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(raw)), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestModelKeyRotationAndRevocationIntegration(t *testing.T) {
	db := integrationDB(t)
	db.SetMaxOpenConns(1)
	e, _ := claimTest(t, db)
	ctx := context.Background()
	// Rotation visits every stored version, so retain access to the suite's original key.
	oldPath := os.Getenv("MODEL_MASTER_KEY_FILE")
	if oldPath == "" {
		t.Skip("MODEL_MASTER_KEY_FILE not configured")
	}
	t.Setenv("MODEL_MASTER_KEY_VERSION", "1")
	a := apiServer{db: db}
	save := func(key string) {
		req := httptest.NewRequest("PUT", "/model-config", strings.NewReader(`{"base_url":"https://api.deepseek.com","model":"fixture","api_key":"`+key+`"}`))
		requestCtx, cancelRequest := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelRequest()
		req = req.WithContext(requestCtx)
		rec := httptest.NewRecorder()
		a.modelConfig(rec, req, e.Tenant)
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
	save("fixture-model-key")
	save("")
	config, err := loadModel(ctx, db, e.Tenant, 2)
	if err != nil || config.Key != "fixture-model-key" {
		t.Fatal("retained key", err)
	}
	t.Setenv("MODEL_MASTER_KEY_FILE_V1", oldPath)
	t.Setenv("MODEL_MASTER_KEY_FILE", testMaster(t))
	t.Setenv("MODEL_MASTER_KEY_VERSION", "2")
	if err = rotateModelKeys(ctx, db); err != nil {
		t.Fatal(err)
	}
	if config, err = loadModel(ctx, db, e.Tenant, 2); err != nil || config.Key != "fixture-model-key" {
		t.Fatal("rotated key", err)
	}
	// Rotate back, keeping all unrelated suite configs readable afterwards.
	newPath := os.Getenv("MODEL_MASTER_KEY_FILE")
	t.Setenv("MODEL_MASTER_KEY_FILE_V2", newPath)
	t.Setenv("MODEL_MASTER_KEY_FILE", oldPath)
	t.Setenv("MODEL_MASTER_KEY_VERSION", "1")
	if err = rotateModelKeys(ctx, db); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("DELETE", "/model-config", nil)
	rec := httptest.NewRecorder()
	a.revokeModel(rec, req, e.Tenant)
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	if _, err = loadModel(ctx, db, e.Tenant, 2); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("revoked key usable", err)
	}
	if _, err = e.generate(ctx, config, "must never call upstream"); err == nil || err.Error() != "model_configuration_revoked" {
		t.Fatal(err)
	}
}
func TestRedisConcurrentRateLimitIntegration(t *testing.T) {
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_REDIS_ADDR not set")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatal("Redis unavailable", err)
	}
	l := redisLimiter{client}
	subject := identifier(16)
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := l.Allow(context.Background(), "test", subject, 5)
			if err != nil {
				t.Error(err)
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 5 {
		t.Fatalf("allowed %d", allowed.Load())
	}
	// Check atomic TTL without scanning unrelated deployment keys.
	digest := shaSubject(subject)
	key := rateTestKey("test", digest)
	ttl, err := client.TTL(context.Background(), key).Result()
	if err == nil && ttl <= 0 {
		previous := fmt.Sprintf("rp:%s:rl:v1:test:%s:%d", env("APP_ENV", "local"), digest, time.Now().UTC().Unix()/60-1)
		ttl, err = client.TTL(context.Background(), previous).Result()
	}
	if err != nil || ttl <= 0 || ttl > 120*time.Second {
		t.Fatal(ttl, err)
	}
}
func fmtSeq(seq uint64) string { return fmt.Sprintf("%d", seq) }
func shaSubject(subject string) string {
	d := sha256.Sum256([]byte(subject))
	return fmt.Sprintf("%x", d[:16])
}
func rateTestKey(operation, digest string) string {
	return fmt.Sprintf("rp:%s:rl:v1:%s:%s:%d", env("APP_ENV", "local"), operation, digest, time.Now().UTC().Unix()/60)
}

func TestModelAttemptLimitSurvivesRecoveryIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	e, _ := claimTest(t, db)
	scoped := context.WithValue(ctx, modelCallKey{}, "synthesis")
	for i := 0; i < 3; i++ {
		id, err := e.reserve(scoped, 10)
		if err != nil {
			t.Fatal(err)
		}
		if err = e.settle(ctx, id, 1, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("UPDATE research_tasks SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=?", e.Task); err != nil {
		t.Fatal(err)
	}
	if err := e.runner.recoverTask(ctx, e.delivery); err != nil {
		t.Fatal(err)
	}
	d := e.delivery
	d.Generation++
	state, ok, err := e.runner.claim(ctx, d)
	if err != nil || !ok {
		t.Fatal(err)
	}
	next := execution{runner: e.runner, delivery: d, epoch: state.epoch}
	if _, err = next.reserve(scoped, 10); err == nil || err.Error() != "model_retry_exhausted" {
		t.Fatal("retry limit reset", err)
	}
}

func TestLostRedisMessageIsRedispatchedIntegration(t *testing.T) {
	db := integrationDB(t)
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_REDIS_ADDR not set")
	}
	ctx := context.Background()
	d := newTestTask(t, db)
	queue := "rp_test_" + identifier(16)
	opt := asynq.RedisClientOpt{Addr: address}
	client := asynq.NewClient(opt)
	defer client.Close()
	inspector := asynq.NewInspector(opt)
	defer inspector.Close()
	defer inspector.DeleteQueue(queue, true)
	if err := relay(ctx, db, client, queue); err != nil {
		t.Fatal(err)
	}
	if _, err := inspector.GetTaskInfo(queue, fmt.Sprintf("research:%s:1", d.Task)); err != nil {
		t.Fatal(err)
	}
	if _, err := inspector.DeleteAllPendingTasks(queue); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE task_outbox SET sent_at=UTC_TIMESTAMP(6)-INTERVAL 61 SECOND WHERE task_id=? AND generation=1", d.Task); err != nil {
		t.Fatal(err)
	}
	r := runner{db: db}
	if err := r.recoverTask(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := r.claim(ctx, d); err != nil || claimed {
		t.Fatal("stale generation accepted", err)
	}
	if err := relay(ctx, db, client, queue); err != nil {
		t.Fatal(err)
	}
	if _, err := inspector.GetTaskInfo(queue, fmt.Sprintf("research:%s:2", d.Task)); err != nil {
		t.Fatal("lost message not redispatched", err)
	}
}

func TestModelProbeUsesRedactedCapabilitiesIntegration(t *testing.T) {
	db := integrationDB(t)
	e, _ := claimTest(t, db)
	if os.Getenv("MODEL_MASTER_KEY_FILE") == "" {
		t.Skip("MODEL_MASTER_KEY_FILE not set")
	}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
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
	a := apiServer{db: db, modelHTTP: &http.Client{Transport: transport}}
	req := httptest.NewRequest("PUT", "/model-config", strings.NewReader(`{"base_url":"https://api.deepseek.com","model":"fixture","api_key":"fixture-model-key"}`))
	rec := httptest.NewRecorder()
	a.modelConfig(rec, req, e.Tenant)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	rec = httptest.NewRecorder()
	a.probeModel(rec, httptest.NewRequest("POST", "/probe", nil), e.Tenant)
	var result struct{ Capabilities map[string]bool }
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &result) != nil || len(result.Capabilities) != 5 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	for name, ok := range result.Capabilities {
		if !ok {
			t.Error("probe failed", name)
		}
	}
	if strings.Contains(rec.Body.String(), "fixture-model-key") {
		t.Fatal("probe leaked key")
	}
}

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
	e.runner.modelHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("private_address_forbidden") })}
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

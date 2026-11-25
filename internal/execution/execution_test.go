package execution

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/network/standard"
	"github.com/hibiken/asynq"

	"researchpilot/internal/common/envconfig"
	"researchpilot/internal/common/identity"
	"researchpilot/internal/handler"
	"researchpilot/internal/persistence"
	"researchpilot/internal/persistence/migrations"
	"researchpilot/internal/repository"
	"researchpilot/internal/repository/model"
	"researchpilot/internal/service"
	taskservice "researchpilot/internal/task"
	"researchpilot/internal/transport/httpserver"
)

type snapshot = model.TaskSnapshot

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not configured")
	}
	db, err := persistence.OpenMySQL(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func executionStore(t *testing.T, db *sql.DB) *repository.Store {
	t.Helper()
	store, err := repository.New(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func newTestTask(t *testing.T, db *sql.DB) delivery {
	t.Helper()
	d := delivery{Tenant: identity.New(16), Task: identity.New(16), Generation: 1}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "INSERT INTO users(id,email,password_hash) VALUES(?,?,?)", d.Tenant, d.Tenant+"@example.test", "unused"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO research_tasks(id,tenant_id,goal,status,event_seq,deadline_at,runtime_version) VALUES(?,?,?,'QUEUED',1,UTC_TIMESTAMP(6)+INTERVAL 5 MINUTE,'m2.1')", d.Task, d.Tenant, "integration"); err != nil {
		t.Fatal(err)
	}
	store := executionStore(t, db)
	if err := store.TransactExecution(ctx, func(tx *repository.ExecutionTx) error {
		if err := tx.InsertTaskOutbox(ctx, d.Tenant, d.Task, 1); err != nil {
			return err
		}
		return appendEvent(ctx, tx, d.Tenant, d.Task, 1, "task_status", map[string]string{"status": "QUEUED"})
	}); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDurableExecutionIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	r := Runner{store: executionStore(t, db), tool: stubTool{}, model: stubProvider{}}
	d := newTestTask(t, db)
	logID := identity.New(16)
	if _, err := db.ExecContext(ctx, "UPDATE research_tasks SET creation_logid=? WHERE id=?", logID, d.Task); err != nil {
		t.Fatal(err)
	}
	state, ok, err := r.claim(ctx, d)
	if err != nil || !ok {
		t.Fatalf("claim=%v %v", ok, err)
	}
	if _, ok, err = r.claim(ctx, d); err != nil || ok {
		t.Fatalf("duplicate claim=%v %v", ok, err)
	}
	e := execution{Runner: r, delivery: d, epoch: state.epoch}

	var called atomic.Int32
	fn := func(context.Context) (string, error) { called.Add(1); return "saved result", nil }
	if _, err = e.checkpoint(ctx, "collect", fn); err != nil {
		t.Fatal(err)
	}
	if _, err = e.checkpoint(ctx, "collect", fn); err != nil {
		t.Fatal(err)
	}
	if called.Load() != 1 {
		t.Fatalf("tool called %d times", called.Load())
	}
	_, err = e.reserve(ctx, 40001)
	if !errors.Is(err, errBudget) {
		t.Fatalf("budget error=%v", err)
	}
	reservation, err := e.reserve(ctx, 200)
	if err != nil || reservation == "" {
		t.Fatal(err)
	}
	// Simulate a dead worker after a charged external request with no receipt.
	if _, err = db.Exec("UPDATE research_tasks SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=?", d.Task); err != nil {
		t.Fatal(err)
	}
	if err = r.recoverTask(ctx, d); err != nil {
		t.Fatal(err)
	}
	d.Generation++
	next, ok, err := r.claim(ctx, d)
	if err != nil || !ok {
		t.Fatalf("recovery claim=%v %v", ok, err)
	}
	if next.creationLogID != logID {
		t.Fatalf("recovered logid=%q, want %q", next.creationLogID, logID)
	}
	if err = e.mutate(ctx, func(*repository.ExecutionTx, taskRow) error { return nil }); !errors.Is(err, errLeaseLost) {
		t.Fatalf("stale worker err=%v", err)
	}
	replacement := execution{Runner: r, delivery: d, epoch: next.epoch}
	if _, err = replacement.checkpoint(ctx, "collect", fn); err != nil {
		t.Fatal(err)
	}
	if called.Load() != 1 {
		t.Fatal("completed step reran after takeover")
	}
	var used, reserved int
	if err = db.QueryRow("SELECT tokens_used,tokens_reserved FROM research_tasks WHERE id=?", d.Task).Scan(&used, &reserved); err != nil {
		t.Fatal(err)
	}
	if used != 200 || reserved != 0 {
		t.Fatalf("settlement %d %d", used, reserved)
	}
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	err = r.executeDurable(ctx, d, next)
	slog.SetDefault(previousLogger)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), `"msg":"execution_step","logid":"`+logID+`"`) {
		t.Fatalf("worker log missing request correlation: %s", logs.String())
	}
	var status string
	if err = db.QueryRow("SELECT status FROM research_tasks WHERE id=?", d.Task).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "SUCCEEDED" {
		t.Fatal(status)
	}
	var records int
	if err = db.QueryRow("SELECT COUNT(*) FROM research_reports WHERE task_id=?", d.Task).Scan(&records); err != nil || records != 1 {
		t.Fatalf("reports=%d %v", records, err)
	}
}

func TestLeaseRenewalAndCancellationIntegration(t *testing.T) {
	db := integrationDB(t)
	d := newTestTask(t, db)
	r := Runner{store: executionStore(t, db)}
	state, ok, err := r.claim(context.Background(), d)
	if err != nil || !ok {
		t.Fatal(err)
	}
	e := execution{Runner: r, delivery: d, epoch: state.epoch}
	_, err = db.Exec("UPDATE research_tasks SET lease_until=UTC_TIMESTAMP(6)+INTERVAL 6 SECOND WHERE id=?", d.Task)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go e.keepAlive(ctx, cancel, done)
	// One renewal must run before the deliberately shortened lease expires.
	select {
	case <-done:
		t.Fatal("lease renewer stopped")
	case <-time.After(4 * time.Second):
	}
	var remaining int
	if err = db.QueryRow("SELECT TIMESTAMPDIFF(SECOND,UTC_TIMESTAMP(6),lease_until) FROM research_tasks WHERE id=?", d.Task).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining < 20 {
		t.Fatalf("lease did not renew: %d seconds", remaining)
	}
	if _, err = db.Exec("UPDATE research_tasks SET status='CANCELLING' WHERE id=?", d.Task); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation not observed")
	}
	if err = e.failed(context.Background(), context.Canceled); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = db.QueryRow("SELECT status FROM research_tasks WHERE id=?", d.Task).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "CANCELLED" {
		t.Fatal(status)
	}
}

type apiServer struct {
	db *sql.DB
}

func (a *apiServer) repository() (*repository.Store, error) { return repository.New(a.db) }

func (a *apiServer) readTask(ctx context.Context, tenant, id string) (snapshot, error) {
	store, err := a.repository()
	if err != nil {
		return snapshot{}, err
	}
	return store.ReadTask(ctx, tenant, id)
}

func (a *apiServer) modelService() service.Models {
	store, err := a.repository()
	if err != nil {
		panic(err)
	}
	return service.Models{Store: store}
}

func (a *apiServer) modelConfig(w http.ResponseWriter, r *http.Request, tenant string) {
	models := a.modelService()
	if r.Method == "GET" {
		out, err := models.Status(r.Context(), tenant)
		writeTestResponse(w, 200, out, err)
		return
	}
	var input service.ModelInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		panic(err)
	}
	version, err := models.Save(r.Context(), tenant, input)
	writeTestResponse(w, 200, map[string]any{"configured": true, "version": version}, err)
}

func (a *apiServer) revokeModel(w http.ResponseWriter, r *http.Request, tenant string) {
	err := a.modelService().Revoke(r.Context(), tenant)
	writeTestResponse(w, 200, map[string]bool{"configured": false, "revoked": true}, err)
}

func (a *apiServer) createTask(w http.ResponseWriter, r *http.Request, tenant string) {
	var input struct{ Goal string }
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		panic(err)
	}
	store, err := a.repository()
	if err != nil {
		panic(err)
	}
	task, err := (taskservice.TaskActions{Store: store}).Submit(r.Context(), tenant, r.Header.Get("Idempotency-Key"), input.Goal, "", taskservice.SubmitOptions{})
	if err == nil {
		w.Header().Set("Location", "/api/v1/research/tasks/"+task.ID)
	}
	writeTestResponse(w, 202, task, err)
}

func writeTestResponse(w http.ResponseWriter, status int, body any, err error) {
	if err != nil {
		var code string
		status, code = handler.WriteError(err)
		body = map[string]string{"code": code}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func relay(ctx context.Context, db *sql.DB, client *asynq.Client, queue string) error {
	store, err := repository.New(db)
	if err != nil {
		return err
	}
	return taskservice.RelayOutbox(ctx, store, client, queue)
}

func pruneEvents(ctx context.Context, db *sql.DB) error {
	store, err := repository.New(db)
	if err != nil {
		return err
	}
	return taskservice.PruneEvents(ctx, store)
}

func loadModel(ctx context.Context, db *sql.DB, tenant string, version uint32) (modelConfig, error) {
	store, err := repository.New(db)
	if err != nil {
		return modelConfig{}, err
	}
	return service.LoadModel(ctx, store, tenant, version)
}

func claimTest(t *testing.T, db *sql.DB) (execution, taskRow) {
	t.Helper()
	d := newTestTask(t, db)
	r := Runner{store: executionStore(t, db), tool: stubTool{}, model: stubProvider{}}
	state, ok, err := r.claim(context.Background(), d)
	if err != nil || !ok {
		t.Fatal("claim", err)
	}
	return execution{Runner: r, delivery: d, epoch: state.epoch}, state
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
	api := apiServer{db: db}
	store, err := api.repository()
	if err != nil {
		t.Fatal(err)
	}
	token := identity.New(32)
	digest := sha256.Sum256([]byte(token))
	if err := store.CreateSession(ctx, digest[:], e.Tenant); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	h := server.New(server.WithListener(ln), server.WithTransport(standard.NewTransporter))
	h.Use(httpserver.AccessLog)
	auth := handler.Auth{Store: store, Guests: store, Origin: "http://" + ln.Addr().String()}
	auth.RegisterRoutes(h)
	handler.Models{Service: service.Models{Store: store}, Auth: auth}.RegisterRoutes(h)
	handler.Tasks{Actions: taskservice.TaskActions{Store: store}, Auth: auth, Service: taskservice.Tasks{Store: store}, Stream: taskservice.EventStream{Store: store}}.RegisterRoutes(h)
	webRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("fixture web index"), 0600); err != nil {
		t.Fatal(err)
	}
	handler.Web{Root: webRoot}.RegisterRoutes(h)
	done := make(chan error, 1)
	go func() { done <- h.Run() }()
	t.Cleanup(func() { _ = h.Close(); <-done })
	t.Run("authentication and tenant isolation", func(t *testing.T) {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
		var lastHeader http.Header
		check := func(method, path, body string, want int) []byte {
			t.Helper()
			req, err := http.NewRequest(method, auth.Origin+path, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if method != "GET" {
				req.Header.Set("Content-Type", "application/json")
				if path == "/api/v1/research/tasks" {
					req.Header.Set("Idempotency-Key", "native-integration-test")
				}
				req.Header.Set("Origin", auth.Origin)
			}
			req.Header.Set("X-Log-ID", "caller-chosen-id")
			res, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			lastHeader = res.Header
			got, err := io.ReadAll(res.Body)
			if err != nil || res.StatusCode != want {
				t.Fatalf("%s %s: got %d %s (%v), want %d", method, path, res.StatusCode, got, err, want)
			}
			return got
		}
		if got := check("GET", "/", "", 200); string(got) != "fixture web index" {
			t.Fatalf("unexpected web index: %q", got)
		}
		check("GET", "/api/v1/unknown", "", 404)
		check("GET", "/api/v1/research/usage", "", 401)
		check("GET", "/api/v1/research/overview", "", 401)
		check("GET", "/api/v1/research/tasks/"+e.Task+"/report/evidence", "", 401)
		check("GET", "/api/v1/research/tasks/"+e.Task+"/report/feedback", "", 401)
		check("POST", "/api/v1/auth/guest", "{}", 200)
		check("GET", "/api/v1/auth/session", "", 401)
		check("GET", "/api/v1/research/tasks", "", 200)
		check("GET", "/api/v1/research/tasks/"+e.Task, "", 404)
		check("GET", "/api/v1/model-config", "", 401)
		credentials := fmt.Sprintf("{\"email\":\"%s@example.test\",\"password\":\"fixture-password\"}", identity.New(16))
		check("POST", "/api/v1/auth/register", credentials, 201)
		check("POST", "/api/v1/auth/register", credentials, 409)
		check("POST", "/api/v1/auth/login", credentials, 200)
		check("GET", "/api/v1/auth/session", "", 200)
		check("GET", "/api/v1/research/tasks", "", 200)
		check("GET", "/api/v1/research/tasks/"+e.Task, "", 404)
		check("GET", "/api/v1/research/tasks/"+e.Task+"/report/evidence", "", 404)
		check("PUT", "/api/v1/research/tasks/"+e.Task+"/report/feedback", `{"rating":"helpful"}`, 404)
		if os.Getenv("MODEL_MASTER_KEY_FILE") == "" {
			t.Setenv("MODEL_MASTER_KEY_FILE", testMaster(t))
		}
		t.Setenv("MODEL_MASTER_KEY_VERSION", "1")
		t.Setenv("TRIAL_MODEL_ENABLED", "false")
		check("POST", "/api/v1/model-config/probe", "{}", 409)
		check("PUT", "/api/v1/model-config", `{"base_url":"https://api.deepseek.com","model":"fixture","api_key":"fixture-native-key"}`, 200)
		check("GET", "/api/v1/model-config", "", 200)
		var created, replay, cancelled snapshot
		for _, out := range []*snapshot{&created, &replay} {
			if err := json.Unmarshal(check("POST", "/api/v1/research/tasks", "{\"goal\":\"native HTTP integration\"}", 202), out); err != nil {
				t.Fatal(err)
			}
			id := lastHeader.Get("X-Log-ID")
			if decoded, err := hex.DecodeString(id); err != nil || len(decoded) != 16 {
				t.Fatalf("invalid generated logid: %q", id)
			}
			if out.CreationLogID == nil || (out == &created) != (*out.CreationLogID == id) {
				t.Fatalf("unexpected creation logid for HTTP request %q: %+v", id, out.CreationLogID)
			}
		}
		if created.ID == "" || replay.ID != created.ID || created.Status != "QUEUED" {
			t.Fatal(created, replay)
		}
		if *created.CreationLogID != *replay.CreationLogID {
			t.Fatal("idempotent replay changed the original logid")
		}
		var initialOverview taskservice.Overview
		check("GET", "/api/v1/research/tasks/"+created.ID+"/report/evidence", "", 409)
		check("GET", "/api/v1/research/tasks/"+created.ID+"/report/feedback", "", 409)
		if err := json.Unmarshal(check("GET", "/api/v1/research/overview", "", 200), &initialOverview); err != nil ||
			len(initialOverview.Open) != 1 || initialOverview.Open[0].ID != created.ID || initialOverview.Finished == nil ||
			len(initialOverview.Finished) != 0 || initialOverview.MoreOpen {
			t.Fatalf("unexpected initial workspace: %+v, %v", initialOverview, err)
		}
		{
			now := time.Now().UTC().Truncate(24 * time.Hour)
			for _, fixture := range []struct {
				task, status string
				tokens       int
				settled      time.Time
				failure      any
			}{
				{created.ID, "ACTUAL", 12, now, nil},
				{created.ID, "ESTIMATED", 34, now, "model_request_timeout"},
				{created.ID, "RELEASED", 99, now, nil},
				{created.ID, "ACTUAL", 10, now.AddDate(0, 0, -7), nil},
				{e.Task, "ACTUAL", 88, now, nil},
			} {
				if _, err := db.Exec(`INSERT INTO model_attempts
					(tenant_id,task_id,attempt_id,lease_epoch,status,reserved_tokens,charged_tokens,settled_at,error_code)
					SELECT tenant_id,id,?,1,?,100,?,?,? FROM research_tasks WHERE id=?`,
					identity.New(16), fixture.status, fixture.tokens, fixture.settled, fixture.failure, fixture.task); err != nil {
					t.Fatal(err)
				}
			}
			var week, month []model.UsageDay
			if _, err := db.ExecContext(ctx, "UPDATE model_attempts SET input_tokens=8,output_tokens=4 WHERE task_id=? AND charged_tokens=12 AND status='ACTUAL'", created.ID); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(check("GET", "/api/v1/research/usage", "", 200), &week); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(check("GET", "/api/v1/research/usage?days=30", "", 200), &month); err != nil {
				t.Fatal(err)
			}
			expected := model.UsageDay{Day: now.Format(time.DateOnly), ActualTokens: 12, EstimatedTokens: 34, Attempts: 2, FailedAttempts: 1}
			older := model.UsageDay{Day: now.AddDate(0, 0, -7).Format(time.DateOnly), ActualTokens: 10, Attempts: 1}
			if len(week) != 7 || !slices.Contains(week, expected) || slices.Contains(week, older) || len(month) != 30 || !slices.Contains(month, older) || !slices.Contains(month, expected) {
				t.Fatalf("unexpected usage: week=%+v month=%+v", week, month)
			}
			check("GET", "/api/v1/research/usage?days=8", "", 422)
			var modelUsage []model.ModelUsage
			if err := json.Unmarshal(check("GET", "/api/v1/research/usage/models?days=30", "", 200), &modelUsage); err != nil ||
				len(modelUsage) != 1 || modelUsage[0].InputTokens != 8 || modelUsage[0].OutputTokens != 4 ||
				modelUsage[0].UnsplitTokens != 10 || modelUsage[0].EstimatedTokens != 34 {
				t.Fatalf("wrong model cost inputs: %+v %v", modelUsage, err)
			}
		}
		check("POST", "/api/v1/research/tasks", "{\"goal\":\"different goal\"}", 409)
		if err := json.Unmarshal(check("POST", "/api/v1/research/tasks/"+created.ID+"/cancel", "{}", 202), &cancelled); err != nil || cancelled.Status != "CANCELLED" || cancelled.Seq != created.Seq+1 {
			t.Fatal(cancelled, err)
		}
		check("POST", "/api/v1/research/tasks/"+created.ID+"/cancel", "{}", 202)
		t.Run("history cursor preserves tenant and tie-break ordering", func(t *testing.T) {
			var tenant string
			if err := db.QueryRowContext(ctx, "SELECT tenant_id FROM research_tasks WHERE id=?", created.ID).Scan(&tenant); err != nil {
				t.Fatal(err)
			}
			prefix := identity.New(15)
			for i := range 51 {
				if _, err := db.ExecContext(ctx, "INSERT INTO research_tasks(id,tenant_id,goal,status,event_seq,deadline_at,created_at) VALUES(?,?,?,'CANCELLED',1,UTC_TIMESTAMP(6),'2000-01-01')", prefix+fmt.Sprintf("%02x", i), tenant, "history fixture"); err != nil {
					t.Fatal(err)
				}
			}
			const history = "/api/v1/research/tasks"
			var first, second, foreign []snapshot
			if err := json.Unmarshal(check("GET", history, "", 200), &first); err != nil || len(first) != 50 {
				t.Fatalf("first page: %d records, %v", len(first), err)
			}
			cursor := lastHeader.Get("X-Next-Cursor")
			if first[0].ID != created.ID || first[1].ID != prefix+"32" || cursor != prefix+"02" || cursor != first[49].ID {
				t.Fatal("incorrect descending order or next cursor")
			}
			if err := json.Unmarshal(check("GET", history+"?before="+cursor, "", 200), &second); err != nil || len(second) != 2 || second[0].ID != prefix+"01" || second[1].ID != prefix+"00" || lastHeader.Get("X-Next-Cursor") != "" {
				t.Fatalf("second page: %+v, %v", second, err)
			}
			if err := json.Unmarshal(check("GET", history+"?before="+e.Task, "", 200), &foreign); err != nil || len(foreign) != 0 {
				t.Fatalf("unexpected results for a different tenant's cursor: %v", err)
			}
			check("GET", history+"?before=invalid", "", 422)

			oldApproval := identity.New(16)
			if _, err := db.ExecContext(ctx, `INSERT INTO research_tasks(id,tenant_id,goal,status,event_seq,deadline_at,created_at)
				VALUES(?,?,?,'AWAITING_APPROVAL',1,UTC_TIMESTAMP(6)+INTERVAL 1 HOUR,'1999-01-01')`, oldApproval, tenant, "older plan"); err != nil {
				t.Fatal(err)
			}
			var workspace taskservice.Overview
			if err := json.Unmarshal(check("GET", "/api/v1/research/overview", "", 200), &workspace); err != nil ||
				len(workspace.Open) != 1 || workspace.Open[0].ID != oldApproval || workspace.Open[0].Status != "AWAITING_APPROVAL" ||
				len(workspace.Finished) != 10 || workspace.Finished[0].ID != created.ID || workspace.Finished[0].FinishedAt == nil || workspace.MoreOpen {
				t.Fatalf("unexpected workspace after cancellation: %+v, %v", workspace, err)
			}
			if _, err := db.ExecContext(ctx, `UPDATE research_tasks SET status='RUNNING',phase='executing',
				deadline_at=UTC_TIMESTAMP(6)+INTERVAL 1 HOUR,lease_until=UTC_TIMESTAMP(6)+INTERVAL 1 HOUR
				WHERE tenant_id=? AND created_at='2000-01-01'`, tenant); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(check("GET", "/api/v1/research/overview", "", 200), &workspace); err != nil ||
				len(workspace.Open) != 50 || !workspace.MoreOpen || workspace.Open[0].Phase == nil || *workspace.Open[0].Phase != "executing" ||
				len(workspace.Finished) != 1 {
				t.Fatalf("unexpected workspace at the open task limit: %+v, %v", workspace, err)
			}
		})
		check("DELETE", "/api/v1/model-config", "{}", 200)
		check("POST", "/api/v1/auth/logout", "{}", 204)
		check("GET", "/api/v1/auth/session", "", 401)
	})
	fetch := func(after string) string {
		t.Helper()
		req, err := http.NewRequest("GET", "http://"+ln.Addr().String()+"/api/v1/research/tasks/"+e.Task+"/events?after="+after, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: "rp_session", Value: token})
		client := &http.Client{Timeout: 5 * time.Second}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("SSE status %d: %s (%v)", res.StatusCode, body, err)
		}
		return string(body)
	}
	if body := fetch("0"); !strings.Contains(body, "id: 1") || !strings.Contains(body, "event: task_status") || !strings.Contains(body, "\"payload\":{") {
		t.Fatal(body)
	}
	if _, err = db.Exec("UPDATE research_tasks SET finished_at=UTC_TIMESTAMP(6)-INTERVAL 31 DAY WHERE id=?", e.Task); err != nil {
		t.Fatal(err)
	}
	if err = pruneEvents(ctx, db); err != nil {
		t.Fatal(err)
	}
	snapshot, err := api.readTask(ctx, e.Tenant, e.Task)
	if err != nil || snapshot.MinSeq != snapshot.Seq+1 {
		t.Fatal(snapshot, err)
	}
	body := fetch("0")
	if !strings.Contains(body, "event: reset") || strings.Contains(body, "id:") {
		t.Fatal(body)
	}
	if body := fetch(fmtSeq(snapshot.Seq)); strings.Contains(body, "event: reset") {
		t.Fatal("valid floor reset", body)
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM research_sources WHERE task_id=?", e.Task).Scan(&count); err != nil || count != 1 {
		t.Fatal("retention removed sources", err)
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
	store, err := repository.New(db)
	if err != nil {
		t.Fatal(err)
	}
	rollbackErr := errors.New("interrupt key rotation")
	var firstCredential model.RotationCredential
	updates := 0
	err = store.RotateCredentials(ctx, 2, func(row model.RotationCredential) ([]byte, error) {
		if updates > 0 {
			return nil, rollbackErr
		}
		firstCredential = row
		updates++
		return []byte("uncommitted ciphertext"), nil
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("expected interruption after one update, got %v", err)
	}
	var keyVersion uint32
	var ciphertext []byte
	err = db.QueryRowContext(ctx, "SELECT key_version,key_cipher FROM model_configs WHERE tenant_id=? AND version=?", firstCredential.TenantID, firstCredential.Version).Scan(&keyVersion, &ciphertext)
	if err != nil || keyVersion != firstCredential.KeyVersion || !bytes.Equal(ciphertext, firstCredential.KeyCipher) {
		t.Fatalf("credential changed after rollback: %v", err)
	}

	rotation := service.KeyRotation{Store: store}
	if err = rotation.Run(ctx); err != nil {
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
	if err = rotation.Run(ctx); err != nil {
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

func fmtSeq(seq uint64) string { return fmt.Sprintf("%d", seq) }

// This exercise uses only disposable databases and an empty Redis instance.
func TestBackupRestoreDrillIntegration(t *testing.T) {
	phase := os.Getenv("M3_DRILL_PHASE")
	if phase == "" {
		t.Skip("restore drill not requested")
	}
	db := integrationDB(t)
	ctx := context.Background()
	statePath := os.Getenv("M3_DRILL_STATE")
	if statePath == "" {
		t.Fatal("M3_DRILL_STATE required")
	}
	switch phase {
	case "prepare":
		if err := migrations.Migrate(ctx, db, envconfig.String("MIGRATIONS_DIR", filepath.Join("..", "..", "migrations"))); err != nil {
			t.Fatal(err)
		}
		d := newTestTask(t, db)
		a := apiServer{db: db}
		rec := httptest.NewRecorder()
		a.modelConfig(rec, httptest.NewRequest(http.MethodPut, "/api/v1/model-config", strings.NewReader(`{"base_url":"https://api.deepseek.com","model":"fixture","api_key":"fixture-drill-key"}`)), d.Tenant)
		if rec.Code != 200 {
			t.Fatalf("model fixture: %d %s", rec.Code, rec.Body.String())
		}
		if _, err := db.Exec("UPDATE task_outbox SET sent_at=UTC_TIMESTAMP(6)-INTERVAL 2 MINUTE WHERE tenant_id=? AND task_id=?", d.Tenant, d.Task); err != nil {
			t.Fatal(err)
		}
		reportTask := identity.New(16)
		if _, err := db.Exec("INSERT INTO research_tasks(id,tenant_id,goal,status,event_seq,deadline_at,runtime_version) VALUES(?,?,?,'SUCCEEDED',1,UTC_TIMESTAMP(6)+INTERVAL 5 MINUTE,'m2.1')", reportTask, d.Tenant, "completed fixture"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO research_reports(tenant_id,task_id,markdown,kind) VALUES(?,?,?,'demo')", d.Tenant, reportTask, "restore-fixture-report"); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(d)
		if err := os.WriteFile(statePath, raw, 0600); err != nil {
			t.Fatal(err)
		}
	case "verify":
		raw, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		var d delivery
		if err = json.Unmarshal(raw, &d); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadModel(ctx, db, d.Tenant, 1)
		if err != nil || cfg.Key != "fixture-drill-key" {
			t.Fatalf("restore key: %v", err)
		}
		var report string
		if err = db.QueryRow("SELECT markdown FROM research_reports WHERE tenant_id=? AND markdown=?", d.Tenant, "restore-fixture-report").Scan(&report); err != nil || report != "restore-fixture-report" {
			t.Fatalf("restore report: %v", err)
		}
		if err = (&Runner{store: executionStore(t, db)}).recoverTask(ctx, d); err != nil {
			t.Fatal(err)
		}
		var generation uint32
		if err = db.QueryRow("SELECT generation FROM research_tasks WHERE tenant_id=? AND id=?", d.Tenant, d.Task).Scan(&generation); err != nil || generation != 2 {
			t.Fatalf("generation=%d err=%v", generation, err)
		}
		addr := os.Getenv("TEST_REDIS_ADDR")
		if addr == "" {
			t.Fatal("isolated Redis required")
		}
		opt := asynq.RedisClientOpt{Addr: addr}
		client := asynq.NewClient(opt)
		defer client.Close()
		queue := "m3_restore_" + d.Tenant[:8]
		if err = relay(ctx, db, client, queue); err != nil {
			t.Fatal(err)
		}
		inspect := asynq.NewInspector(opt)
		defer inspect.Close()
		if _, err = inspect.GetTaskInfo(queue, fmt.Sprintf("research:%s:%d", d.Task, generation)); err != nil {
			t.Fatalf("redis redispatch: %v", err)
		}
		r := Runner{store: executionStore(t, db), tool: stubTool{}, model: stubProvider{}}
		server := asynq.NewServer(opt, asynq.Config{Concurrency: 1, Queues: map[string]int{queue: 1}, ShutdownTimeout: 5 * time.Second})
		mux := asynq.NewServeMux()
		mux.HandleFunc(runTask, r.Execute)
		if err = server.Start(mux); err != nil {
			t.Fatal(err)
		}
		defer server.Shutdown()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			var status string
			if err = db.QueryRow("SELECT status FROM research_tasks WHERE tenant_id=? AND id=?", d.Tenant, d.Task).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status == "SUCCEEDED" {
				var count int
				if err = db.QueryRow("SELECT COUNT(*) FROM research_reports WHERE tenant_id=? AND task_id=?", d.Tenant, d.Task).Scan(&count); err != nil || count != 1 {
					t.Fatalf("completed report count=%d err=%v", count, err)
				}
				return
			}
			if status == "FAILED" {
				t.Fatal("recovered task failed")
			}
			time.Sleep(500 * time.Millisecond)
		}
		t.Fatal("recovered task was not completed")
	default:
		t.Fatal("unknown drill phase")
	}
}

func TestTrialQuotaIntegration(t *testing.T) {
	db := integrationDB(t)
	if err := migrations.Migrate(t.Context(), db, envconfig.String("MIGRATIONS_DIR", filepath.Join("..", "..", "migrations"))); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRIAL_MODEL_ENABLED", "true")
	t.Setenv("MODEL_MASTER_KEY_FILE", testMaster(t))
	keyPath := filepath.Join(t.TempDir(), "trial-key")
	if err := os.WriteFile(keyPath, []byte("test-only-trial-key"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRIAL_MODEL_API_KEY_FILE", keyPath)
	tenant := identity.New(16)
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

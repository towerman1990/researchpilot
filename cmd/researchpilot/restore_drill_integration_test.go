package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

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
		if err := migrate(ctx, db, env("MIGRATIONS_DIR", filepath.Join("..", "..", "migrations"))); err != nil {
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
		reportTask := identifier(16)
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
		if err = (&runner{db: db}).recoverTask(ctx, d); err != nil {
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
		r := runner{db: db, tool: stubTool{}, model: stubProvider{}}
		server := asynq.NewServer(opt, asynq.Config{Concurrency: 1, Queues: map[string]int{queue: 1}, ShutdownTimeout: 5 * time.Second})
		mux := asynq.NewServeMux()
		mux.HandleFunc(runTask, r.execute)
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

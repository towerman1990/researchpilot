package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not configured")
	}
	config, err := mysqlConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestTask(t *testing.T, db *sql.DB) delivery {
	t.Helper()
	d := delivery{Tenant: identifier(16), Task: identifier(16), Generation: 1}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "INSERT INTO users(id,email,password_hash) VALUES(?,?,?)", d.Tenant, d.Tenant+"@example.test", "unused"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO research_tasks(id,tenant_id,goal,status,event_seq,deadline_at,runtime_version) VALUES(?,?,?,'QUEUED',1,UTC_TIMESTAMP(6)+INTERVAL 5 MINUTE,'m2.1')", d.Task, d.Tenant, "integration"); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = addOutbox(ctx, tx, d.Tenant, d.Task, 1); err != nil {
		t.Fatal(err)
	}
	if err = appendEvent(ctx, tx, d.Tenant, d.Task, 1, "task_status", map[string]string{"status": "QUEUED"}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestMigrationsIntegration(t *testing.T) {
	db := integrationDB(t)
	dir := env("MIGRATIONS_DIR", filepath.Join("..", "..", "migrations"))
	ctx := context.Background()
	// Reproduce a populated M1 volume with no migration ledger.
	var exists int
	if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='users'").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists == 0 {
		raw, err := os.ReadFile(filepath.Join(dir, "001_m1.sql"))
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(raw), ";") {
			if strings.TrimSpace(statement) != "" {
				if _, err = db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
		}
		d := delivery{Tenant: identifier(16), Task: identifier(16)}
		if _, err = db.Exec("INSERT INTO users VALUES(?,?,?)", d.Tenant, d.Tenant+"@example.test", "unused"); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("INSERT INTO research_tasks(id,tenant_id,goal,status,event_seq,deadline_at) VALUES(?,?,?,'SUCCEEDED',1,UTC_TIMESTAMP(6))", d.Task, d.Tenant, "legacy"); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("INSERT INTO research_reports VALUES(?,?,?)", d.Tenant, d.Task, "legacy report"); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("UPDATE research_tasks SET status='SUCCEEDED' WHERE id=?", d.Task); err != nil {
			t.Fatal(err)
		}
	}
	// Tests use a dedicated empty database. Re-running must preserve data and ledger.
	if err := migrate(ctx, db, dir); err != nil {
		t.Fatal(err)
	}
	var legacy string
	if err := db.QueryRow("SELECT markdown FROM research_reports WHERE kind='demo' LIMIT 1").Scan(&legacy); err != nil || legacy != "legacy report" {
		t.Fatalf("legacy report lost: %v", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE dirty=false").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 6 {
		t.Fatalf("ledger count=%d", count)
	}
	if err := migrate(ctx, db, dir); err != nil {
		t.Fatal(err)
	}
}

func TestDurableExecutionIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	r := runner{db: db, tool: stubTool{}, model: stubProvider{}}
	d := newTestTask(t, db)
	state, ok, err := r.claim(ctx, d)
	if err != nil || !ok {
		t.Fatalf("claim=%v %v", ok, err)
	}
	if _, ok, err = r.claim(ctx, d); err != nil || ok {
		t.Fatalf("duplicate claim=%v %v", ok, err)
	}
	e := execution{runner: r, delivery: d, epoch: state.epoch}

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
	if err = e.mutate(ctx, func(*sql.Tx, taskRow) error { return nil }); !errors.Is(err, errLeaseLost) {
		t.Fatalf("stale worker err=%v", err)
	}
	replacement := execution{runner: r, delivery: d, epoch: next.epoch}
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
	if err = r.executeDurable(ctx, d, next); err != nil {
		t.Fatal(err)
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
	r := runner{db: db}
	state, ok, err := r.claim(context.Background(), d)
	if err != nil || !ok {
		t.Fatal(err)
	}
	e := execution{runner: r, delivery: d, epoch: state.epoch}
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

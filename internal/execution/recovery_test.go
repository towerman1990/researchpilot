package execution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hibiken/asynq"

	"researchpilot/internal/common/identity"
	"researchpilot/internal/repository"
)

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
	if err := e.Runner.recoverTask(ctx, e.delivery); err != nil {
		t.Fatal(err)
	}
	d := e.delivery
	d.Generation++
	state, ok, err := e.Runner.claim(ctx, d)
	if err != nil || !ok {
		t.Fatal(err)
	}
	next := execution{Runner: e.Runner, delivery: d, epoch: state.epoch}
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
	if err = next.mutate(ctx, func(*repository.ExecutionTx, taskRow) error { t.Error("cancelled completion entered"); return nil }); !errors.Is(err, errLeaseLost) {
		t.Fatal(err)
	}
	if err = next.failed(ctx, context.Canceled); err != nil {
		t.Fatal(err)
	}
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
	if err := e.Runner.recoverTask(ctx, e.delivery); err != nil {
		t.Fatal(err)
	}
	d := e.delivery
	d.Generation++
	state, ok, err := e.Runner.claim(ctx, d)
	if err != nil || !ok {
		t.Fatal(err)
	}
	next := execution{Runner: e.Runner, delivery: d, epoch: state.epoch}
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
	queue := "rp_test_" + identity.New(16)
	opt := asynq.RedisClientOpt{Addr: address}
	client := asynq.NewClient(opt)
	defer client.Close()
	inspector := asynq.NewInspector(opt)
	defer inspector.Close()
	defer inspector.DeleteQueue(queue, true)
	dispatch := func(generation uint32) {
		t.Helper()
		id := fmt.Sprintf("research:%s:%d", d.Task, generation)
		// A prior fixture may have queued work ahead of this task. The relay claims at most 16 per batch.
		for i := 0; i < 8; i++ {
			if err := relay(ctx, db, client, queue); err != nil {
				t.Fatal(err)
			}
			_, err := inspector.GetTaskInfo(queue, id)
			if err == nil {
				return
			}
			if !errors.Is(err, asynq.ErrTaskNotFound) {
				t.Fatal(err)
			}
		}
		t.Fatalf("generation %d was not dispatched", generation)
	}
	dispatch(1)
	if _, err := inspector.DeleteAllPendingTasks(queue); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE task_outbox SET sent_at=UTC_TIMESTAMP(6)-INTERVAL 61 SECOND WHERE task_id=? AND generation=1", d.Task); err != nil {
		t.Fatal(err)
	}
	r := Runner{store: executionStore(t, db)}
	if err := r.recoverTask(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := r.claim(ctx, d); err != nil || claimed {
		t.Fatal("stale generation accepted", err)
	}
	dispatch(2)
}

func TestRecoveryContinuesAfterLockedTaskIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := t.Context()
	first, next := newTestTask(t, db), newTestTask(t, db)
	if first.Task > next.Task {
		first, next = next, first
	}
	for _, item := range []delivery{first, next} {
		if _, err := db.ExecContext(ctx, "UPDATE task_outbox SET available_at=UTC_TIMESTAMP(6)-INTERVAL 3 MINUTE,sent_at=UTC_TIMESTAMP(6)-INTERVAL 2 MINUTE WHERE tenant_id=? AND task_id=?", item.Tenant, item.Task); err != nil {
			t.Fatal(err)
		}
	}
	worker := Runner{store: executionStore(t, db)}
	if delayed, err := worker.store.HasDelayedTasks(ctx, 120); err != nil || !delayed {
		t.Fatalf("expected queue backlog: delayed=%v err=%v", delayed, err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE research_tasks SET goal=goal WHERE id=?", first.Task); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.Recover(ctx, ""); err == nil || !strings.Contains(err.Error(), first.Task) {
		t.Fatalf("expected recovery error identifying locked task, got %v", err)
	}
	var generation uint32
	if err := db.QueryRowContext(ctx, "SELECT generation FROM research_tasks WHERE id=?", next.Task).Scan(&generation); err != nil || generation != 2 {
		t.Fatalf("next task did not recover: generation=%d err=%v", generation, err)
	}
}

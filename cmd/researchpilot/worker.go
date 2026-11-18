package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/hibiken/asynq"
)

const runTask = "research:run:v1"

type delivery struct {
	Tenant     string `json:"tenant_id"`
	Task       string `json:"task_id"`
	Generation uint32 `json:"generation"`
}

// These dependencies keep the local stub replaceable without changing the worker's persistence.
type reportProvider interface {
	Report(context.Context, string, string) (string, error)
}

type researchTool interface {
	Collect(context.Context, string) (string, error)
}

type stubTool struct{}
type stubProvider struct{}

func (stubTool) Collect(ctx context.Context, _ string) (string, error) {
	timer := time.NewTimer(1200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "本地固定工具结果，未访问外部资料。", nil
	}
}

func (stubProvider) Report(_ context.Context, goal, observation string) (string, error) {
	return fmt.Sprintf("# M1 演示报告\n\n研究目标：%s\n\n## 工具结果\n\n%s\n\n## 结果说明\n\n本报告用于验证任务流转。没有外部证据或引用。", goal, observation), nil
}

type runner struct {
	db        *sql.DB
	model     reportProvider
	tool      researchTool
	modelHTTP *http.Client
	toolsHTTP *http.Client
}

func runWorker(ctx context.Context, db *sql.DB, address string) error {
	opt := asynq.RedisClientOpt{Addr: address, Password: os.Getenv("REDIS_PASSWORD")}
	client := asynq.NewClient(opt)
	defer client.Close()

	queue := env("RESEARCH_QUEUE", "rp_local_research")
	server := asynq.NewServer(opt, asynq.Config{Concurrency: 4, Queues: map[string]int{queue: 1}, ShutdownTimeout: 8 * time.Second})
	r := runner{db: db, model: stubProvider{}, tool: stubTool{}}
	mux := asynq.NewServeMux()
	mux.HandleFunc(runTask, r.execute)
	if err := server.Start(mux); err != nil {
		return err
	}
	defer server.Shutdown()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	scan := time.NewTicker(5 * time.Second)
	defer scan.Stop()
	retention := time.NewTicker(time.Hour)
	defer retention.Stop()
	log.Printf("worker listening on %s", queue)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-retention.C:
			if err := pruneEvents(ctx, db); err != nil {
				log.Printf("event retention: %T", err)
			}
		case <-tick.C:
			if err := relay(ctx, db, client, queue); err != nil && ctx.Err() == nil {
				log.Printf("outbox relay: %v", err)
			}
		case <-scan.C:
			if err := r.recover(ctx); err != nil && ctx.Err() == nil {
				log.Printf("task recovery: %v", err)
			}
		}
	}
}

func appendEvent(ctx context.Context, tx *sql.Tx, tenant, id string, seq uint64, kind string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO task_events(tenant_id,task_id,seq,event_type,payload) VALUES(?,?,?,?,?)", tenant, id, seq, kind, string(raw))
	return err
}

func addOutbox(ctx context.Context, tx *sql.Tx, tenant, id string, generation uint32) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO task_outbox(tenant_id,task_id,generation) VALUES(?,?,?)", tenant, id, generation)
	return err
}

func claimOutbox(ctx context.Context, db *sql.DB) (d delivery, epoch uint32, err error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return d, 0, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, "SELECT tenant_id,task_id,generation,claim_epoch FROM task_outbox WHERE sent_at IS NULL AND available_at<=UTC_TIMESTAMP(6) AND (claim_until IS NULL OR claim_until<=UTC_TIMESTAMP(6)) ORDER BY available_at LIMIT 1 FOR UPDATE SKIP LOCKED").
		Scan(&d.Tenant, &d.Task, &d.Generation, &epoch)
	if err != nil {
		return d, 0, err
	}
	epoch++
	_, err = tx.ExecContext(ctx, "UPDATE task_outbox SET claim_epoch=?,claim_until=UTC_TIMESTAMP(6)+INTERVAL 15 SECOND WHERE tenant_id=? AND task_id=? AND generation=?", epoch, d.Tenant, d.Task, d.Generation)
	if err != nil {
		return d, 0, err
	}
	return d, epoch, tx.Commit()
}

func relay(ctx context.Context, db *sql.DB, client *asynq.Client, queue string) error {
	for i := 0; i < 16; i++ {
		d, epoch, err := claimOutbox(ctx, db)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		raw, err := json.Marshal(d)
		if err != nil {
			return err
		}
		_, err = client.EnqueueContext(ctx, asynq.NewTask(runTask, raw), asynq.Queue(queue),
			asynq.TaskID(fmt.Sprintf("research:%s:%d", d.Task, d.Generation)), asynq.MaxRetry(2),
			asynq.Timeout(6*time.Minute))
		if err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
			return err // The claim expires and can be retried.
		}
		_, err = db.ExecContext(ctx, "UPDATE task_outbox SET sent_at=UTC_TIMESTAMP(6),claim_until=NULL WHERE tenant_id=? AND task_id=? AND generation=? AND claim_epoch=?", d.Tenant, d.Task, d.Generation, epoch)
		if err != nil {
			return err
		}
	}
	return nil
}

type taskRow struct {
	status, goal             string
	generation, epoch        uint32
	seq                      uint64
	pastDeadline, validLease bool
	retries                  uint16
	remaining                time.Duration
	modelVersion             uint32
}

func lockTask(ctx context.Context, tx *sql.Tx, d delivery) (t taskRow, err error) {
	var seconds float64
	err = tx.QueryRowContext(ctx, "SELECT status,goal,generation,lease_epoch,event_seq,deadline_at<=UTC_TIMESTAMP(6),COALESCE(lease_until>UTC_TIMESTAMP(6),0),retry_count,TIMESTAMPDIFF(MICROSECOND,UTC_TIMESTAMP(6),deadline_at)/1000000.0,COALESCE(model_version,0) FROM research_tasks WHERE tenant_id=? AND id=? FOR UPDATE", d.Tenant, d.Task).
		Scan(&t.status, &t.goal, &t.generation, &t.epoch, &t.seq, &t.pastDeadline, &t.validLease, &t.retries, &seconds, &t.modelVersion)
	t.remaining = time.Duration(seconds * float64(time.Second))
	return t, err
}

func (r runner) claim(ctx context.Context, d delivery) (taskRow, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return taskRow{}, false, err
	}
	defer tx.Rollback()
	t, err := lockTask(ctx, tx, d)
	if errors.Is(err, sql.ErrNoRows) {
		return t, false, nil
	}
	if err != nil {
		return t, false, err
	}
	if t.status != "QUEUED" || t.generation != d.Generation || t.pastDeadline {
		return t, false, nil
	}

	t.epoch++
	t.seq++
	if _, err = tx.ExecContext(ctx, "UPDATE research_tasks SET status='RUNNING',phase='planning',lease_epoch=?,event_seq=?,lease_until=UTC_TIMESTAMP(6)+INTERVAL 30 SECOND WHERE tenant_id=? AND id=?", t.epoch, t.seq, d.Tenant, d.Task); err != nil {
		return t, false, err
	}
	if err = appendEvent(ctx, tx, d.Tenant, d.Task, t.seq, "task_status", map[string]string{"status": "RUNNING", "phase": "planning"}); err != nil {
		return t, false, err
	}
	return t, true, tx.Commit()
}

func (r runner) execute(ctx context.Context, task *asynq.Task) error {
	var d delivery
	if err := json.Unmarshal(task.Payload(), &d); err != nil || len(d.Task) != 32 || len(d.Tenant) != 32 || d.Generation == 0 {
		return asynq.SkipRetry
	}
	t, claimed, err := r.claim(ctx, d)
	if err != nil || !claimed {
		return err
	}

	return r.executeDurable(ctx, d, t)
}

func (r runner) recover(ctx context.Context) error {
	const query = "SELECT t.tenant_id,t.id FROM research_tasks t LEFT JOIN task_outbox o ON o.tenant_id=t.tenant_id AND o.task_id=t.id AND o.generation=t.generation WHERE (t.status IN ('QUEUED','RUNNING') AND t.deadline_at<=UTC_TIMESTAMP(6)) OR (t.status='RUNNING' AND t.lease_until<=UTC_TIMESTAMP(6)) OR (t.status='QUEUED' AND o.sent_at<=UTC_TIMESTAMP(6)-INTERVAL 60 SECOND) OR (t.status='CANCELLING' AND t.lease_until<=UTC_TIMESTAMP(6)) OR (t.status='RETRY_WAIT' AND (t.retry_at<=UTC_TIMESTAMP(6) OR t.deadline_at<=UTC_TIMESTAMP(6))) LIMIT 50"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	var tasks []delivery
	for rows.Next() {
		var d delivery
		if err = rows.Scan(&d.Tenant, &d.Task); err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, d := range tasks {
		if err = r.recoverTask(ctx, d); err != nil {
			return err
		}
	}
	return nil
}

func (r runner) recoverTask(ctx context.Context, d delivery) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t, err := lockTask(ctx, tx, d)
	if err != nil {
		return err
	}
	if terminal(t.status) {
		return nil
	}
	if t.status == "CANCELLING" {
		if t.validLease {
			return nil
		}
		if err = finishTask(ctx, tx, d, t, "CANCELLED", ""); err != nil {
			return err
		}
		return tx.Commit()
	}
	if t.pastDeadline {
		if err = finishTask(ctx, tx, d, t, "FAILED", "deadline_exceeded"); err != nil {
			return err
		}
		return tx.Commit()
	}
	if t.status == "RUNNING" && t.validLease {
		return nil
	}
	if t.status == "RUNNING" {
		if t.retries >= 2 {
			if err = finishTask(ctx, tx, d, t, "FAILED", "retry_exhausted"); err != nil {
				return err
			}
			return tx.Commit()
		}
		if err = settleReservations(ctx, tx, d); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE research_tasks SET retry_count=retry_count+1 WHERE tenant_id=? AND id=?", d.Tenant, d.Task); err != nil {
			return err
		}
	}
	if t.status == "RETRY_WAIT" {
		var due bool
		if err = tx.QueryRowContext(ctx, "SELECT retry_at<=UTC_TIMESTAMP(6) FROM research_tasks WHERE tenant_id=? AND id=?", d.Tenant, d.Task).Scan(&due); err != nil {
			return err
		}
		if !due {
			return nil
		}
	}
	if t.status == "QUEUED" {
		var due bool
		err = tx.QueryRowContext(ctx, "SELECT COALESCE(sent_at<=UTC_TIMESTAMP(6)-INTERVAL 60 SECOND,0) FROM task_outbox WHERE tenant_id=? AND task_id=? AND generation=?", d.Tenant, d.Task, t.generation).Scan(&due)
		if err != nil {
			return err
		}
		if !due {
			return nil
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE research_tasks SET status='QUEUED',phase=NULL,generation=?,event_seq=?,lease_until=NULL WHERE tenant_id=? AND id=?", t.generation+1, t.seq+1, d.Tenant, d.Task); err != nil {
		return err
	}
	if err = addOutbox(ctx, tx, d.Tenant, d.Task, t.generation+1); err != nil {
		return err
	}
	if err = appendEvent(ctx, tx, d.Tenant, d.Task, t.seq+1, "task_status", map[string]string{"status": "QUEUED", "reason": "dispatch_recovered"}); err != nil {
		return err
	}
	return tx.Commit()
}

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var errLeaseLost = errors.New("execution lease lost")
var errBudget = errors.New("token budget exhausted")

type execution struct {
	runner
	delivery
	epoch uint32
}

// mutate serializes every execution write with cancellation, takeover and finalization.
func (e execution) mutate(ctx context.Context, f func(*sql.Tx, taskRow) error) error {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t, err := lockTask(ctx, tx, e.delivery)
	if err != nil {
		return err
	}
	if t.status != "RUNNING" || t.epoch != e.epoch || !t.validLease || t.pastDeadline {
		return errLeaseLost
	}
	if err = f(tx, t); err != nil {
		return err
	}
	return tx.Commit()
}

func eventUpdate(ctx context.Context, tx *sql.Tx, d delivery, t taskRow, kind string, payload any) error {
	_, err := tx.ExecContext(ctx, "UPDATE research_tasks SET event_seq=? WHERE tenant_id=? AND id=?", t.seq+1, d.Tenant, d.Task)
	if err != nil {
		return err
	}
	return appendEvent(ctx, tx, d.Tenant, d.Task, t.seq+1, kind, payload)
}

func (e execution) keepAlive(ctx context.Context, cancel context.CancelFunc, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := e.db.ExecContext(ctx, "UPDATE research_tasks SET lease_until=LEAST(deadline_at,UTC_TIMESTAMP(6)+INTERVAL 30 SECOND) WHERE tenant_id=? AND id=? AND status='RUNNING' AND lease_epoch=? AND lease_until>UTC_TIMESTAMP(6) AND deadline_at>UTC_TIMESTAMP(6)", e.Tenant, e.Task, e.epoch)
			if err != nil {
				cancel()
				return
			}
			count, err := result.RowsAffected()
			if err != nil || count != 1 {
				cancel()
				return
			}
		}
	}
}

func (e execution) checkpoint(ctx context.Context, key string, run func(context.Context) (string, error)) (string, error) {
	var saved []byte
	err := e.db.QueryRowContext(ctx, "SELECT result_json FROM task_checkpoints WHERE tenant_id=? AND task_id=? AND step_key=?", e.Tenant, e.Task, key).Scan(&saved)
	if err == nil {
		var value string
		if err = json.Unmarshal(saved, &value); err != nil {
			return "", err
		}
		return value, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var attempt uint32
	err = e.mutate(ctx, func(tx *sql.Tx, t taskRow) error {
		if _, err := tx.ExecContext(ctx, "UPDATE execution_attempts SET status='ABANDONED',finished_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND task_id=? AND step_key=? AND status='STARTED'", e.Tenant, e.Task, key); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(attempt_no),0)+1 FROM execution_attempts WHERE tenant_id=? AND task_id=? AND step_key=?", e.Tenant, e.Task, key).Scan(&attempt); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO execution_attempts(tenant_id,task_id,step_key,attempt_no,lease_epoch,status) VALUES(?,?,?,?,?,'STARTED')", e.Tenant, e.Task, key, attempt, e.epoch)
		if err != nil {
			return err
		}
		return eventUpdate(ctx, tx, e.delivery, t, "step_started", map[string]any{"step_key": key, "tool_call_id": key, "attempt_no": attempt})
	})
	if err != nil {
		return "", err
	}
	value, runErr := run(ctx)
	if runErr != nil {
		return "", runErr
	}
	err = e.mutate(ctx, func(tx *sql.Tx, t taskRow) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO task_checkpoints(tenant_id,task_id,step_key,result_json) VALUES(?,?,?,?)", e.Tenant, e.Task, key, string(raw))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE execution_attempts SET status='SUCCEEDED',finished_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND task_id=? AND step_key=? AND attempt_no=? AND lease_epoch=?", e.Tenant, e.Task, key, attempt, e.epoch)
		if err != nil {
			return err
		}
		status, summary := "SUCCEEDED", "步骤结果已保存"
		var observation struct{ Error string }
		if json.Unmarshal([]byte(value), &observation) == nil && observation.Error != "" {
			status = "FAILED"
			summary = observation.Error
		}
		return eventUpdate(ctx, tx, e.delivery, t, "tool_finished", map[string]any{"step_key": key, "tool_call_id": key, "attempt_no": attempt, "status": status, "summary": summary})
	})
	return value, err
}

// Reservations survive an ambiguous upstream response. Recovery charges them
// conservatively before another worker can make a replacement call.
func settleReservations(ctx context.Context, tx *sql.Tx, d delivery) error {
	_, err := tx.ExecContext(ctx, "UPDATE research_tasks SET tokens_used=tokens_used+tokens_reserved,tokens_reserved=0 WHERE tenant_id=? AND id=?", d.Tenant, d.Task)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE model_attempts SET status='ESTIMATED',charged_tokens=reserved_tokens,settled_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND task_id=? AND status='RESERVED'", d.Tenant, d.Task)
	return err
}

func (e execution) reserve(ctx context.Context, tokens uint32) (string, error) {
	id := identifier(16)
	err := e.mutate(ctx, func(tx *sql.Tx, _ taskRow) error {
		result, err := tx.ExecContext(ctx, "UPDATE research_tasks SET tokens_reserved=tokens_reserved+? WHERE tenant_id=? AND id=? AND tokens_used+tokens_reserved+?<=token_limit", tokens, e.Tenant, e.Task, tokens)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return errBudget
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO model_attempts(tenant_id,task_id,attempt_id,lease_epoch,status,reserved_tokens) VALUES(?,?,?,?,'RESERVED',?)", e.Tenant, e.Task, id, e.epoch, tokens)
		return err
	})
	return id, err
}

func (e execution) settle(ctx context.Context, id string, used uint32, estimated bool) error {
	return e.mutate(ctx, func(tx *sql.Tx, t taskRow) error {
		var reserved uint32
		var status string
		if err := tx.QueryRowContext(ctx, "SELECT reserved_tokens,status FROM model_attempts WHERE tenant_id=? AND task_id=? AND attempt_id=? AND lease_epoch=?", e.Tenant, e.Task, id, e.epoch).Scan(&reserved, &status); err != nil {
			return err
		}
		if status != "RESERVED" {
			return nil
		}
		status = "ACTUAL"
		if estimated {
			used = reserved
			status = "ESTIMATED"
		}
		_, err := tx.ExecContext(ctx, "UPDATE model_attempts SET status=?,charged_tokens=?,settled_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND task_id=? AND attempt_id=?", status, used, e.Tenant, e.Task, id)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE research_tasks SET tokens_reserved=tokens_reserved-?,tokens_used=tokens_used+? WHERE tenant_id=? AND id=?", reserved, used, e.Tenant, e.Task)
		if err != nil {
			return err
		}
		return eventUpdate(ctx, tx, e.delivery, t, "usage_updated", map[string]any{"charged_tokens": used, "estimated": estimated})
	})
}

func finishTask(ctx context.Context, tx *sql.Tx, d delivery, t taskRow, status, code string) error {
	if err := settleReservations(ctx, tx, d); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "UPDATE execution_attempts SET status='ABANDONED',finished_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND task_id=? AND status='STARTED'", d.Tenant, d.Task)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE research_tasks SET status=?,phase=NULL,lease_until=NULL,finished_at=UTC_TIMESTAMP(6),error_code=NULLIF(?,'') WHERE tenant_id=? AND id=?", status, code, d.Tenant, d.Task)
	if err != nil {
		return err
	}
	return eventUpdate(ctx, tx, d, t, "task_status", map[string]string{"status": status, "error_code": code})
}

func (e execution) failed(ctx context.Context, cause error) error {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t, err := lockTask(ctx, tx, e.delivery)
	if err != nil {
		return err
	}
	if t.epoch != e.epoch || terminal(t.status) {
		return nil
	}
	if t.status == "CANCELLING" {
		err = finishTask(ctx, tx, e.delivery, t, "CANCELLED", "")
	} else if !t.validLease {
		return nil
	} else if t.pastDeadline {
		err = finishTask(ctx, tx, e.delivery, t, "FAILED", "deadline_exceeded")
	} else if errors.Is(cause, errBudget) {
		err = finishTask(ctx, tx, e.delivery, t, "FAILED", "budget_exhausted")
	} else if t.retries >= 2 {
		err = finishTask(ctx, tx, e.delivery, t, "FAILED", "retry_exhausted")
	} else {
		if err = settleReservations(ctx, tx, e.delivery); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE research_tasks SET status='RETRY_WAIT',phase=NULL,lease_until=NULL,retry_count=retry_count+1,retry_at=UTC_TIMESTAMP(6)+INTERVAL 2 SECOND,error_code='execution_interrupted' WHERE tenant_id=? AND id=?", e.Tenant, e.Task)
		if err == nil {
			err = eventUpdate(ctx, tx, e.delivery, t, "task_status", map[string]string{"status": "RETRY_WAIT", "error_code": "execution_interrupted"})
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r runner) executeDurable(ctx context.Context, d delivery, t taskRow) error {
	execCtx, cancel := context.WithTimeout(ctx, t.remaining)
	e := execution{runner: r, delivery: d, epoch: t.epoch}
	done := make(chan struct{})
	go e.keepAlive(execCtx, cancel, done)
	defer func() { cancel(); <-done }()
	if t.modelVersion > 0 {
		err := e.executeResearch(execCtx, t)
		if err == nil {
			return nil
		}
		cleanup, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancelCleanup()
		return e.failed(cleanup, err)
	}
	observation, err := e.checkpoint(execCtx, "collect", func(ctx context.Context) (string, error) { return r.tool.Collect(ctx, t.goal) })
	if err == nil {
		var report string
		report, err = e.checkpoint(execCtx, "report", func(ctx context.Context) (string, error) {
			// Stub output is deterministic and incurs zero external token usage.
			id, err := e.reserve(ctx, 1)
			if err != nil {
				return "", err
			}
			value, err := r.model.Report(ctx, t.goal, observation)
			if err != nil {
				return "", err
			}
			if err = e.settle(ctx, id, 0, false); err != nil {
				return "", err
			}
			return value, nil
		})
		if err == nil {
			err = e.mutate(execCtx, func(tx *sql.Tx, current taskRow) error {
				_, err := tx.ExecContext(execCtx, "INSERT INTO research_reports(tenant_id,task_id,markdown) VALUES(?,?,?)", d.Tenant, d.Task, report)
				if err != nil {
					return err
				}
				_, err = tx.ExecContext(execCtx, "UPDATE research_tasks SET status='SUCCEEDED',phase=NULL,lease_until=NULL,finished_at=UTC_TIMESTAMP(6),error_code=NULL WHERE tenant_id=? AND id=?", d.Tenant, d.Task)
				if err != nil {
					return err
				}
				return eventUpdate(execCtx, tx, d, current, "report_ready", map[string]string{"status": "SUCCEEDED"})
			})
		}
	}
	if err == nil {
		return nil
	}
	cleanup, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelCleanup()
	if failureErr := e.failed(cleanup, err); failureErr != nil {
		return fmt.Errorf("settle execution failure: %w", failureErr)
	}
	return nil
}

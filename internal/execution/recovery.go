package execution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"researchpilot/internal/repository"
	taskservice "researchpilot/internal/task"
)

// Recover handles one ordered batch. Keep its next cursor even if an individual task fails.
func (r Runner) Recover(ctx context.Context, after string) (string, error) {
	const batchSize = 50
	tasks, err := r.store.RecoveryCandidates(ctx, after, batchSize)
	if err != nil {
		return after, err
	}

	var failures []error
	for _, d := range tasks {
		if err := ctx.Err(); err != nil {
			return after, errors.Join(append(failures, err)...)
		}
		after = d.Task
		taskCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := r.recoverTask(taskCtx, d)
		cancel()
		if err != nil {
			failures = append(failures, fmt.Errorf("task %s: %w", d.Task, err))
		}
	}
	if len(tasks) < batchSize {
		after = ""
	}
	return after, errors.Join(failures...)
}

func (r Runner) recoverTask(ctx context.Context, d delivery) error {
	return r.store.TransactExecution(ctx, func(tx *repository.ExecutionTx) error {
		locked, err := lockTask(ctx, tx, d)
		if err != nil {
			return err
		}

		var status, code string
		switch {
		case taskservice.Terminal(locked.status):
			return nil
		case locked.status == "CANCELLING":
			if locked.validLease {
				return nil
			}
			status = "CANCELLED"
		case locked.pastDeadline:
			status, code = "FAILED", "deadline_exceeded"
		case locked.status == "RUNNING" && locked.validLease:
			return nil
		case locked.status == "RUNNING" && locked.retries >= 2:
			status, code = "FAILED", "retry_exhausted"
		}
		if status != "" {
			return finishTask(ctx, tx, d, locked, status, code)
		}
		switch locked.status {
		case "RUNNING":
			if err := tx.SettleReservations(ctx, d.Tenant, d.Task); err != nil {
				return err
			}
			if err := tx.IncreaseRetry(ctx, d.Tenant, d.Task); err != nil {
				return err
			}
		case "RETRY_WAIT":
			due, err := tx.RetryDue(ctx, d.Tenant, d.Task)
			if err != nil || !due {
				return err
			}
		case "QUEUED":
			expired, err := tx.DispatchExpired(ctx, d.Tenant, d.Task, locked.generation)
			if err != nil || !expired {
				return err
			}
		}
		generation, seq := locked.generation+1, locked.seq+1
		if err := tx.RequeueTask(ctx, d.Tenant, d.Task, generation, seq); err != nil {
			return err
		}
		if err := tx.InsertTaskOutbox(ctx, d.Tenant, d.Task, generation); err != nil {
			return err
		}
		return appendEvent(ctx, tx, d.Tenant, d.Task, seq, "task_status", map[string]string{"status": "QUEUED", "reason": "dispatch_recovered"})
	})
}

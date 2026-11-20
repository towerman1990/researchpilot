package execution

import (
	"context"
	"errors"

	"researchpilot/internal/repository"
	taskservice "researchpilot/internal/task"
)

func finishTask(ctx context.Context, tx *repository.ExecutionTx, d delivery, t taskRow, status, code string) error {
	if err := tx.SettleReservations(ctx, d.Tenant, d.Task); err != nil {
		return err
	}
	if err := tx.AbandonTaskAttempts(ctx, d.Tenant, d.Task); err != nil {
		return err
	}
	if err := tx.SetTaskFinished(ctx, d.Tenant, d.Task, status, code); err != nil {
		return err
	}
	if status == "FAILED" {
		published, err := publishPartial(ctx, tx, d, t, code)
		if err != nil {
			return err
		}
		if published {
			return nil
		}
	}
	return eventUpdate(ctx, tx, d, t, "task_status", map[string]string{"status": status, "error_code": code})
}

func (e execution) failed(ctx context.Context, cause error) error {
	return e.store.TransactExecution(ctx, func(tx *repository.ExecutionTx) error {
		locked, err := lockTask(ctx, tx, e.delivery)
		if err != nil {
			return err
		}
		if locked.epoch != e.epoch || taskservice.Terminal(locked.status) {
			return nil
		}

		var status, code string
		switch {
		case locked.status == "CANCELLING":
			status = "CANCELLED"
		case !locked.validLease:
			return nil
		case locked.pastDeadline:
			status, code = "FAILED", "deadline_exceeded"
		case errors.Is(cause, ErrBudget):
			status, code = "FAILED", "budget_exhausted"
		case permanentFailure(cause):
			status, code = "FAILED", safeFailureCode(cause)
		case locked.retries >= 2:
			status, code = "FAILED", "retry_exhausted"
		}
		if status != "" {
			return finishTask(ctx, tx, e.delivery, locked, status, code)
		}
		if err := tx.SettleReservations(ctx, e.Tenant, e.Task); err != nil {
			return err
		}
		if err := tx.SetRetryWait(ctx, e.Tenant, e.Task); err != nil {
			return err
		}
		return eventUpdate(ctx, tx, e.delivery, locked, "task_status", map[string]string{"status": "RETRY_WAIT", "error_code": "execution_interrupted"})
	})
}

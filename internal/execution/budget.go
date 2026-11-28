package execution

import (
	"context"
	"errors"

	"researchpilot/internal/common/identity"
	"researchpilot/internal/model"
	"researchpilot/internal/repository"
)

var ErrBudget = errors.New("token budget exhausted")

// Reserve must run inside the transaction holding the task lease lock.
func Reserve(ctx context.Context, store *repository.ExecutionTx, tenant, task, key string, epoch, tokens uint32, keep ...uint32) (string, error) {
	id := identity.New(16)
	var logical *string
	var attempt *uint32
	if key != "" {
		count, err := store.AttemptCount(ctx, tenant, task, key)
		if err != nil {
			return "", err
		}
		if count >= 3 {
			return "", errors.New("model_retry_exhausted")
		}
		logical = &key
		next := count + 1
		attempt = &next
	}
	count, err := store.Reserve(ctx, tenant, task, tokens, keep...)
	if err != nil {
		return "", err
	}
	if count != 1 {
		return "", ErrBudget
	}
	if err = store.InsertAttempt(ctx, tenant, task, id, epoch, tokens, logical, attempt); err != nil {
		return "", err
	}
	return id, nil
}

// Settle returns the charged amount and whether a reserved attempt was updated.
// It must run under the same task lease lock as Reserve.
func Settle(ctx context.Context, store *repository.ExecutionTx, tenant, task, id string, epoch, used uint32, estimated bool, failure string, breakdown *model.TokenBreakdown) (uint32, bool, error) {
	record, err := store.Attempt(ctx, tenant, task, id, epoch)
	if err != nil {
		return 0, false, err
	}
	if record.Status != "RESERVED" {
		return 0, false, nil
	}
	status := "ACTUAL"
	var input, output *uint32
	if estimated {
		used = record.ReservedTokens
		status = "ESTIMATED"
	} else if breakdown != nil {
		input, output = &breakdown.Input, &breakdown.Output
	}
	if err := store.SettleAttempt(ctx, tenant, task, id, status, failure, used, record.ReservedTokens, input, output); err != nil {
		return 0, false, err
	}
	return used, true, nil
}

type modelCallKey struct{}

func (e execution) reserve(ctx context.Context, tokens uint32, keep ...uint32) (string, error) {
	var id string
	err := e.mutate(ctx, func(tx *repository.ExecutionTx, _ taskRow) error {
		key, _ := ctx.Value(modelCallKey{}).(string)
		var err error
		id, err = Reserve(ctx, tx, e.Tenant, e.Task, key, e.epoch, tokens, keep...)
		return err
	})
	return id, err
}

func (e execution) settle(ctx context.Context, id string, used uint32, estimated bool) error {
	return e.settleOutcome(ctx, id, used, estimated, "", nil)
}
func (e execution) settleOutcome(ctx context.Context, id string, used uint32, estimated bool, failure string, breakdown *model.TokenBreakdown) error {
	return e.mutate(ctx, func(tx *repository.ExecutionTx, t taskRow) error {
		charged, updated, err := Settle(ctx, tx, e.Tenant, e.Task, id, e.epoch, used, estimated, failure, breakdown)
		if err != nil {
			return err
		}
		if !updated {
			return nil
		}
		used = charged
		return eventUpdate(ctx, tx, e.delivery, t, "usage_updated", map[string]any{"charged_tokens": used, "estimated": estimated})
	})
}

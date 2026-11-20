// Package task contains task lifecycle and event services.
package task

import (
	"context"

	"researchpilot/internal/common/envconfig"
	"researchpilot/internal/repository"
	"researchpilot/internal/repository/model"
)

type EventRetentionStore interface {
	ExpiredEventTasks(context.Context, int) ([]model.Delivery, error)
	Write(context.Context, string, bool, func(repository.WriteTx) error) error
}

// PruneEvents updates the replay floor and removes events under the same task lock.
func PruneEvents(ctx context.Context, store EventRetentionStore) error {
	tasks, err := store.ExpiredEventTasks(ctx, envconfig.Int("EVENT_RETENTION_DAYS", 30, 1, 10000))
	if err != nil {
		return err
	}
	for _, task := range tasks {
		err = store.Write(ctx, task.Tenant, false, func(tx repository.WriteTx) error {
			current, err := tx.LockTask(task.Tenant, task.Task)
			if err != nil {
				return err
			}
			if !Terminal(current.Status) {
				return nil
			}
			return tx.PruneTaskEvents(task.Tenant, task.Task)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Package task contains task lifecycle and event services.
package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"

	"researchpilot/internal/repository/model"
)

const RunTaskName = "research:run:v1"

type OutboxStore interface {
	ClaimOutbox(context.Context) (model.OutboxClaim, error)
	MarkOutboxSent(context.Context, model.OutboxClaim) error
}

func RelayOutbox(ctx context.Context, store OutboxStore, client *asynq.Client, queue string) error {
	for i := 0; i < 16; i++ {
		claim, err := store.ClaimOutbox(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		raw, err := json.Marshal(claim.Delivery)
		if err != nil {
			return err
		}
		_, err = client.EnqueueContext(ctx, asynq.NewTask(RunTaskName, raw), asynq.Queue(queue),
			asynq.TaskID(fmt.Sprintf("research:%s:%d", claim.Task, claim.Generation)), asynq.MaxRetry(2), asynq.Timeout(6*time.Minute))
		if err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
			return err
		}
		if err := store.MarkOutboxSent(ctx, claim); err != nil {
			return err
		}
	}
	return nil
}

package execution

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"researchpilot/internal/common/identity"
	"researchpilot/internal/common/textutil"
	"researchpilot/internal/repository"
)

func (e execution) checkpoint(ctx context.Context, key string, run func(context.Context) (string, error)) (string, error) {
	saved, err := e.store.ReadCheckpoint(ctx, e.Tenant, e.Task, key)
	if err == nil {
		var value string
		err = json.Unmarshal([]byte(saved), &value)
		return value, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var attempt uint32
	err = e.mutate(ctx, func(tx *repository.ExecutionTx, locked taskRow) error {
		var err error
		attempt, err = tx.StartStep(ctx, e.Tenant, e.Task, key, e.epoch)
		if err != nil {
			return err
		}
		return eventUpdate(ctx, tx, e.delivery, locked, "step_started", map[string]any{"step_key": key, "tool_call_id": key, "attempt_no": attempt})
	})
	if err != nil {
		return "", err
	}
	started := time.Now()
	value, runErr := run(context.WithValue(ctx, modelCallKey{}, key))
	slog.Info("execution_step", "task_id", e.Task, "trace_id", e.Task, "step", key, "attempt", attempt, "elapsed_ms", time.Since(started).Milliseconds(), "ok", runErr == nil)
	if runErr != nil {
		return "", runErr
	}
	err = e.mutate(ctx, func(tx *repository.ExecutionTx, locked taskRow) error {
		var err error
		value, err = prepareCheckpointValue(ctx, tx, e.delivery, key, value)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if err = tx.SaveCheckpoint(ctx, e.Tenant, e.Task, key, string(raw)); err != nil {
			return err
		}
		status, summary := "SUCCEEDED", "步骤结果已保存"
		var observation struct{ Error string }
		if json.Unmarshal([]byte(value), &observation) == nil && observation.Error != "" {
			status = "FAILED"
			summary = textutil.Clip(observation.Error, 128)
		}
		failure := ""
		if status == "FAILED" {
			failure = summary
		}
		if err = tx.FinishStep(ctx, e.Tenant, e.Task, key, status, failure, attempt, e.epoch); err != nil {
			return err
		}
		return eventUpdate(ctx, tx, e.delivery, locked, "tool_finished", map[string]any{"step_key": key, "tool_call_id": key, "attempt_no": attempt, "status": status, "summary": summary})
	})
	return value, err
}

func prepareCheckpointValue(ctx context.Context, tx *repository.ExecutionTx, d delivery, key, value string) (string, error) {
	if !strings.HasPrefix(key, "tool-") {
		return value, nil
	}
	var sources []evidence
	if json.Unmarshal([]byte(value), &sources) != nil || len(sources) == 0 {
		return value, nil
	}
	if err := storeSources(ctx, tx, d, sources); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(sources)
	return string(encoded), err
}

func (e execution) saveSources(ctx context.Context, sources []evidence) ([]evidence, error) {
	err := e.mutate(ctx, func(tx *repository.ExecutionTx, _ taskRow) error { return storeSources(ctx, tx, e.delivery, sources) })
	return sources, err
}
func storeSources(ctx context.Context, tx *repository.ExecutionTx, d delivery, sources []evidence) error {
	for i := range sources {
		source := &sources[i]
		source.ID = identity.New(16)
		source.Title = textutil.Clip(source.Title, 512)
		fingerprint := sha256.Sum256([]byte(source.URL + "\n" + source.Excerpt))
		existing, err := tx.SourceByFingerprint(ctx, d.Tenant, d.Task, fingerprint)
		if err == nil {
			source.ID = existing
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		err = tx.InsertSource(ctx, d.Tenant, d.Task, *source, fingerprint)
		if err != nil {
			return err
		}
	}
	return nil
}

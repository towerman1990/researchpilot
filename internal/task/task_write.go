// Package task contains task lifecycle and event services.
package task

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"researchpilot/internal/common/identity"
	"researchpilot/internal/repository"
	"researchpilot/internal/repository/model"
	"researchpilot/internal/service"
)

var (
	ErrInvalidTask         = errors.New("invalid_task_request")
	ErrIdempotencyConflict = errors.New("idempotency_conflict")
	ErrModelRevoked        = errors.New("model_configuration_revoked")
	ErrTaskNotFound        = errors.New("task_not_found")
	ErrTaskFinished        = errors.New("task_already_finished")
	errDuplicateRequest    = errors.New("duplicate request key")
)

type TaskWriter interface {
	Write(context.Context, string, bool, func(repository.WriteTx) error) error
	RequestKey(context.Context, string, [32]byte) (model.RequestKey, error)
	ReadTask(context.Context, string, string) (model.TaskSnapshot, error)
}

type TaskActions struct{ Store TaskWriter }

func (s TaskActions) Submit(ctx context.Context, tenant, key, goal string) (model.TaskSnapshot, error) {
	goal = strings.TrimSpace(goal)
	if len(key) < 8 || len(key) > 128 || goal == "" || utf8.RuneCountInString(goal) > 2000 {
		return model.TaskSnapshot{}, ErrInvalidTask
	}
	kd, rd := sha256.Sum256([]byte(key)), sha256.Sum256([]byte(goal))
	var id string
	err := s.Store.Write(ctx, tenant, true, func(tx repository.WriteTx) error {
		previous, err := tx.RequestKey(tenant, kd)
		if err == nil {
			if !bytes.Equal(previous.Digest, rd[:]) {
				return ErrIdempotencyConflict
			}
			id = previous.TaskID
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		id = identity.New(16)
		if err := tx.AddTask(tenant, id, goal); err != nil {
			return err
		}
		version, err := tx.ActiveModelVersion(tenant)
		if err != nil {
			return err
		}
		if version == 0 && service.TrialEnabled() {
			version, err = service.CreateTrial(tx, tenant, id)
			if err != nil {
				return err
			}
		}
		if version > 0 {
			revoked, err := tx.ModelRevoked(tenant, version)
			if err != nil {
				return err
			}
			if revoked {
				return ErrModelRevoked
			}
			if err := tx.SetTaskModel(tenant, id, version); err != nil {
				return err
			}
		}
		if err := tx.AddRequestKey(tenant, id, kd, rd); repository.IsDuplicate(err) {
			return errDuplicateRequest
		} else if err != nil {
			return err
		}
		if err := tx.AppendEvent(tenant, id, 1, `{"status":"QUEUED"}`); err != nil {
			return err
		}
		return tx.AddOutbox(tenant, id)
	})
	if errors.Is(err, errDuplicateRequest) {
		previous, readErr := s.Store.RequestKey(ctx, tenant, kd)
		if readErr != nil {
			return model.TaskSnapshot{}, readErr
		}
		if !bytes.Equal(previous.Digest, rd[:]) {
			return model.TaskSnapshot{}, ErrIdempotencyConflict
		}
		id, err = previous.TaskID, nil
	}
	if err != nil {
		return model.TaskSnapshot{}, err
	}
	return s.Store.ReadTask(ctx, tenant, id)
}

func (s TaskActions) Cancel(ctx context.Context, tenant, id string) (model.TaskSnapshot, error) {
	err := s.Store.Write(ctx, tenant, false, func(tx repository.WriteTx) error {
		task, err := tx.LockTask(tenant, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTaskNotFound
		}
		if err != nil {
			return err
		}
		if task.Status == "CANCELLED" || task.Status == "CANCELLING" {
			return nil
		}
		if Terminal(task.Status) {
			return ErrTaskFinished
		}
		if err := tx.FlagCancellation(tenant, id); err != nil {
			return err
		}
		seq := task.Seq + 1
		payload := map[string]string{"status": "CANCELLING"}
		if task.Status == "RUNNING" {
			err = tx.MarkCancelling(tenant, id, seq)
		} else {
			payload = map[string]string{"status": "CANCELLED", "error_code": ""}
			err = tx.MarkCancelled(tenant, id, seq)
		}
		if err != nil {
			return err
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		return tx.AppendEvent(tenant, id, seq, string(body))
	})
	if err != nil {
		return model.TaskSnapshot{}, err
	}
	return s.Store.ReadTask(ctx, tenant, id)
}

// Package task contains task lifecycle and event services.
package task

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"researchpilot/internal/agent"
	"researchpilot/internal/common/identity"
	"researchpilot/internal/common/requestmeta"
	"researchpilot/internal/repository"
	"researchpilot/internal/repository/model"
	"researchpilot/internal/service"
)

var (
	ErrKnowledgeLibraryNotFound = errors.New("knowledge_library_not_found")
	ErrInvalidTask              = errors.New("invalid_task_request")
	ErrIdempotencyConflict      = errors.New("idempotency_conflict")
	ErrModelRevoked             = errors.New("model_configuration_revoked")
	ErrTaskNotFound             = errors.New("task_not_found")
	ErrTaskFinished             = errors.New("task_already_finished")
	ErrPlanNotAwaiting          = errors.New("plan_not_awaiting_approval")
	ErrInvalidPlan              = errors.New("invalid_plan")
	errDuplicateRequest         = errors.New("duplicate request key")
)

type TaskWriter interface {
	ScheduleStore
	ReadResearchPreferences(context.Context, string) (model.ResearchPreferences, error)
	RequestKey(context.Context, string, [32]byte) (model.RequestKey, error)
	ReadTask(context.Context, string, string) (model.TaskSnapshot, error)
}

type TaskActions struct{ Store TaskWriter }

type SubmitOptions struct {
	ReviewPlan         bool
	ReviewCitations    bool
	MultiAgent         bool
	DemoKnowledge      bool
	KnowledgeLibraryID string
	GuestAddress       string
}

func (s TaskActions) Submit(ctx context.Context, tenant, key, goal, parentID string, options SubmitOptions, attachments ...Material) (model.TaskSnapshot, error) {
	goal = strings.TrimSpace(goal)
	if len(key) < 8 || len(key) > 128 || goal == "" || !utf8.ValidString(goal) || utf8.RuneCountInString(goal) > 2000 {
		return model.TaskSnapshot{}, ErrInvalidTask
	}
	kd, rd := sha256.Sum256([]byte(key)), sha256.Sum256([]byte(goal))
	if parentID != "" {
		if _, err := hex.DecodeString(parentID); len(parentID) != 32 || err != nil {
			return model.TaskSnapshot{}, ErrInvalidTask
		}
		parentID = strings.ToLower(parentID)
		// A leading invalid UTF-8 byte separates follow-up digests from existing goal-only digests.
		rd = sha256.Sum256([]byte("\xff" + parentID + goal))
	}
	if options.ReviewPlan {
		rd = sha256.Sum256(append([]byte{0xfe}, rd[:]...))
	}
	if options.ReviewCitations {
		rd = sha256.Sum256(append([]byte{0xfc}, rd[:]...))
	}
	if options.MultiAgent {
		rd = sha256.Sum256(append([]byte{0xf9}, rd[:]...))
	}
	if options.DemoKnowledge {
		rd = sha256.Sum256(append([]byte{0xfb}, rd[:]...))
	}
	libraryID := options.KnowledgeLibraryID
	if libraryID != "" {
		if _, err := hex.DecodeString(libraryID); len(libraryID) != 32 || err != nil {
			return model.TaskSnapshot{}, ErrInvalidTask
		}
		if options.GuestAddress != "" {
			return model.TaskSnapshot{}, ErrKnowledgeLibraryNotFound
		}
		libraryID = strings.ToLower(libraryID)
		rd = sha256.Sum256(append(append([]byte{0xfa}, rd[:]...), libraryID...))
	}
	files, err := normalizeMaterials(attachments)
	if err != nil {
		return model.TaskSnapshot{}, err
	}
	for _, file := range files {
		fingerprint := materialFingerprint(file)
		rd = sha256.Sum256(append(append([]byte{0xfd}, rd[:]...), fingerprint[:]...))
	}
	var id string
	guest := options.GuestAddress != ""
	err = s.Store.Write(ctx, tenant, !guest, func(tx repository.WriteTx) error {
		if guest {
			if err := tx.EnsureGuestUser(tenant); err != nil {
				return err
			}
		}
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
		if guest {
			allowed, err := tx.ConsumeGuestTrial(tenant, options.GuestAddress, service.GuestTrialLimit)
			if err != nil {
				return err
			}
			if !allowed {
				return service.ErrGuestTrialExhausted
			}
		}
		if parentID != "" {
			allowed, err := tx.CanFollowUp(tenant, parentID)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrTaskNotFound
			}
			if err != nil {
				return err
			}
			if !allowed {
				return ErrReportNotReady
			}
		}
		id = identity.New(16)
		if err := tx.AddTask(tenant, id, goal, parentID, requestmeta.LogID(ctx), options.ReviewPlan, options.ReviewCitations, true); err != nil {
			return err
		}
		if options.DemoKnowledge {
			if err := tx.EnableDemoKnowledge(tenant, id); err != nil {
				return err
			}
		}
		if options.MultiAgent {
			if err := tx.EnableMultiAgent(tenant, id); err != nil {
				return err
			}
		}
		if libraryID != "" {
			if err := tx.SelectKnowledgeLibrary(tenant, id, libraryID); errors.Is(err, sql.ErrNoRows) {
				return ErrKnowledgeLibraryNotFound
			} else if err != nil {
				return err
			}
		}
		version, err := tx.ActiveModelVersion(tenant)
		if err != nil {
			return err
		}
		if version == 0 && service.TrialEnabled() {
			if guest {
				version, err = service.CreateTrialCredential(tx, tenant)
			} else {
				version, err = service.CreateTrial(tx, tenant, id)
			}
			if err != nil {
				return err
			}
		}
		if (options.ReviewPlan || options.ReviewCitations || options.MultiAgent || options.DemoKnowledge || libraryID != "") && version == 0 {
			return service.ErrModelNotConfigured
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
		for _, file := range files {
			sourceID := identity.New(16)
			source := model.Source{
				ID: sourceID, Title: file.Name, Excerpt: file.Content,
				URL: "/api/v1/research/tasks/" + id + "/sources/" + sourceID,
			}
			if err := tx.AddUploadedSource(tenant, id, source, materialFingerprint(file)); err != nil {
				return err
			}
		}
		if err := tx.AddRequestKey(tenant, id, kd, rd); repository.IsDuplicate(err) {
			return errDuplicateRequest
		} else if err != nil {
			return err
		}
		return enqueueNewTask(tx, tenant, id)
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

func enqueueNewTask(tx repository.WriteTx, tenant, id string) error {
	if err := tx.AppendEvent(tenant, id, 1, `{"status":"QUEUED"}`); err != nil {
		return err
	}
	return tx.AddOutbox(tenant, id)
}

func (s TaskActions) ApprovePlan(ctx context.Context, tenant, id string, seq uint64, rawPlan json.RawMessage) (model.TaskSnapshot, error) {
	plan, err := agent.ParsePlan(string(rawPlan), 1)
	if err != nil {
		return model.TaskSnapshot{}, ErrInvalidPlan
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return model.TaskSnapshot{}, err
	}
	err = s.Store.Write(ctx, tenant, false, func(tx repository.WriteTx) error {
		task, err := tx.LockTask(tenant, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTaskNotFound
		}
		if err != nil {
			return err
		}
		if task.Status != "AWAITING_APPROVAL" || task.Seq != seq {
			return ErrPlanNotAwaiting
		}
		if err := tx.ApprovePlan(tenant, id, string(encoded), seq+1); err != nil {
			return err
		}
		if err := tx.AppendEvent(tenant, id, seq+1, `{"status":"QUEUED","reason":"plan_approved"}`); err != nil {
			return err
		}
		return tx.AddOutbox(tenant, id)
	})
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

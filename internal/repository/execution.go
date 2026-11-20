package repository

import (
	"context"
	"encoding/hex"

	"researchpilot/internal/repository/model"
	"researchpilot/internal/repository/query"
)

// ExecutionTx contains queries scoped to a worker transaction.
type ExecutionTx struct{ q *query.Query }

func (s *Store) TransactExecution(ctx context.Context, run func(*ExecutionTx) error) error {
	return query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		return run(&ExecutionTx{q: tx})
	})
}

func (s *Store) ReadCheckpoint(ctx context.Context, tenant, id, key string) (string, error) {
	return requireOne(s.q.TaskSnapshot.WithContext(ctx).Checkpoint(tenant, id, key))
}

func (s *ExecutionTx) StartStep(ctx context.Context, tenant, id, key string, epoch uint32) (uint32, error) {
	q := s.q.TaskSnapshot.WithContext(ctx)
	if err := q.AbandonStep(tenant, id, key); err != nil {
		return 0, err
	}
	attempt, err := requireOne(q.NextStepAttempt(tenant, id, key))
	if err != nil {
		return 0, err
	}
	return attempt, q.StartStep(tenant, id, key, attempt, epoch)
}
func (s *ExecutionTx) SaveCheckpoint(ctx context.Context, tenant, id, key, value string) error {
	return s.q.TaskSnapshot.WithContext(ctx).SaveCheckpoint(tenant, id, key, value)
}
func (s *ExecutionTx) FinishStep(ctx context.Context, tenant, id, key, status, failure string, attempt, epoch uint32) error {
	return s.q.TaskSnapshot.WithContext(ctx).FinishStep(tenant, id, key, status, failure, attempt, epoch)
}

func (s *ExecutionTx) LockExecutionTask(ctx context.Context, tenant, id string) (model.ExecutionTask, error) {
	return requireOne(s.q.TaskSnapshot.WithContext(ctx).LockExecutionTask(tenant, id))
}
func (s *ExecutionTx) PauseForPlanReview(ctx context.Context, tenant, id, plan string) error {
	return s.q.TaskSnapshot.WithContext(ctx).PauseForPlanReview(tenant, id, plan)
}
func (s *ExecutionTx) ClaimExecutionTask(ctx context.Context, tenant, id string, epoch uint32, seq uint64) error {
	return s.q.TaskSnapshot.WithContext(ctx).ClaimExecutionTask(tenant, id, epoch, seq)
}
func (s *Store) RenewExecutionLease(ctx context.Context, tenant, id string, epoch uint32) (int64, error) {
	count, err := s.q.TaskSnapshot.WithContext(ctx).RenewExecutionLease(tenant, id, epoch)
	return int64(count), err
}

func (s *ExecutionTx) IncreaseRetry(ctx context.Context, tenant, id string) error {
	return s.q.TaskSnapshot.WithContext(ctx).IncreaseRetry(tenant, id)
}
func (s *ExecutionTx) RetryDue(ctx context.Context, tenant, id string) (bool, error) {
	return requireOne(s.q.TaskSnapshot.WithContext(ctx).RetryDue(tenant, id))
}
func (s *ExecutionTx) DispatchExpired(ctx context.Context, tenant, id string, generation uint32) (bool, error) {
	return requireOne(s.q.TaskSnapshot.WithContext(ctx).DispatchExpired(tenant, id, generation))
}
func (s *ExecutionTx) RequeueTask(ctx context.Context, tenant, id string, generation uint32, seq uint64) error {
	return s.q.TaskSnapshot.WithContext(ctx).RequeueTask(tenant, id, generation, seq)
}
func (s *ExecutionTx) InsertTaskOutbox(ctx context.Context, tenant, id string, generation uint32) error {
	return s.q.TaskSnapshot.WithContext(ctx).InsertTaskOutbox(tenant, id, generation)
}
func (s *ExecutionTx) InsertTaskEvent(ctx context.Context, tenant, id string, seq uint64, kind, payload string) error {
	return s.q.TaskSnapshot.WithContext(ctx).InsertTaskEvent(tenant, id, seq, kind, payload)
}
func (s *ExecutionTx) SetEventSequence(ctx context.Context, tenant, id string, seq uint64) error {
	return s.q.TaskSnapshot.WithContext(ctx).SetEventSequence(tenant, id, seq)
}

func (s *ExecutionTx) AbandonTaskAttempts(ctx context.Context, tenant, id string) error {
	return s.q.TaskSnapshot.WithContext(ctx).AbandonTaskAttempts(tenant, id)
}
func (s *ExecutionTx) SetTaskFinished(ctx context.Context, tenant, id, status, code string) error {
	return s.q.TaskSnapshot.WithContext(ctx).SetTaskFinished(tenant, id, status, code)
}
func (s *ExecutionTx) SetRetryWait(ctx context.Context, tenant, id string) error {
	return s.q.TaskSnapshot.WithContext(ctx).SetRetryWait(tenant, id)
}

func (s *ExecutionTx) SettleReservations(ctx context.Context, tenant, id string) error {
	q := s.q.TaskSnapshot.WithContext(ctx)
	if err := q.ChargeReservedTokens(tenant, id); err != nil {
		return err
	}
	return q.SettleReservedAttempts(tenant, id)
}
func (s *ExecutionTx) AttemptCount(ctx context.Context, tenant, id, key string) (uint32, error) {
	return requireOne(s.q.TaskSnapshot.WithContext(ctx).ModelAttemptCount(tenant, id, key))
}
func (s *ExecutionTx) Reserve(ctx context.Context, tenant, id string, tokens uint32) (int64, error) {
	count, err := s.q.TaskSnapshot.WithContext(ctx).ReserveTokens(tenant, id, tokens)
	return int64(count), err
}
func (s *ExecutionTx) InsertAttempt(ctx context.Context, tenant, id, attemptID string, epoch, tokens uint32, logical *string, attempt *uint32) error {
	return s.q.TaskSnapshot.WithContext(ctx).InsertModelAttempt(tenant, id, attemptID, epoch, tokens, logical, attempt)
}
func (s *ExecutionTx) Attempt(ctx context.Context, tenant, id, attemptID string, epoch uint32) (model.BudgetAttempt, error) {
	return requireOne(s.q.TaskSnapshot.WithContext(ctx).FindModelAttempt(tenant, id, attemptID, epoch))
}
func (s *ExecutionTx) SettleAttempt(ctx context.Context, tenant, id, attemptID, status, failure string, used, reserved uint32) error {
	q := s.q.TaskSnapshot.WithContext(ctx)
	if err := q.SettleModelAttempt(tenant, id, attemptID, status, failure, used); err != nil {
		return err
	}
	return q.ChargeTokens(tenant, id, reserved, used)
}

// RecoveryCandidates identifies tasks that need durable-execution recovery.
func (s *Store) RecoveryCandidates(ctx context.Context, after string, limit int) ([]model.Delivery, error) {
	return s.q.TaskSnapshot.WithContext(ctx).RecoveryCandidates(after, limit)
}
func (s *Store) PreviousModelFailure(ctx context.Context, tenant, id, key string) (string, error) {
	return requireOne(s.q.TaskSnapshot.WithContext(ctx).PreviousModelFailure(tenant, id, key))
}

func (s *ExecutionTx) SetPhase(ctx context.Context, tenant, id, phase string) error {
	return s.q.TaskSnapshot.WithContext(ctx).SetExecutionPhase(tenant, id, phase)
}
func (s *ExecutionTx) SourceByFingerprint(ctx context.Context, tenant, id string, fingerprint [32]byte) (string, error) {
	return requireOne(s.q.TaskSnapshot.WithContext(ctx).FindExecutionSource(tenant, id, hex.EncodeToString(fingerprint[:])))
}
func (s *ExecutionTx) InsertSource(ctx context.Context, tenant, id string, source model.Source, fingerprint [32]byte) error {
	return s.q.TaskSnapshot.WithContext(ctx).InsertExecutionSource(tenant, id, source.ID, source.Title, source.URL, source.Excerpt, hex.EncodeToString(fingerprint[:]))
}
func (s *ExecutionTx) PartialReportSources(ctx context.Context, tenant, id string) ([]model.Source, error) {
	return s.q.TaskSnapshot.WithContext(ctx).PartialReportSources(tenant, id)
}
func (s *ExecutionTx) SaveReport(ctx context.Context, tenant, id, markdown, kind string, citations []model.Source) error {
	q := s.q.TaskSnapshot.WithContext(ctx)
	if err := q.InsertExecutionReport(tenant, id, markdown, kind); err != nil {
		return err
	}
	for i, source := range citations {
		if err := q.InsertReportCitation(tenant, id, source.ID, i+1); err != nil {
			return err
		}
	}
	return nil
}
func (s *ExecutionTx) SetPartial(ctx context.Context, tenant, id string) error {
	return s.q.TaskSnapshot.WithContext(ctx).SetExecutionPartial(tenant, id)
}

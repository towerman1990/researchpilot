package repository

import (
	"context"
	"database/sql"

	"researchpilot/internal/repository/model"
	"researchpilot/internal/repository/query"
)

func (s *Store) ClaimOutbox(ctx context.Context) (model.OutboxClaim, error) {
	var claim model.OutboxClaim
	err := query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		outbox := tx.OutboxClaim.WithContext(ctx)
		next, err := requireOne(outbox.FindNext())
		if err != nil {
			return err
		}
		next.ClaimEpoch++
		if err := outbox.Reserve(next.Tenant, next.Task, next.Generation, next.ClaimEpoch); err != nil {
			return err
		}
		claim = next
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return claim, err
}
func (s *Store) MarkOutboxSent(ctx context.Context, claim model.OutboxClaim) error {
	return s.q.OutboxClaim.WithContext(ctx).MarkSent(claim.Tenant, claim.Task, claim.Generation, claim.ClaimEpoch)
}

func (s *Store) HasDelayedTasks(ctx context.Context, seconds int) (bool, error) {
	return requireOne(s.q.OutboxClaim.WithContext(ctx).HasDelayedTasks(seconds))
}

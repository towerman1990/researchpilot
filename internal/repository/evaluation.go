package repository

import (
	"context"
	"researchpilot/internal/repository/model"
)

func (s *Store) EvaluationTenants(ctx context.Context) ([]string, error) {
	return s.q.ModelSummary.WithContext(ctx).EvaluationTenants()
}
func (s *Store) EvaluationCreate(ctx context.Context, tenant, id, goal string, version uint32) error {
	return s.Write(ctx, tenant, false, func(tx WriteTx) error {
		if err := tx.AddTask(tenant, id, goal); err != nil {
			return err
		}
		if err := tx.SetTaskModel(tenant, id, version); err != nil {
			return err
		}
		if err := tx.AppendEvent(tenant, id, 1, `{"status":"QUEUED"}`); err != nil {
			return err
		}
		return tx.AddOutbox(tenant, id)
	})
}
func (s *Store) EvaluationSources(ctx context.Context, tenant, id string) ([]model.Source, error) {
	return s.q.TaskSnapshot.WithContext(ctx).EvaluationSources(tenant, id)
}
func (s *Store) EvaluationCitationCount(ctx context.Context, tenant, id string) (int, error) {
	return requireOne(s.q.TaskSnapshot.WithContext(ctx).EvaluationCitationCount(tenant, id))
}

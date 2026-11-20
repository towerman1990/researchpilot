package repository

import (
	"context"
	"database/sql"
	"time"

	"researchpilot/internal/repository/model"
	"researchpilot/internal/repository/query"
)

func (s *Store) EvaluationTenants(ctx context.Context) ([]string, error) {
	return s.q.ModelSummary.WithContext(ctx).EvaluationTenants()
}
func (s *Store) EvaluationCreate(ctx context.Context, tenant, id, goal string, version uint32) error {
	return s.Write(ctx, tenant, false, func(tx WriteTx) error {
		if err := tx.AddTask(tenant, id, goal, "", "", false, false, false); err != nil {
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

func (s *Store) ResearchEvaluation(ctx context.Context, tenant string, start, end time.Time) (result model.ResearchEvaluation, err error) {
	from, to := start.UTC().Format(time.DateTime), end.UTC().Format(time.DateTime)
	err = query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		tasks := tx.TaskSnapshot.WithContext(ctx)
		var err error
		result.Summary, err = requireOne(tasks.ResearchEvaluationSummary(tenant, from, to))
		if err != nil {
			return err
		}
		result.ReviewQueue, err = tasks.ResearchEvaluationReviewQueue(tenant, from, to)
		if result.ReviewQueue == nil {
			result.ReviewQueue = []model.EvaluationReviewItem{}
		}
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}

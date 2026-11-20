package repository

import (
	"context"
	"database/sql"

	"gorm.io/gorm/clause"

	"researchpilot/internal/repository/query"
)

// WriteTx keeps related task and credential writes in a single transaction.
type WriteTx struct {
	tasks   query.ITaskSnapshotDo
	configs query.IModelSummaryDo
}

func (s *Store) Write(ctx context.Context, tenant string, lockUser bool, action func(WriteTx) error) error {
	return query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		if lockUser {
			user := tx.User
			if _, err := user.WithContext(ctx).Select(user.ID).Where(user.ID.Eq(tenant)).Clauses(clause.Locking{Strength: "UPDATE"}).First(); err != nil {
				return err
			}
		}
		return action(WriteTx{tasks: tx.TaskSnapshot.WithContext(ctx), configs: tx.ModelSummary.WithContext(ctx)})
	})
}

func requireOne[T any](rows []T, err error) (T, error) {
	if err != nil {
		var zero T
		return zero, err
	}
	if len(rows) == 0 {
		var zero T
		return zero, sql.ErrNoRows
	}
	return rows[0], nil
}

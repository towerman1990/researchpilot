package repository

import (
	"context"
	"database/sql"

	"researchpilot/internal/repository/model"
	"researchpilot/internal/repository/query"
)

func (s *Store) ReadTask(ctx context.Context, tenant, id string) (snapshot model.TaskSnapshot, err error) {
	err = query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		tasks := tx.TaskSnapshot.WithContext(ctx)
		rows, err := tasks.FindSnapshot(tenant, id)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return sql.ErrNoRows
		}
		snapshot = rows[0]
		snapshot.Calls, err = tasks.LatestCalls(tenant, id)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return snapshot, err
}
func (s *Store) ListTasks(ctx context.Context, tenant, before string) ([]model.TaskSnapshot, error) {
	q := s.q.TaskSnapshot.WithContext(ctx)
	if before != "" {
		return q.ListSnapshotsBefore(tenant, before)
	}
	return q.ListSnapshots(tenant)
}

func (s *Store) ReadReport(ctx context.Context, tenant, id string) (string, error) {
	rows, err := s.q.TaskSnapshot.WithContext(ctx).ReadReport(tenant, id)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", sql.ErrNoRows
	}
	return rows[0], nil
}

func (s *Store) ReadEventPosition(ctx context.Context, tenant, id string) (model.EventPosition, error) {
	return requireOne(s.q.TaskSnapshot.WithContext(ctx).EventPosition(tenant, id))
}

func (s *Store) ReadEventBatch(ctx context.Context, tenant, id string, after uint64) (model.EventPosition, []model.TaskEvent, error) {
	rows, err := s.q.TaskSnapshot.WithContext(ctx).EventBatch(tenant, id, after)
	if err != nil {
		return model.EventPosition{}, nil, err
	}
	if len(rows) == 0 {
		return model.EventPosition{}, nil, sql.ErrNoRows
	}
	if rows[0].Seq == nil {
		return rows[0].EventPosition, nil, nil
	}
	batch := make([]model.TaskEvent, 0, len(rows))
	for _, row := range rows {
		batch = append(batch, model.TaskEvent{Seq: *row.Seq, Kind: row.Kind, Payload: row.Payload, Timestamp: row.Timestamp})
	}
	return rows[0].EventPosition, batch, nil
}

func (s *Store) ReadSource(ctx context.Context, tenant, id, source string) (model.Source, error) {
	rows, err := s.q.TaskSnapshot.WithContext(ctx).FindSource(tenant, id, source)
	if err != nil {
		return model.Source{}, err
	}
	if len(rows) == 0 {
		return model.Source{}, sql.ErrNoRows
	}
	return rows[0], nil
}

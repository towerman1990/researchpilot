package repository

import (
	"context"

	"researchpilot/internal/repository/model"
)

func (s *Store) ExpiredEventTasks(ctx context.Context, days int) ([]model.Delivery, error) {
	return s.q.TaskSnapshot.WithContext(ctx).FindExpiredEvents(days)
}
func (t WriteTx) PruneTaskEvents(tenant, id string) error {
	if err := t.tasks.AdvanceEventFloor(tenant, id); err != nil {
		return err
	}
	return t.tasks.RemoveTaskEvents(tenant, id)
}

package repository

import (
	"context"

	"researchpilot/internal/repository/model"
)

func (s *Store) ListResearchSchedules(ctx context.Context, tenant string) ([]model.ResearchSchedule, error) {
	return s.q.TaskSnapshot.WithContext(ctx).ListResearchSchedules(tenant)
}

func (s *Store) CancelResearchSchedule(ctx context.Context, tenant, id string) (bool, error) {
	rows, err := s.q.TaskSnapshot.WithContext(ctx).CancelResearchSchedule(tenant, id)
	return rows != 0, err
}

func (s *Store) ListDueResearchSchedules(ctx context.Context, now string) ([]model.DueSchedule, error) {
	return s.q.TaskSnapshot.WithContext(ctx).ListDueResearchSchedules(now)
}

func (t WriteTx) InsertResearchSchedule(tenant, id, goal string, interval int, next string, notify bool) error {
	return t.tasks.InsertResearchSchedule(tenant, id, goal, interval, next, notify)
}

func (s *Store) SetScheduleSubscription(ctx context.Context, tenant, id string, notify bool) error {
	return s.q.TaskSnapshot.WithContext(ctx).SetScheduleSubscription(tenant, id, notify)
}

func (t WriteTx) RecordScheduleResult(tenant, taskID, scheduleID string) error {
	return t.tasks.RecordScheduleResult(tenant, taskID, scheduleID)
}

func (s *Store) ListScheduleResults(ctx context.Context, tenant string) ([]model.ScheduleResult, error) {
	return s.q.TaskSnapshot.WithContext(ctx).ListScheduleResults(tenant)
}

func (s *Store) MarkScheduleResultRead(ctx context.Context, tenant, taskID string) error {
	return s.q.TaskSnapshot.WithContext(ctx).MarkScheduleResultRead(tenant, taskID)
}

func (t WriteTx) LockDueResearchSchedule(tenant, id, now string) (model.ResearchSchedule, error) {
	return requireOne(t.tasks.LockDueResearchSchedule(tenant, id, now))
}

func (t WriteTx) AdvanceResearchSchedule(tenant, id, status, next, taskID string) error {
	return t.tasks.AdvanceResearchSchedule(tenant, id, status, next, taskID)
}

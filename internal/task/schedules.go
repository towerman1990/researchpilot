package task

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"researchpilot/internal/common/identity"
	"researchpilot/internal/repository"
	"researchpilot/internal/repository/model"
	"researchpilot/internal/service"
)

var ErrInvalidSchedule = errors.New("invalid_research_schedule")
var ErrScheduleNotFound = errors.New("active_schedule_not_found")

type ScheduleStore interface {
	Write(context.Context, string, bool, func(repository.WriteTx) error) error
	ListResearchSchedules(context.Context, string) ([]model.ResearchSchedule, error)
	CancelResearchSchedule(context.Context, string, string) (bool, error)
	ListDueResearchSchedules(context.Context, string) ([]model.DueSchedule, error)
	SetScheduleSubscription(context.Context, string, string, bool) error
	ListScheduleResults(context.Context, string) ([]model.ScheduleResult, error)
	MarkScheduleResultRead(context.Context, string, string) error
}

func (s TaskActions) ListSchedules(ctx context.Context, tenant string) ([]model.ResearchSchedule, error) {
	rows, err := s.Store.ListResearchSchedules(ctx, tenant)
	if rows == nil {
		rows = []model.ResearchSchedule{}
	}
	return rows, err
}

func (s TaskActions) CreateSchedule(ctx context.Context, tenant, goal, start string, interval int, notify bool) error {
	goal = strings.TrimSpace(goal)
	next, err := time.Parse(time.RFC3339, start)
	if err != nil || next.UTC().Year() > 9998 || !next.After(time.Now()) || goal == "" ||
		!utf8.ValidString(goal) || utf8.RuneCountInString(goal) > 2000 || (interval != 0 && interval != 1 && interval != 7) {
		return ErrInvalidSchedule
	}
	return s.Store.Write(ctx, tenant, true, func(tx repository.WriteTx) error {
		version, err := tx.ActiveModelVersion(tenant)
		if err != nil {
			return err
		}
		if version == 0 {
			return service.ErrModelNotConfigured
		}
		return tx.InsertResearchSchedule(tenant, identity.New(16), goal, interval, next.UTC().Format(time.DateTime), notify)
	})
}

func (s TaskActions) CancelSchedule(ctx context.Context, tenant, id string) error {
	found, err := s.Store.CancelResearchSchedule(ctx, tenant, id)
	if err != nil {
		return err
	}
	if !found {
		return ErrScheduleNotFound
	}
	return nil
}

func RunDueSchedules(ctx context.Context, store ScheduleStore, now time.Time) error {
	dueAt := now.UTC().Format(time.DateTime)
	rows, err := store.ListDueResearchSchedules(ctx, dueAt)
	if err != nil {
		return err
	}
	var failures error
	for _, row := range rows {
		if ctx.Err() != nil {
			return errors.Join(failures, ctx.Err())
		}
		err := store.Write(ctx, row.TenantID, true, func(tx repository.WriteTx) error {
			return runSchedule(tx, row, dueAt, now)
		})
		failures = errors.Join(failures, err)
	}
	return failures
}

func runSchedule(tx repository.WriteTx, row model.DueSchedule, dueAt string, now time.Time) error {
	tenant := row.TenantID
	schedule, err := tx.LockDueResearchSchedule(tenant, row.ID, dueAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	version, err := tx.ActiveModelVersion(tenant)
	if err != nil {
		return err
	}
	if version == 0 {
		return tx.AdvanceResearchSchedule(tenant, row.ID, "blocked", "", "")
	}
	taskID := identity.New(16)
	if err := tx.AddTask(tenant, taskID, schedule.Goal, "", "", false, false, true); err != nil {
		return err
	}
	if err := tx.SetTaskModel(tenant, taskID, version); err != nil {
		return err
	}
	if err := enqueueNewTask(tx, tenant, taskID); err != nil {
		return err
	}
	if schedule.NotifyResults {
		if err := tx.RecordScheduleResult(tenant, taskID, row.ID); err != nil {
			return err
		}
	}
	if schedule.IntervalDays == 0 {
		return tx.AdvanceResearchSchedule(tenant, row.ID, "completed", "", taskID)
	}
	first, err := time.Parse(time.RFC3339, *schedule.NextRun)
	if err != nil {
		return err
	}
	interval := time.Duration(schedule.IntervalDays) * 24 * time.Hour
	next := first.Add((now.Sub(first)/interval + 1) * interval)
	return tx.AdvanceResearchSchedule(tenant, row.ID, "active", next.Format(time.DateTime), taskID)
}

func (s TaskActions) ScheduleResults(ctx context.Context, tenant string) ([]model.ScheduleResult, error) {
	rows, err := s.Store.ListScheduleResults(ctx, tenant)
	if rows == nil {
		rows = []model.ScheduleResult{}
	}
	return rows, err
}

func (s TaskActions) SetScheduleSubscription(ctx context.Context, tenant, id string, notify bool) error {
	return s.Store.SetScheduleSubscription(ctx, tenant, id, notify)
}

func (s TaskActions) MarkScheduleResultRead(ctx context.Context, tenant, taskID string) error {
	return s.Store.MarkScheduleResultRead(ctx, tenant, taskID)
}

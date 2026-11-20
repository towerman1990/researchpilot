// Package task contains task lifecycle and event services.
package task

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"researchpilot/internal/repository/model"
)

type TaskReader interface {
	ReadSource(context.Context, string, string, string) (model.Source, error)
	ListTasks(context.Context, string, string) ([]model.TaskSnapshot, error)
	ReadTask(context.Context, string, string) (model.TaskSnapshot, error)
	ReadReport(context.Context, string, string) (string, error)
	ReadReportEvidence(context.Context, string, string) (model.ReportEvidence, error)
	ReadReportFeedback(context.Context, string, string) (model.ReportFeedback, error)
	SaveReportFeedback(context.Context, string, string, model.ReportFeedback) error
	DailyModelUsage(context.Context, string, time.Time, time.Time) ([]model.UsageDay, error)
	UsageByModel(context.Context, string, time.Time, time.Time) ([]model.ModelUsage, error)
	ResearchEvaluation(context.Context, string, time.Time, time.Time) (model.ResearchEvaluation, error)
	OverviewTasks(context.Context, string) ([]model.OverviewTask, []model.OverviewTask, error)
}
type Tasks struct{ Store TaskReader }

var errReadReport = errors.New("could not read stored report")
var ErrReportUnavailable = errors.New("report_unavailable")
var ErrReportNotReady = errors.New("report_not_ready")

var ErrInvalidHistoryCursor = errors.New("invalid_history_cursor")

const historyPageSize = 50

type Overview struct {
	Open     []model.OverviewTask `json:"open"`
	Finished []model.OverviewTask `json:"finished"`
	MoreOpen bool                 `json:"more_open"`
}

func (s Tasks) Overview(ctx context.Context, tenant string) (Overview, error) {
	const maxOpen = 50
	open, finished, err := s.Store.OverviewTasks(ctx, tenant)
	if err != nil {
		return Overview{}, err
	}
	return Overview{
		Open:     append([]model.OverviewTask{}, open[:min(len(open), maxOpen)]...),
		Finished: append([]model.OverviewTask{}, finished...),
		MoreOpen: len(open) > maxOpen,
	}, nil
}

var ErrInvalidUsagePeriod = errors.New("invalid_usage_period")

func usageWindow(days string, now time.Time) (time.Time, time.Time, int, error) {
	var span int
	switch days {
	case "", "7":
		span = 7
	case "30":
		span = 30
	default:
		return time.Time{}, time.Time{}, 0, ErrInvalidUsagePeriod
	}
	today := now.UTC().Truncate(24 * time.Hour)
	end := today.AddDate(0, 0, 1)
	start := end.AddDate(0, 0, -span)
	return start, end, span, nil
}

func (s Tasks) ModelUsage(ctx context.Context, tenant, days string, now time.Time) ([]model.ModelUsage, error) {
	start, end, _, err := usageWindow(days, now)
	if err != nil {
		return nil, err
	}
	rows, err := s.Store.UsageByModel(ctx, tenant, start, end)
	if rows == nil {
		rows = []model.ModelUsage{}
	}
	return rows, err
}

func (s Tasks) Usage(ctx context.Context, tenant string, days string, now time.Time) ([]model.UsageDay, error) {
	start, end, span, err := usageWindow(days, now)
	if err != nil {
		return nil, err
	}
	rows, err := s.Store.DailyModelUsage(ctx, tenant, start, end)
	if err != nil {
		return nil, err
	}
	result := make([]model.UsageDay, 0, span)
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		label := day.Format(time.DateOnly)
		if len(rows) > 0 && rows[0].Day == label {
			result = append(result, rows[0])
			rows = rows[1:]
		} else {
			result = append(result, model.UsageDay{Day: label})
		}
	}
	return result, nil
}

func (s Tasks) Evaluation(ctx context.Context, tenant, days string, now time.Time) (model.ResearchEvaluation, error) {
	start, end, _, err := usageWindow(days, now)
	if err != nil {
		return model.ResearchEvaluation{}, err
	}
	return s.Store.ResearchEvaluation(ctx, tenant, start, end)
}

func (s Tasks) List(ctx context.Context, tenant, before string) ([]model.TaskSnapshot, string, error) {
	if before != "" {
		if _, err := hex.DecodeString(before); len(before) != 32 || err != nil {
			return nil, "", ErrInvalidHistoryCursor
		}
	}
	rows, err := s.Store.ListTasks(ctx, tenant, before)
	if err != nil {
		return nil, "", err
	}
	if len(rows) > historyPageSize {
		return rows[:historyPageSize], rows[historyPageSize-1].ID, nil
	}
	if rows == nil {
		rows = []model.TaskSnapshot{}
	}
	return rows, "", nil
}

func (s Tasks) Get(ctx context.Context, tenant, id string) (model.TaskSnapshot, error) {
	return s.Store.ReadTask(ctx, tenant, id)
}
func (s Tasks) Report(ctx context.Context, tenant, id string) (string, error) {
	if err := s.checkReport(ctx, tenant, id); err != nil {
		return "", err
	}
	body, err := s.Store.ReadReport(ctx, tenant, id)
	if err != nil {
		return "", errReadReport
	}
	return body, nil
}

func (s Tasks) ReportEvidence(ctx context.Context, tenant, id string) (model.ReportEvidence, error) {
	if err := s.checkReport(ctx, tenant, id); err != nil {
		return model.ReportEvidence{}, err
	}
	evidence, err := s.Store.ReadReportEvidence(ctx, tenant, id)
	if err != nil {
		return model.ReportEvidence{}, errReadReport
	}
	return evidence, nil
}

func (s Tasks) checkReport(ctx context.Context, tenant, id string) error {
	snapshot, err := s.Get(ctx, tenant, id)
	if err != nil {
		return err
	}
	switch snapshot.Status {
	case "FAILED", "CANCELLED":
		return ErrReportUnavailable
	case "SUCCEEDED", "PARTIAL":
		return nil
	default:
		return ErrReportNotReady
	}
}

func (s Tasks) Source(ctx context.Context, tenant, id, source string) (model.Source, error) {
	return s.Store.ReadSource(ctx, tenant, id, source)
}

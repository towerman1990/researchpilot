// Package task contains task lifecycle and event services.
package task

import (
	"context"
	"encoding/hex"
	"errors"

	"researchpilot/internal/repository/model"
)

type TaskReader interface {
	ReadSource(context.Context, string, string, string) (model.Source, error)
	ListTasks(context.Context, string, string) ([]model.TaskSnapshot, error)
	ReadTask(context.Context, string, string) (model.TaskSnapshot, error)
	ReadReport(context.Context, string, string) (string, error)
}
type Tasks struct{ Store TaskReader }

var errReadReport = errors.New("could not read stored report")
var ErrReportUnavailable = errors.New("report_unavailable")
var ErrReportNotReady = errors.New("report_not_ready")

var ErrInvalidHistoryCursor = errors.New("invalid_history_cursor")

const historyPageSize = 50

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
	snapshot, err := s.Get(ctx, tenant, id)
	if err != nil {
		return "", err
	}
	switch snapshot.Status {
	case "FAILED", "CANCELLED":
		return "", ErrReportUnavailable
	case "SUCCEEDED", "PARTIAL":
		body, err := s.Store.ReadReport(ctx, tenant, id)
		if err != nil {
			return "", errReadReport
		}
		return body, nil
	default:
		return "", ErrReportNotReady
	}
}

func (s Tasks) Source(ctx context.Context, tenant, id, source string) (model.Source, error) {
	return s.Store.ReadSource(ctx, tenant, id, source)
}

package task

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"researchpilot/internal/repository/model"
)

var ErrInvalidReportFeedback = errors.New("invalid_report_feedback")

func (s Tasks) ReportFeedback(ctx context.Context, tenant, id string) (model.ReportFeedback, error) {
	if err := s.checkReport(ctx, tenant, id); err != nil {
		return model.ReportFeedback{}, err
	}
	return s.Store.ReadReportFeedback(ctx, tenant, id)
}

func (s Tasks) SaveReportFeedback(ctx context.Context, tenant, id string, feedback model.ReportFeedback) (model.ReportFeedback, error) {
	if err := s.checkReport(ctx, tenant, id); err != nil {
		return model.ReportFeedback{}, err
	}
	feedback.Comment = strings.TrimSpace(feedback.Comment)
	if (feedback.Rating != "helpful" && feedback.Rating != "not_helpful") || utf8.RuneCountInString(feedback.Comment) > 500 {
		return model.ReportFeedback{}, ErrInvalidReportFeedback
	}
	if err := s.Store.SaveReportFeedback(ctx, tenant, id, feedback); err != nil {
		return model.ReportFeedback{}, err
	}
	return feedback, nil
}

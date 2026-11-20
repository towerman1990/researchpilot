package handler

import (
	"context"
	"errors"

	"github.com/cloudwego/hertz/pkg/app"
	"researchpilot/internal/repository/model"
	taskservice "researchpilot/internal/task"
)

func (h Tasks) reportFeedback(ctx context.Context, c *app.RequestContext) {
	feedback, err := h.Service.ReportFeedback(ctx, c.GetString(tenantKey), c.Param("id"))
	if err != nil {
		h.reportError(c, err)
		return
	}
	c.JSON(200, feedback)
}

func (h Tasks) saveReportFeedback(ctx context.Context, c *app.RequestContext) {
	var input model.ReportFeedback
	if !parseJSON(c, &input) {
		return
	}
	feedback, err := h.Service.SaveReportFeedback(ctx, c.GetString(tenantKey), c.Param("id"), input)
	if errors.Is(err, taskservice.ErrInvalidReportFeedback) {
		problem(c, 422, err.Error())
		return
	}
	if err != nil {
		h.reportError(c, err)
		return
	}
	c.JSON(200, feedback)
}

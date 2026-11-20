package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"researchpilot/internal/common/envconfig"
)

func (h Tasks) listSchedules(ctx context.Context, c *app.RequestContext) {
	rows, err := h.Actions.ListSchedules(ctx, c.GetString(tenantKey))
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(200, rows)
}

func (h Tasks) createSchedule(ctx context.Context, c *app.RequestContext) {
	tenant := c.GetString(tenantKey)
	if !h.Auth.allow(ctx, c, "schedule-create", tenant, envconfig.Int("RATE_TASK_CREATE", 10, 1, 10000)) {
		return
	}
	var input struct {
		Goal          string `json:"goal"`
		FirstRun      string `json:"first_run"`
		IntervalDays  int    `json:"interval_days"`
		NotifyResults bool   `json:"notify_results"`
	}
	if !parseJSONLimit(c, &input, 16<<10) {
		return
	}
	if err := h.Actions.CreateSchedule(ctx, tenant, input.Goal, input.FirstRun, input.IntervalDays, input.NotifyResults); err != nil {
		writeFailure(c, err)
		return
	}
	c.JSON(201, map[string]bool{"created": true})
}

func (h Tasks) cancelSchedule(ctx context.Context, c *app.RequestContext) {
	if err := h.Actions.CancelSchedule(ctx, c.GetString(tenantKey), c.Param("id")); err != nil {
		writeFailure(c, err)
		return
	}
	c.SetStatusCode(204)
}

func (h Tasks) setScheduleSubscription(ctx context.Context, c *app.RequestContext) {
	var input struct {
		NotifyResults *bool `json:"notify_results"`
	}
	if !parseJSON(c, &input) {
		return
	}
	if input.NotifyResults == nil {
		problem(c, 422, "invalid_schedule_subscription")
		return
	}
	if err := h.Actions.SetScheduleSubscription(ctx, c.GetString(tenantKey), c.Param("id"), *input.NotifyResults); err != nil {
		internalError(c, err)
		return
	}
	c.SetStatusCode(204)
}

func (h Tasks) scheduleResults(ctx context.Context, c *app.RequestContext) {
	rows, err := h.Actions.ScheduleResults(ctx, c.GetString(tenantKey))
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(200, rows)
}

func (h Tasks) markScheduleResultRead(ctx context.Context, c *app.RequestContext) {
	if err := h.Actions.MarkScheduleResultRead(ctx, c.GetString(tenantKey), c.Param("id")); err != nil {
		internalError(c, err)
		return
	}
	c.SetStatusCode(204)
}

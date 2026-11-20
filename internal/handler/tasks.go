package handler

import (
	"context"
	"database/sql"
	"errors"
	taskservice "researchpilot/internal/task"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route"

	"researchpilot/internal/common/envconfig"
)

type Tasks struct {
	Service taskservice.Tasks
	Actions taskservice.TaskActions
	Stream  taskservice.EventStream
	Auth    Auth
}

func (h Tasks) RegisterRoutes(r route.IRouter) {
	g := r.Group("/api/v1/research/tasks", h.Auth.apiHeaders, h.Auth.requireSession)
	g.GET("", h.list)
	g.GET("/:id", h.get)
	g.GET("/:id/report", h.report)
	g.GET("/:id/events", h.events)
	g.GET("/:id/sources/:source", h.source)
	writes := r.Group("/api/v1/research/tasks", h.Auth.requireJSON, h.Auth.requireSession)
	writes.POST("", h.submit)
	writes.POST("/:id/cancel", h.cancel)
}
func (h Tasks) list(ctx context.Context, c *app.RequestContext) {
	rows, next, err := h.Service.List(ctx, c.GetString(tenantKey), c.Query("before"))
	if errors.Is(err, taskservice.ErrInvalidHistoryCursor) {
		problem(c, 422, "invalid_history_cursor")
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	if next != "" {
		c.Header("X-Next-Cursor", next)
	}
	c.JSON(200, rows)
}
func (h Tasks) get(ctx context.Context, c *app.RequestContext) {
	snapshot, err := h.Service.Get(ctx, c.GetString(tenantKey), c.Param("id"))
	if errors.Is(err, sql.ErrNoRows) {
		problem(c, 404, "task_not_found")
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(200, snapshot)
}
func (h Tasks) report(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	body, err := h.Service.Report(ctx, c.GetString(tenantKey), id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		problem(c, 404, "task_not_found")
	case errors.Is(err, taskservice.ErrReportUnavailable):
		problem(c, 404, "report_unavailable")
	case errors.Is(err, taskservice.ErrReportNotReady):
		problem(c, 409, "report_not_ready")
	case err != nil:
		internalError(c, err)
	default:
		c.JSON(200, map[string]string{"task_id": id, "markdown": body})
	}
}

func (h Tasks) source(ctx context.Context, c *app.RequestContext) {
	data, err := h.Service.Source(ctx, c.GetString(tenantKey), c.Param("id"), c.Param("source"))
	if errors.Is(err, sql.ErrNoRows) {
		problem(c, 404, "source_not_found")
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(200, data)
}

func (h Tasks) submit(ctx context.Context, c *app.RequestContext) {
	tenant := c.GetString(tenantKey)
	if !h.Auth.allow(ctx, c, "task-create", tenant, envconfig.Int("RATE_TASK_CREATE", 10, 1, 10000)) {
		return
	}
	var input struct{ Goal string }
	if !parseJSON(c, &input) {
		return
	}
	task, err := h.Actions.Submit(ctx, tenant, string(c.GetHeader("Idempotency-Key")), input.Goal)
	if err != nil {
		writeFailure(c, err)
		return
	}
	c.Header("Location", "/api/v1/research/tasks/"+task.ID)
	c.JSON(202, task)
}

func (h Tasks) cancel(ctx context.Context, c *app.RequestContext) {
	var input struct{}
	if !parseJSON(c, &input) {
		return
	}
	task, err := h.Actions.Cancel(ctx, c.GetString(tenantKey), c.Param("id"))
	if err != nil {
		writeFailure(c, err)
		return
	}
	c.JSON(202, task)
}

package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route"

	"researchpilot/internal/common/envconfig"
	"researchpilot/internal/common/requestmeta"
	taskservice "researchpilot/internal/task"
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
	r.GET("/api/v1/research/preferences", h.Auth.apiHeaders, h.Auth.requireSession, h.getPreferences)
	r.GET("/api/v1/research/usage", h.Auth.apiHeaders, h.Auth.requireSession, h.getUsage)
	r.GET("/api/v1/research/usage/models", h.Auth.apiHeaders, h.Auth.requireSession, h.getModelUsage)
	r.GET("/api/v1/research/evaluation", h.Auth.apiHeaders, h.Auth.requireSession, h.getEvaluation)
	r.GET("/api/v1/research/overview", h.Auth.apiHeaders, h.Auth.requireSession, h.getOverview)
	r.PUT("/api/v1/research/preferences", h.Auth.requireJSON, h.Auth.requireSession, h.putPreferences)
	r.GET("/api/v1/research/schedules", h.Auth.apiHeaders, h.Auth.requireSession, h.listSchedules)
	r.POST("/api/v1/research/schedules", h.Auth.requireJSON, h.Auth.requireSession, h.createSchedule)
	r.POST("/api/v1/research/schedules/:id/cancel", h.Auth.requireJSON, h.Auth.requireSession, h.cancelSchedule)
	r.POST("/api/v1/research/schedules/:id/subscription", h.Auth.requireJSON, h.Auth.requireSession, h.setScheduleSubscription)
	r.GET("/api/v1/research/schedule-results", h.Auth.apiHeaders, h.Auth.requireSession, h.scheduleResults)
	r.POST("/api/v1/research/schedule-results/:id/read", h.Auth.requireJSON, h.Auth.requireSession, h.markScheduleResultRead)
	g.GET("/:id", h.get)
	g.GET("/:id/report", h.report)
	g.GET("/:id/report/evidence", h.reportEvidence)
	g.GET("/:id/report/feedback", h.reportFeedback)
	g.GET("/:id/events", h.events)
	g.GET("/:id/sources/:source", h.source)
	writes := r.Group("/api/v1/research/tasks", h.Auth.requireJSON, h.Auth.requireSession)
	writes.POST("", h.submit)
	writes.POST("/:id/cancel", h.cancel)
	writes.POST("/:id/plan/approve", h.approvePlan)
	writes.PUT("/:id/report/feedback", h.saveReportFeedback)
}

func (h Tasks) getOverview(ctx context.Context, c *app.RequestContext) {
	overview, err := h.Service.Overview(ctx, c.GetString(tenantKey))
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(200, overview)
}

func (h Tasks) getUsage(ctx context.Context, c *app.RequestContext) {
	usage, err := h.Service.Usage(ctx, c.GetString(tenantKey), c.Query("days"), time.Now())
	if errors.Is(err, taskservice.ErrInvalidUsagePeriod) {
		problem(c, 422, err.Error())
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(200, usage)
}

func (h Tasks) getEvaluation(ctx context.Context, c *app.RequestContext) {
	result, err := h.Service.Evaluation(ctx, c.GetString(tenantKey), c.Query("days"), time.Now())
	if errors.Is(err, taskservice.ErrInvalidUsagePeriod) {
		problem(c, 422, err.Error())
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(200, result)
}

func (h Tasks) getModelUsage(ctx context.Context, c *app.RequestContext) {
	usage, err := h.Service.ModelUsage(ctx, c.GetString(tenantKey), c.Query("days"), time.Now())
	if errors.Is(err, taskservice.ErrInvalidUsagePeriod) {
		problem(c, 422, err.Error())
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(200, usage)
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
	if err != nil {
		h.reportError(c, err)
		return
	}
	c.JSON(200, map[string]string{"task_id": id, "markdown": body})
}

func (h Tasks) reportEvidence(ctx context.Context, c *app.RequestContext) {
	evidence, err := h.Service.ReportEvidence(ctx, c.GetString(tenantKey), c.Param("id"))
	if err != nil {
		h.reportError(c, err)
		return
	}
	c.JSON(200, evidence)
}

func (h Tasks) reportError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		problem(c, 404, "task_not_found")
	case errors.Is(err, taskservice.ErrReportUnavailable):
		problem(c, 404, "report_unavailable")
	case errors.Is(err, taskservice.ErrReportNotReady):
		problem(c, 409, "report_not_ready")
	default:
		internalError(c, err)
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
	var input struct {
		Goal            string                 `json:"goal"`
		ParentTaskID    string                 `json:"parent_task_id"`
		ReviewPlan      bool                   `json:"review_plan"`
		ReviewCitations bool                   `json:"review_citations"`
		Attachments     []taskservice.Material `json:"attachments"`
	}
	if !parseJSONLimit(c, &input, 320<<10) {
		return
	}
	options := taskservice.SubmitOptions{ReviewPlan: input.ReviewPlan, ReviewCitations: input.ReviewCitations}
	task, err := h.Actions.Submit(ctx, tenant, string(c.GetHeader("Idempotency-Key")), input.Goal, input.ParentTaskID, options, input.Attachments...)
	if err != nil {
		writeFailure(c, err)
		return
	}
	c.Header("Location", "/api/v1/research/tasks/"+task.ID)
	if task.CreationLogID != nil {
		slog.Info("task_submission", "logid", requestmeta.LogID(ctx), "task_id", task.ID, "creation_logid", *task.CreationLogID)
	}
	c.JSON(202, task)
}

func (h Tasks) approvePlan(ctx context.Context, c *app.RequestContext) {
	var input struct {
		Seq  uint64          `json:"event_seq,string"`
		Plan json.RawMessage `json:"plan"`
	}
	if !parseJSONLimit(c, &input, 33024) {
		return
	}
	snapshot, err := h.Actions.ApprovePlan(ctx, c.GetString(tenantKey), c.Param("id"), input.Seq, input.Plan)
	if err != nil {
		writeFailure(c, err)
		return
	}
	c.JSON(202, snapshot)
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

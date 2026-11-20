package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route"

	"researchpilot/internal/common/envconfig"
	"researchpilot/internal/service"
)

type Models struct {
	Service service.Models
	Auth    Auth
}

func (h Models) RegisterRoutes(r route.IRouter) {
	r.GET("/api/v1/model-config", h.Auth.apiHeaders, h.Auth.requireSession, h.status)
	r.DELETE("/api/v1/model-config", h.Auth.requireJSON, h.Auth.requireSession, h.revoke)
	r.PUT("/api/v1/model-config", h.Auth.requireJSON, h.Auth.requireSession, h.save)
	r.POST("/api/v1/model-config/probe", h.Auth.requireJSON, h.Auth.requireSession, h.probe)
}
func (h Models) status(ctx context.Context, c *app.RequestContext) {
	data, err := h.Service.Status(ctx, c.GetString(tenantKey))
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(200, data)
}
func (h Models) revoke(ctx context.Context, c *app.RequestContext) {
	if err := h.Service.Revoke(ctx, c.GetString(tenantKey)); err != nil {
		internalError(c, err)
		return
	}
	c.JSON(200, map[string]bool{"configured": false, "revoked": true})
}

func (h Models) save(ctx context.Context, c *app.RequestContext) {
	var input service.ModelInput
	if !parseJSON(c, &input) {
		return
	}
	version, err := h.Service.Save(ctx, c.GetString(tenantKey), input)
	if err != nil {
		writeFailure(c, err)
		return
	}
	c.JSON(200, map[string]any{"configured": true, "version": version})
}

func (h Models) probe(ctx context.Context, c *app.RequestContext) {
	tenant := c.GetString(tenantKey)
	if !h.Auth.allow(ctx, c, "model-probe", tenant, envconfig.Int("RATE_MODEL_PROBE", 3, 1, 10000)) {
		return
	}
	data, err := h.Service.Probe(ctx, tenant)
	if err != nil {
		writeFailure(c, err)
		return
	}
	c.JSON(200, data)
}

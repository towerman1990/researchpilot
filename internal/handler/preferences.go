package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"researchpilot/internal/repository/model"
)

func (h Tasks) getPreferences(ctx context.Context, c *app.RequestContext) {
	preferences, err := h.Actions.Preferences(ctx, c.GetString(tenantKey))
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(200, preferences)
}

func (h Tasks) putPreferences(ctx context.Context, c *app.RequestContext) {
	var preferences model.ResearchPreferences
	if !parseJSON(c, &preferences) {
		return
	}
	if err := h.Actions.SavePreferences(ctx, c.GetString(tenantKey), preferences); err != nil {
		writeFailure(c, err)
		return
	}
	c.JSON(200, preferences)
}

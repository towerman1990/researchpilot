// Package handler contains Hertz-native HTTP route handlers.
package handler

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
)

type databasePinger interface {
	PingContext(context.Context) error
}

type Health struct {
	Pinger          databasePinger
	PingRedis       func(context.Context) error
	HasDelayedTasks func(context.Context, int) (bool, error)
}

func (h Health) check(ctx context.Context, c *app.RequestContext) {
	c.Header("X-Content-Type-Options", "nosniff")
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := h.Pinger.PingContext(ctx); err != nil {
		c.JSON(consts.StatusServiceUnavailable, map[string]string{"code": "database_unavailable"})
		return
	}
	c.JSON(consts.StatusOK, map[string]bool{"ok": true})
}

func (h Health) ready(ctx context.Context, c *app.RequestContext) {
	c.Header("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := h.Pinger.PingContext(ctx); err != nil {
		problem(c, consts.StatusServiceUnavailable, "database_unavailable")
		return
	}
	if h.PingRedis == nil || h.PingRedis(ctx) != nil {
		problem(c, consts.StatusServiceUnavailable, "redis_unavailable")
		return
	}
	c.JSON(consts.StatusOK, map[string]bool{"ok": true})
}

func (h Health) queue(ctx context.Context, c *app.RequestContext) {
	c.Header("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	const delayedAfterSeconds = 120
	if h.HasDelayedTasks == nil {
		problem(c, consts.StatusServiceUnavailable, "queue_status_unavailable")
		return
	}
	delayed, err := h.HasDelayedTasks(ctx, delayedAfterSeconds)
	if err != nil {
		problem(c, consts.StatusServiceUnavailable, "queue_status_unavailable")
		return
	}
	if delayed {
		problem(c, consts.StatusServiceUnavailable, "queue_delayed")
		return
	}
	c.JSON(consts.StatusOK, map[string]bool{"ok": true})
}

func (h Health) RegisterRoutes(r route.IRouter) {
	r.GET("/healthz", h.check)
	r.GET("/readyz", h.ready)
	r.GET("/queuez", h.queue)
}

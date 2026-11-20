package httpserver

import (
	"context"
	"log/slog"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"researchpilot/internal/common/identity"
	"researchpilot/internal/common/requestmeta"
)

func AccessLog(ctx context.Context, c *app.RequestContext) {
	id := identity.New(16)
	c.Header("X-Log-ID", id)
	start := time.Now()
	defer func() {
		slog.Info("http_request", "logid", id, "method", string(c.Method()), "route", c.FullPath(),
			"status", c.Response.StatusCode(), "elapsed_ms", time.Since(start).Milliseconds())
	}()
	c.Next(requestmeta.WithLogID(ctx, id))
}

package handler

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route"
)

type Web struct{ Root string }

func (w Web) RegisterRoutes(r route.IRouter) {
	fs := app.FS{Root: w.Root, IndexNames: []string{"index.html"}, AcceptByteRange: true}
	serve := fs.NewRequestHandler()
	handler := func(ctx context.Context, c *app.RequestContext) {
		c.Header("X-Content-Type-Options", "nosniff")
		path := string(c.Path())
		if path == "/api" || strings.HasPrefix(path, "/api/") {
			setAPIHeaders(c)
			c.SetStatusCode(404)
			return
		}
		serve(ctx, c)
	}
	r.GET("/*path", handler)
	r.HEAD("/*path", handler)
}

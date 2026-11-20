// Package httpserver owns the Hertz server lifecycle.
package httpserver

import (
	"context"
	"log"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/route"
)

// Serve starts Hertz and cancels active requests during shutdown.
func Serve(ctx context.Context, address string, register func(route.IRouter)) error {
	h := server.New(server.WithHostPorts(address), server.WithReadTimeout(5*time.Second))
	h.Use(AccessLog)
	h.Use(func(requestCtx context.Context, c *app.RequestContext) {
		requestCtx, cancel := context.WithCancel(requestCtx)
		defer cancel()
		stop := context.AfterFunc(ctx, cancel)
		defer stop()
		c.Next(requestCtx)
	})
	register(h)
	errCh := make(chan error, 1)
	go func() { errCh <- h.Run() }()
	log.Printf("Hertz API listening on %s", address)
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return h.Shutdown(shutdownCtx)
	}
}

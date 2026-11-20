package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type healthPinger func(context.Context) error

func (p healthPinger) PingContext(ctx context.Context) error { return p(ctx) }

func TestHealthDatabaseCheck(t *testing.T) {
	for _, check := range []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{"available", nil, http.StatusOK, `"ok":true`},
		{"unavailable", errors.New("offline"), http.StatusServiceUnavailable, `"code":"database_unavailable"`},
	} {
		t.Run(check.name, func(t *testing.T) {
			h := server.New()
			Health{Pinger: healthPinger(func(ctx context.Context) error {
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > time.Second {
					t.Error("database ping lacks one-second deadline")
				}
				return check.err
			})}.RegisterRoutes(h)
			response := ut.PerformRequest(h.Engine, http.MethodGet, "/healthz", nil)
			if response.Code != check.status || !strings.Contains(response.Body.String(), check.body) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestReadinessAndQueueSignals(t *testing.T) {
	offline := errors.New("offline")
	for _, tc := range []struct {
		path     string
		redisErr error
		delayed  bool
		status   int
		body     string
	}{
		{"/readyz", nil, false, 200, "ok"},
		{"/readyz", offline, false, 503, "redis_unavailable"},
		{"/queuez", nil, true, 503, "queue_delayed"},
		{"/queuez", nil, false, 200, "ok"},
	} {
		h := server.New()
		Health{
			Pinger:    healthPinger(func(context.Context) error { return nil }),
			PingRedis: func(context.Context) error { return tc.redisErr },
			HasDelayedTasks: func(_ context.Context, threshold int) (bool, error) {
				if threshold != 120 {
					t.Errorf("unexpected queue threshold: %d", threshold)
				}
				return tc.delayed, nil
			},
		}.RegisterRoutes(h)
		res := ut.PerformRequest(h.Engine, http.MethodGet, tc.path, nil)
		if res.Code != tc.status || !strings.Contains(res.Body.String(), tc.body) {
			t.Errorf("%s: status %d, body %s", tc.path, res.Code, res.Body.String())
		}
	}
}

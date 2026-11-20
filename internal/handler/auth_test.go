package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type fixedLimiter struct {
	allow bool
	err   error
}

func (l fixedLimiter) Allow(context.Context, string, string, int) (bool, error) {
	return l.allow, l.err
}
func TestLimiterFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		limiter fixedLimiter
		status  int
	}{
		{fixedLimiter{false, nil}, 429}, {fixedLimiter{true, errors.New("redis failed")}, 503},
	} {
		h := server.New()
		Auth{Limiter: tc.limiter}.RegisterRoutes(h)
		result := ut.PerformRequest(h.Engine, "POST", "/api/v1/auth/login", nil, ut.Header{Key: "Content-Type", Value: "application/json"})
		if result.Code != tc.status {
			t.Fatal(result.Code)
		}
	}
}

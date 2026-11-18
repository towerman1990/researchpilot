package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
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
		a := apiServer{limiter: tc.limiter}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/login", nil)
		if a.allow(w, r, "login", "user", 1) || w.Code != tc.status {
			t.Fatal(w.Code)
		}
	}
}
func TestClientAddressIgnoresUntrustedForwarding(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("X-Forwarded-For", "1.1.1.1")
	if clientAddress(r) != "192.0.2.1" {
		t.Fatal("untrusted forwarding accepted")
	}
}
func TestExpiredCursorBoundaries(t *testing.T) {
	if cursorExpired(0, 1) || cursorExpired(10, 11) || !cursorExpired(9, 11) {
		t.Fatal("invalid replay floor")
	}
}

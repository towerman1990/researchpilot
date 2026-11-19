package main

import (
	"context"
	"errors"
	"net"
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

func TestForwardedClientAddress(t *testing.T) {
	trusted := []net.IP{net.ParseIP("172.20.0.5")}
	for _, tc := range []struct{ peer, forwarded, want string }{
		{"172.20.0.5", "203.0.113.7", "203.0.113.7"},
		{"172.20.0.6", "203.0.113.7", "172.20.0.6"},
		{"172.20.0.5", "203.0.113.7, 192.0.2.4", "172.20.0.5"},
		{"172.20.0.5", "invalid", "172.20.0.5"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("X-Forwarded-For", tc.forwarded)
		if got := forwardedClientAddress(r, tc.peer, trusted); got != tc.want {
			t.Errorf("peer=%s forwarded=%s got=%s", tc.peer, tc.forwarded, got)
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Add("X-Forwarded-For", "203.0.113.7")
	r.Header.Add("X-Forwarded-For", "192.0.2.4")
	if got := forwardedClientAddress(r, "172.20.0.5", trusted); got != "172.20.0.5" {
		t.Fatal("accepted repeated header")
	}
}

func TestClientAddressResolvesConfiguredProxy(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_HOST", "localhost")
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := clientAddress(r); got != "203.0.113.7" {
		t.Fatalf("trusted proxy address = %s", got)
	}
	r.RemoteAddr = "192.0.2.3:1234"
	if got := clientAddress(r); got != "192.0.2.3" {
		t.Fatalf("untrusted address = %s", got)
	}
}

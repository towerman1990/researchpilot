package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type rateLimiter interface {
	Allow(context.Context, string, string, int) (bool, error)
}
type redisLimiter struct{ client *redis.Client }

var windowCounter = redis.NewScript("local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('EXPIRE',KEYS[1],120) end; return n")

func (l redisLimiter) Allow(ctx context.Context, operation, subject string, limit int) (bool, error) {
	digest := sha256.Sum256([]byte(subject))
	key := fmt.Sprintf("rp:%s:rl:v1:%s:%x:%d", env("APP_ENV", "local"), operation, digest[:16], time.Now().UTC().Unix()/60)
	n, err := windowCounter.Run(ctx, l.client, []string{key}).Int64()
	return err == nil && n <= int64(limit), err
}
func quota(name string, fallback int) int {
	n, err := strconv.Atoi(os.Getenv(name))
	if err == nil && n > 0 && n <= 10000 {
		return n
	}
	return fallback
}
func (a *apiServer) allow(w http.ResponseWriter, r *http.Request, operation, subject string, limit int) bool {
	if a.limiter == nil {
		return true
	} // Unit tests can inject a limiter; production always wires Redis.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	ok, err := a.limiter.Allow(ctx, operation, subject, limit)
	if err != nil {
		problem(w, 503, "rate_limiter_unavailable")
		return false
	}
	if !ok {
		w.Header().Set("Retry-After", "60")
		problem(w, 429, "rate_limited")
		return false
	}
	return true
}
func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	proxyHost := strings.TrimSpace(os.Getenv("TRUSTED_PROXY_HOST"))
	if proxyHost == "" {
		return host
	}
	ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, proxyHost)
	if err != nil {
		return host
	}
	trusted := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		trusted = append(trusted, address.IP)
	}
	return forwardedClientAddress(r, host, trusted)
}

// Only the configured immediate proxy may supply a single client address.
// The proxy must replace untrusted forwarding headers rather than append them.
func forwardedClientAddress(r *http.Request, peer string, trusted []net.IP) string {
	peerIP := net.ParseIP(peer)
	if peerIP == nil {
		return peer
	}
	isTrusted := false
	for _, ip := range trusted {
		if ip.Equal(peerIP) {
			isTrusted = true
			break
		}
	}
	if !isTrusted {
		return peer
	}
	values := r.Header.Values("X-Forwarded-For")
	if len(values) != 1 || strings.Contains(values[0], ",") {
		return peer
	}
	client := net.ParseIP(strings.TrimSpace(values[0]))
	if client == nil {
		return peer
	}
	return client.String()
}
func accountSubject(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

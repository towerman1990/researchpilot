package persistence

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"researchpilot/internal/common/identity"
)

func TestRedisConcurrentRateLimitIntegration(t *testing.T) {
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_REDIS_ADDR not set")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	ctx := t.Context()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal("Redis unavailable", err)
	}
	l := NewRedisLimiter(client, "integration")
	subject := identity.New(16)
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := l.Allow(ctx, "test", subject, 5)
			if err != nil {
				t.Error(err)
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 5 {
		t.Fatalf("allowed %d", allowed.Load())
	}
	// Check atomic TTL without scanning unrelated deployment keys.
	digest := sha256.Sum256([]byte(subject))
	minute := time.Now().UTC().Unix() / 60
	key := fmt.Sprintf("rp:integration:rl:v1:test:%x:%d", digest[:16], minute)
	ttl, err := client.TTL(ctx, key).Result()
	if err == nil && ttl <= 0 {
		previous := fmt.Sprintf("rp:integration:rl:v1:test:%x:%d", digest[:16], minute-1)
		ttl, err = client.TTL(ctx, previous).Result()
	}
	if err != nil || ttl <= 0 || ttl > 120*time.Second {
		t.Fatal(ttl, err)
	}
}

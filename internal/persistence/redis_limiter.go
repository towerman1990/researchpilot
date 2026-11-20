package persistence

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisLimiter struct {
	client    *redis.Client
	namespace string
}

var windowCounter = redis.NewScript("local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('EXPIRE',KEYS[1],120) end; return n")

func (l *RedisLimiter) Allow(ctx context.Context, operation, subject string, limit int) (bool, error) {
	digest := sha256.Sum256([]byte(subject))
	key := fmt.Sprintf("rp:%s:rl:v1:%s:%x:%d", l.namespace, operation, digest[:16], time.Now().UTC().Unix()/60)
	n, err := windowCounter.Run(ctx, l.client, []string{key}).Int64()
	return err == nil && n <= int64(limit), err
}

// NewRedisLimiter creates the shared request limiter.
func NewRedisLimiter(client *redis.Client, namespace string) *RedisLimiter {
	return &RedisLimiter{client: client, namespace: namespace}
}

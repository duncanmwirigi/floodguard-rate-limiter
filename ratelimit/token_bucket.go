package ratelimit

import (
	"context"
	"sync"

	"golang.org/x/time/rate"
)

// tokenBucket is an in-memory per-key token bucket backed by golang.org/x/time/rate.
type tokenBucket struct {
	rate    rate.Limit
	burst   int
	mu      sync.Mutex
	buckets map[string]*rate.Limiter
}

// NewTokenBucket returns an in-memory Limiter with independent buckets per key.
// Keys are typically user IDs or client IPs.
func NewTokenBucket(cfg TokenBucketConfig) Limiter {
	cfg = cfg.withDefaults()
	return &tokenBucket{
		rate:    cfg.Rate,
		burst:   cfg.Burst,
		buckets: make(map[string]*rate.Limiter),
	}
}

func (t *tokenBucket) Allow(ctx context.Context, key string) (bool, error) {
	if key == "" {
		return false, ErrKeyRequired
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return t.limiter(key).Allow(), nil
}

func (t *tokenBucket) limiter(key string) *rate.Limiter {
	t.mu.Lock()
	defer t.mu.Unlock()

	lim, ok := t.buckets[key]
	if !ok {
		lim = rate.NewLimiter(t.rate, t.burst)
		t.buckets[key] = lim
	}
	return lim
}

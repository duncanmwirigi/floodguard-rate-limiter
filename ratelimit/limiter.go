// Package ratelimit provides keyed rate limiters for HTTP and RPC services.
//
// Use [NewTokenBucket] for in-memory per-key token buckets, or
// [NewRedisSlidingWindow] for distributed sliding-window limits backed by Redis.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/time/rate"
)

// Limiter decides whether a keyed request (user ID, IP, etc.) may proceed.
type Limiter interface {
	Allow(ctx context.Context, key string) (bool, error)
}

// ErrKeyRequired is returned when Allow is called with an empty key.
var ErrKeyRequired = errors.New("ratelimit: key is required")

// ErrNilClient is returned when a Redis-backed limiter is constructed without a client.
var ErrNilClient = errors.New("ratelimit: redis client is required")

// ErrInvalidConfig is returned when limiter configuration is invalid.
var ErrInvalidConfig = errors.New("ratelimit: invalid configuration")

// TokenBucketConfig configures an in-memory per-key token bucket.
type TokenBucketConfig struct {
	// Rate is the sustained requests-per-second limit.
	Rate rate.Limit
	// Burst is the maximum number of tokens that can be consumed at once.
	Burst int
}

func (c TokenBucketConfig) withDefaults() TokenBucketConfig {
	if c.Rate <= 0 {
		c.Rate = rate.Limit(10)
	}
	if c.Burst <= 0 {
		c.Burst = 20
	}
	return c
}

// SlidingWindowConfig configures a Redis-backed rolling-window limiter.
type SlidingWindowConfig struct {
	// Limit is the maximum number of requests allowed within Window.
	Limit int
	// Window is the rolling time window.
	Window time.Duration
}

func (c SlidingWindowConfig) validate() error {
	if c.Limit <= 0 {
		return fmt.Errorf("%w: limit must be positive", ErrInvalidConfig)
	}
	if c.Window <= 0 {
		return fmt.Errorf("%w: window must be positive", ErrInvalidConfig)
	}
	return nil
}

// Package floodguard protects HTTP and gRPC services from rapid or abusive
// requests using rate limiting, idempotency, per-resource locking, and
// velocity-based anomaly detection.
//
// Use [New] to construct a [Guard], then either call [Guard.Protect] directly
// or wrap handlers with [github.com/ultimateprogrammer/floodguard/middleware.Handler].
//
// Request pipeline (see middleware.Handler):
//
//  1. IP rate limit
//  2. Account rate limit
//  3. Idempotency check
//  4. Velocity rules
//  5. Distributed lock
//  6. Handler → release lock → cache idempotent result
package floodguard

import (
	"context"
	"errors"
	"fmt"

	"github.com/ultimateprogrammer/floodguard/idempotency"
	"github.com/ultimateprogrammer/floodguard/lock"
	"github.com/ultimateprogrammer/floodguard/ratelimit"
	"github.com/ultimateprogrammer/floodguard/velocity"
)

// RejectReason explains why a request was blocked.
type RejectReason string

// Rejection reasons returned in [Result.Reason] and [RejectedError.Reason].
const (
	RejectIPRateLimit RejectReason = "ip_rate_limit"
	RejectRateLimit   RejectReason = "rate_limit"
	RejectVelocity    RejectReason = "velocity"
	RejectLocked      RejectReason = "locked"
	RejectCached      RejectReason = "cached"
)

// Request carries the keys needed to evaluate protections.
type Request struct {
	// Key identifies the account or resource (user ID, wallet, etc.).
	Key string
	// IPKey identifies the client IP for the first rate-limit tier.
	IPKey string
	// IdempotencyKey deduplicates mutating requests when non-empty.
	IdempotencyKey string
	// Action labels the request for velocity tracking (e.g. "withdraw", "bet").
	Action string
}

// Result reports the outcome of a protection check.
type Result struct {
	// Allowed is false when the request was blocked or should replay a cached response.
	Allowed bool
	// Reason describes why Allowed is false.
	Reason RejectReason
	// CachedResponse holds a prior response when Reason is [RejectCached].
	CachedResponse []byte
}

// RejectedError is returned by helpers such as [middleware.ProtectFunc]
// when [Guard.Protect] denies a request.
type RejectedError struct {
	Reason RejectReason
	Cached []byte
}

// Error implements the error interface.
func (e RejectedError) Error() string {
	return fmt.Sprintf("floodguard: request rejected (%s)", e.Reason)
}

// Is reports whether target is a [RejectedError].
func (e RejectedError) Is(target error) bool {
	_, ok := target.(RejectedError)
	return ok
}

// As copies e into target when target is a *RejectedError.
func (e RejectedError) As(target any) bool {
	t, ok := target.(*RejectedError)
	if !ok {
		return false
	}
	*t = e
	return true
}

// Config wires the sub-systems. Zero values use sensible in-memory defaults.
type Config struct {
	// IPRateLimiter caps traffic per client IP (layer 1). Nil skips the IP tier.
	IPRateLimiter ratelimit.Limiter
	// RateLimiter caps traffic per account/resource (layer 2).
	RateLimiter ratelimit.Limiter
	Idempotency idempotency.Config
	Lock        lock.Config
	Velocity    velocity.Config
}

// Guard coordinates rate limiting, idempotency, locking, and velocity checks.
type Guard struct {
	ipRate      ratelimit.Limiter
	rate        ratelimit.Limiter
	idempotency *idempotency.Manager
	lock        *lock.Manager
	velocity    *velocity.Detector
}

// New builds a Guard from cfg. All storage backends default to in-memory implementations.
func New(cfg Config) *Guard {
	rateLimiter := cfg.RateLimiter
	if rateLimiter == nil {
		rateLimiter = ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{})
	}

	return &Guard{
		ipRate:      cfg.IPRateLimiter,
		rate:        rateLimiter,
		idempotency: idempotency.New(cfg.Idempotency),
		lock:        lock.New(cfg.Lock),
		velocity:    velocity.New(cfg.Velocity),
	}
}

// Protect runs IP rate limit, account rate limit, idempotency, and velocity checks
// for req in that order. Use [Guard.WithLock] or [Guard.TryLock] before mutating state.
func (g *Guard) Protect(ctx context.Context, req Request) (Result, error) {
	if req.Key == "" {
		return Result{}, ErrKeyRequired
	}

	if g.ipRate != nil && req.IPKey != "" {
		allowed, err := g.ipRate.Allow(ctx, req.IPKey)
		if err != nil {
			return Result{}, fmt.Errorf("floodguard: ip rate limit: %w", err)
		}
		if !allowed {
			return Result{Allowed: false, Reason: RejectIPRateLimit}, nil
		}
	}

	allowed, err := g.rate.Allow(ctx, req.Key)
	if err != nil {
		return Result{}, fmt.Errorf("floodguard: rate limit: %w", err)
	}
	if !allowed {
		return Result{Allowed: false, Reason: RejectRateLimit}, nil
	}

	if req.IdempotencyKey != "" {
		process, cached, err := g.idempotency.Begin(ctx, req.IdempotencyKey)
		if err != nil {
			if errors.Is(err, idempotency.ErrInFlight) {
				return Result{}, duplicateInFlightError(err)
			}
			return Result{}, fmt.Errorf("floodguard: idempotency: %w", err)
		}
		if len(cached) > 0 {
			return Result{Allowed: false, Reason: RejectCached, CachedResponse: cached}, nil
		}
		if !process {
			return Result{}, duplicateInFlightError(idempotency.ErrInFlight)
		}
	}

	velOK, _, err := g.velocity.Check(ctx, velocityKey(req))
	if err != nil {
		return Result{}, fmt.Errorf("floodguard: velocity: %w", err)
	}
	if !velOK {
		return Result{Allowed: false, Reason: RejectVelocity}, nil
	}

	return Result{Allowed: true}, nil
}

// CompleteIdempotency stores the response for an idempotent request.
func (g *Guard) CompleteIdempotency(ctx context.Context, idempotencyKey string, response []byte) error {
	if err := g.idempotency.Complete(ctx, idempotencyKey, response); err != nil {
		return fmt.Errorf("floodguard: complete idempotency: %w", err)
	}
	return nil
}

// WithLock runs fn while holding an exclusive lock on key.
func (g *Guard) WithLock(ctx context.Context, key string, fn func(context.Context) error) error {
	if err := g.lock.With(ctx, key, fn); err != nil {
		return fmt.Errorf("floodguard: with lock: %w", err)
	}
	return nil
}

// TryLock attempts to acquire lock for key without blocking.
// The returned release function must be called to free the lock.
func (g *Guard) TryLock(ctx context.Context, key string) (release func() error, err error) {
	release, err = g.lock.TryAcquire(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("floodguard: try lock: %w", err)
	}
	return release, nil
}

// IPRateLimiter exposes the per-IP rate limiter (layer 1).
func (g *Guard) IPRateLimiter() ratelimit.Limiter {
	return g.ipRate
}

// RateLimiter exposes the per-account rate limiter (layer 2).
func (g *Guard) RateLimiter() ratelimit.Limiter {
	return g.rate
}

// Idempotency exposes the idempotency manager.
func (g *Guard) Idempotency() *idempotency.Manager {
	return g.idempotency
}

// LockManager exposes the lock manager.
func (g *Guard) LockManager() *lock.Manager {
	return g.lock
}

// Velocity exposes the velocity engine.
func (g *Guard) Velocity() *velocity.Detector {
	return g.velocity
}

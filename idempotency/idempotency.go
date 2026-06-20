// Package idempotency deduplicates mutating requests by idempotency key,
// returning cached responses for retries and rejecting in-flight duplicates.
package idempotency

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrKeyRequired is returned when an empty idempotency key is used.
var ErrKeyRequired = errors.New("idempotency: key is required")

// ErrInFlight is returned when another request with the same key is still processing.
var ErrInFlight = errors.New("idempotency: request already in flight")

// ClaimResult is the outcome of an atomic idempotency claim.
type ClaimResult struct {
	// Process is true when this caller should execute the handler.
	Process bool
	// Cached holds a prior successful response when the key was already completed.
	Cached []byte
	// InFlight is true when another caller holds the same key without a result yet.
	InFlight bool
}

// Store tracks idempotency keys and cached responses.
type Store interface {
	// Claim atomically registers key for ttl using SETNX (or equivalent).
	Claim(ctx context.Context, key string, ttl time.Duration) (ClaimResult, error)
	// Complete stores response for key and clears the in-flight claim.
	Complete(ctx context.Context, key string, response []byte, ttl time.Duration) error
}

// Manager deduplicates requests by idempotency key.
type Manager struct {
	store Store
	ttl   time.Duration
}

// Config configures idempotency behavior.
type Config struct {
	// Store persists idempotency state. Defaults to an in-memory store.
	Store Store
	// TTL is how long keys and cached responses are retained.
	TTL time.Duration
}

// New creates a Manager. Default TTL is 24 hours.
func New(cfg Config) *Manager {
	store := cfg.Store
	if store == nil {
		store = NewMemoryStore()
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Manager{store: store, ttl: ttl}
}

// Begin atomically claims key. Returns cached response for replays, or [ErrInFlight]
// when another request with the same key is still processing.
func (m *Manager) Begin(ctx context.Context, key string) (process bool, cached []byte, err error) {
	if key == "" {
		return false, nil, ErrKeyRequired
	}

	result, err := m.store.Claim(ctx, key, m.ttl)
	if err != nil {
		return false, nil, fmt.Errorf("idempotency: claim: %w", err)
	}
	if len(result.Cached) > 0 {
		return false, result.Cached, nil
	}
	if result.InFlight {
		return false, nil, ErrInFlight
	}
	return result.Process, nil, nil
}

// Complete stores the response body for a completed idempotent request.
func (m *Manager) Complete(ctx context.Context, key string, response []byte) error {
	if key == "" {
		return ErrKeyRequired
	}
	if err := m.store.Complete(ctx, key, response, m.ttl); err != nil {
		return fmt.Errorf("idempotency: complete: %w", err)
	}
	return nil
}

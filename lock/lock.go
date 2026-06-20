// Package lock provides distributed mutual exclusion for account and resource keys,
// preventing race conditions such as double-spend during concurrent withdrawals.
package lock

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Lock is a held distributed lock. Call [Lock.Release] when the critical section finishes.
type Lock interface {
	Release(ctx context.Context) error
}

// Client acquires locks for resource keys (accounts, wallets, etc.).
type Client interface {
	Acquire(ctx context.Context, resourceKey string, ttl time.Duration) (Lock, error)
}

// ErrNotAcquired is returned when another caller holds the lock.
var ErrNotAcquired = errors.New("lock: not acquired")

// ErrKeyRequired is returned when resourceKey is empty.
var ErrKeyRequired = errors.New("lock: key is required")

// ErrInvalidTTL is returned when a non-positive TTL is supplied.
var ErrInvalidTTL = errors.New("lock: ttl must be positive")

// ErrNilCallback is returned when With is called with a nil function.
var ErrNilCallback = errors.New("lock: fn is required")

// Manager wraps a [Client] with a default TTL for convenience.
type Manager struct {
	client Client
	ttl    time.Duration
}

// Config configures lock behavior.
type Config struct {
	// Client acquires locks. Defaults to an in-memory client.
	Client Client
	// TTL is the lock lease duration.
	TTL time.Duration
}

// New creates a Manager. Default TTL is 30 seconds; default client is in-memory.
func New(cfg Config) *Manager {
	client := cfg.Client
	if client == nil {
		client = NewMemory()
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &Manager{client: client, ttl: ttl}
}

// Acquire attempts to take lock for key with the manager's default TTL.
func (m *Manager) Acquire(ctx context.Context, resourceKey string) (Lock, error) {
	l, err := m.client.Acquire(ctx, resourceKey, m.ttl)
	if err != nil {
		return nil, fmt.Errorf("lock: acquire: %w", err)
	}
	return l, nil
}

// With runs fn while holding lock for key. Lock is released when fn returns.
func (m *Manager) With(ctx context.Context, key string, fn func(context.Context) error) error {
	if key == "" {
		return ErrKeyRequired
	}
	if fn == nil {
		return ErrNilCallback
	}

	l, err := m.client.Acquire(ctx, key, m.ttl)
	if err != nil {
		return fmt.Errorf("lock: acquire: %w", err)
	}
	defer func() { _ = l.Release(context.Background()) }()

	return fn(ctx)
}

// TryAcquire attempts to take lock without blocking.
// The returned release function must be called to free the lock.
func (m *Manager) TryAcquire(ctx context.Context, key string) (release func() error, err error) {
	if key == "" {
		return nil, ErrKeyRequired
	}

	l, err := m.client.Acquire(ctx, key, m.ttl)
	if err != nil {
		return nil, fmt.Errorf("lock: acquire: %w", err)
	}

	release = func() error {
		return l.Release(context.Background())
	}
	return release, nil
}

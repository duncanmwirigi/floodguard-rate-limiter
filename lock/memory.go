package lock

import (
	"context"
	"sync"
	"time"
)

// Memory is an in-process lock Client for single-instance services and tests.
type Memory struct {
	mu    sync.Mutex
	locks map[string]lockEntry
}

type lockEntry struct {
	token     string
	expiresAt time.Time
}

type memoryLock struct {
	store *Memory
	key   string
	token string
}

// NewMemory returns an in-process lock Client.
func NewMemory() *Memory {
	return &Memory{locks: make(map[string]lockEntry)}
}

// Acquire attempts to take lock for resourceKey.
func (m *Memory) Acquire(_ context.Context, resourceKey string, ttl time.Duration) (Lock, error) {
	if resourceKey == "" {
		return nil, ErrKeyRequired
	}

	token, err := randomToken()
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	if entry, ok := m.locks[resourceKey]; ok {
		if now.Before(entry.expiresAt) {
			return nil, ErrNotAcquired
		}
		delete(m.locks, resourceKey)
	}

	m.locks[resourceKey] = lockEntry{token: token, expiresAt: now.Add(ttl)}
	return &memoryLock{store: m, key: resourceKey, token: token}, nil
}

// Release relinquishes the in-memory lock when the token still matches.
func (l *memoryLock) Release(_ context.Context) error {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()

	entry, ok := l.store.locks[l.key]
	if !ok || entry.token != l.token {
		return nil
	}
	delete(l.store.locks, l.key)
	return nil
}

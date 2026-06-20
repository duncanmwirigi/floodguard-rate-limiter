package idempotency

import (
	"context"
	"sync"
	"time"
)

type memoryEntry struct {
	inFlight  bool
	value     []byte
	expiresAt time.Time
}

type memoryStore struct {
	mu      sync.Mutex
	entries map[string]memoryEntry
}

// NewMemoryStore returns an in-process idempotency Store.
func NewMemoryStore() Store {
	return &memoryStore{entries: make(map[string]memoryEntry)}
}

func (s *memoryStore) Claim(_ context.Context, key string, ttl time.Duration) (ClaimResult, error) {
	if key == "" {
		return ClaimResult{}, ErrKeyRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	entry, ok := s.entries[key]
	if ok && now.After(entry.expiresAt) {
		delete(s.entries, key)
		ok = false
	}

	if ok && len(entry.value) > 0 {
		out := make([]byte, len(entry.value))
		copy(out, entry.value)
		return ClaimResult{Cached: out}, nil
	}

	if ok && entry.inFlight {
		return ClaimResult{InFlight: true}, nil
	}

	s.entries[key] = memoryEntry{
		inFlight:  true,
		expiresAt: now.Add(ttl),
	}
	return ClaimResult{Process: true}, nil
}

func (s *memoryStore) Complete(_ context.Context, key string, response []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	copied := make([]byte, len(response))
	copy(copied, response)
	s.entries[key] = memoryEntry{
		value:     copied,
		expiresAt: time.Now().Add(ttl),
	}
	return nil
}

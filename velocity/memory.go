package velocity

import (
	"context"
	"sync"
	"time"
)

type memoryStore struct {
	mu      sync.Mutex
	actions map[string][]time.Time
}

// NewMemoryStore returns an in-process velocity Store.
func NewMemoryStore() Store {
	return &memoryStore{actions: make(map[string][]time.Time)}
}

func (s *memoryStore) Record(_ context.Context, key string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actions[key] = append(s.actions[key], at)
	return nil
}

func (s *memoryStore) Count(_ context.Context, key string, window time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.trim(key, window)), nil
}

func (s *memoryStore) Last(_ context.Context, key string) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	times := s.actions[key]
	if len(times) == 0 {
		return time.Time{}, false, nil
	}
	return times[len(times)-1], true, nil
}

func (s *memoryStore) trim(key string, window time.Duration) []time.Time {
	cutoff := time.Now().Add(-window)
	times := s.actions[key]
	kept := times[:0]
	for _, ts := range times {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	s.actions[key] = kept
	return kept
}

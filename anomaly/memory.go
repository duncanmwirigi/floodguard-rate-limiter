package anomaly

import (
	"context"
	"sync"
	"time"
)

type memoryCounter struct {
	mu      sync.Mutex
	buckets map[string]int64 // minuteKey -> count
}

// NewMemoryCounter returns an in-memory Counter for tests and dev.
func NewMemoryCounter() Counter {
	return &memoryCounter{buckets: make(map[string]int64)}
}

func (c *memoryCounter) Increment(_ context.Context, metric string, at time.Time, delta int64) error {
	key := minuteKey(metric, at.Truncate(time.Minute))
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buckets[key] += delta
	return nil
}

func (c *memoryCounter) CountInWindow(_ context.Context, metric string, from, to time.Time) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var total int64
	for t := from.Truncate(time.Minute); t.Before(to); t = t.Add(time.Minute) {
		total += c.buckets[minuteKey(metric, t)]
	}
	return total, nil
}

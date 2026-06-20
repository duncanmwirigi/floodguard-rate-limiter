package ledger

import (
	"context"
	"fmt"
	"sync"
)

type memoryStore struct {
	mu      sync.Mutex
	records map[string][]Record
}

// NewMemoryStore returns an in-memory append-only Store.
func NewMemoryStore() Store {
	return &memoryStore{records: make(map[string][]Record)}
}

func (s *memoryStore) Append(_ context.Context, rec Record) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[rec.AccountID] = append(s.records[rec.AccountID], rec)
	return rec, nil
}

func (s *memoryStore) ListByAccount(_ context.Context, accountID string) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, len(s.records[accountID]))
	copy(out, s.records[accountID])
	return out, nil
}

func (s *memoryStore) SetRecordHashForTest(_ context.Context, accountID string, index int, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	recs := s.records[accountID]
	if index < 0 || index >= len(recs) {
		return fmt.Errorf("ledger: index out of range")
	}
	recs[index].RecordHash = hash
	s.records[accountID] = recs
	return nil
}

package devicetrust

import (
	"context"
	"sync"
	"time"
)

type memoryStore struct {
	mu      sync.Mutex
	devices map[string]map[string]DeviceRecord // account -> fingerprint -> record
}

// NewMemoryStore returns an in-process device Store.
func NewMemoryStore() Store {
	return &memoryStore{devices: make(map[string]map[string]DeviceRecord)}
}

func (s *memoryStore) accountKey(accountID, fingerprint string) (map[string]DeviceRecord, DeviceRecord, bool) {
	acct, ok := s.devices[accountID]
	if !ok {
		return nil, DeviceRecord{}, false
	}
	rec, ok := acct[fingerprint]
	return acct, rec, ok
}

func (s *memoryStore) Get(_ context.Context, accountID, fingerprint string) (DeviceRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, rec, ok := s.accountKey(accountID, fingerprint)
	return rec, ok, nil
}

func (s *memoryStore) UpsertSeen(_ context.Context, accountID, fingerprint string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.devices[accountID] == nil {
		s.devices[accountID] = make(map[string]DeviceRecord)
	}
	rec, ok := s.devices[accountID][fingerprint]
	if !ok {
		rec = DeviceRecord{Fingerprint: fingerprint, FirstSeen: at, LastSeen: at}
	} else {
		rec.LastSeen = at
	}
	s.devices[accountID][fingerprint] = rec
	return nil
}

func (s *memoryStore) MarkTrusted(_ context.Context, accountID, fingerprint string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.devices[accountID] == nil {
		s.devices[accountID] = make(map[string]DeviceRecord)
	}
	rec, ok := s.devices[accountID][fingerprint]
	if !ok {
		rec = DeviceRecord{Fingerprint: fingerprint, FirstSeen: at, LastSeen: at}
	} else {
		rec.LastSeen = at
	}
	rec.Trusted = true
	s.devices[accountID][fingerprint] = rec
	return nil
}

// tamperForTest mutates a stored record (test-only simulation of DB tampering).
func (s *memoryStore) tamperForTest(accountID, fingerprint string, trusted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.devices[accountID] == nil {
		return
	}
	rec := s.devices[accountID][fingerprint]
	rec.Trusted = trusted
	s.devices[accountID][fingerprint] = rec
}

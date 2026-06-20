package devicetrust

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisStore struct {
	client *redis.Client
	prefix string
}

// NewRedisStore returns a Redis-backed device Store.
func NewRedisStore(client *redis.Client, keyPrefix string) Store {
	if keyPrefix == "" {
		keyPrefix = "floodguard:devicetrust"
	}
	return &redisStore{client: client, prefix: keyPrefix}
}

func (s *redisStore) key(accountID, fingerprint string) string {
	return fmt.Sprintf("%s:%s:%s", s.prefix, accountID, fingerprint)
}

func (s *redisStore) Get(ctx context.Context, accountID, fingerprint string) (DeviceRecord, bool, error) {
	val, err := s.client.Get(ctx, s.key(accountID, fingerprint)).Bytes()
	if err == redis.Nil {
		return DeviceRecord{}, false, nil
	}
	if err != nil {
		return DeviceRecord{}, false, err
	}
	var rec DeviceRecord
	if err := json.Unmarshal(val, &rec); err != nil {
		return DeviceRecord{}, false, err
	}
	return rec, true, nil
}

func (s *redisStore) UpsertSeen(ctx context.Context, accountID, fingerprint string, at time.Time) error {
	rec, found, err := s.Get(ctx, accountID, fingerprint)
	if err != nil {
		return err
	}
	if !found {
		rec = DeviceRecord{Fingerprint: fingerprint, FirstSeen: at, LastSeen: at}
	} else {
		rec.LastSeen = at
	}
	return s.save(ctx, accountID, fingerprint, rec)
}

func (s *redisStore) MarkTrusted(ctx context.Context, accountID, fingerprint string, at time.Time) error {
	rec, found, err := s.Get(ctx, accountID, fingerprint)
	if err != nil {
		return err
	}
	if !found {
		rec = DeviceRecord{Fingerprint: fingerprint, FirstSeen: at, LastSeen: at}
	} else {
		rec.LastSeen = at
	}
	rec.Trusted = true
	return s.save(ctx, accountID, fingerprint, rec)
}

func (s *redisStore) save(ctx context.Context, accountID, fingerprint string, rec DeviceRecord) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.key(accountID, fingerprint), b, 0).Err()
}

package velocity

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisStore struct {
	client *redis.Client
	prefix string
}

// NewRedisStore returns a distributed velocity Store backed by Redis.
func NewRedisStore(client *redis.Client, keyPrefix string) Store {
	if keyPrefix == "" {
		keyPrefix = "floodguard:velocity"
	}
	return &redisStore{client: client, prefix: keyPrefix}
}

func (s *redisStore) rkey(key string) string {
	return fmt.Sprintf("%s:%s", s.prefix, key)
}

func (s *redisStore) Record(ctx context.Context, key string, at time.Time) error {
	rkey := s.rkey(key)
	member := fmt.Sprintf("%d", at.UnixNano())
	pipe := s.client.TxPipeline()
	pipe.ZAdd(ctx, rkey, redis.Z{Score: float64(at.UnixMilli()), Member: member})
	pipe.Expire(ctx, rkey, 24*time.Hour)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *redisStore) Count(ctx context.Context, key string, window time.Duration) (int, error) {
	rkey := s.rkey(key)
	cutoff := time.Now().Add(-window).UnixMilli()

	pipe := s.client.TxPipeline()
	pipe.ZRemRangeByScore(ctx, rkey, "0", fmt.Sprintf("%d", cutoff))
	countCmd := pipe.ZCard(ctx, rkey)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return int(countCmd.Val()), nil
}

func (s *redisStore) Last(ctx context.Context, key string) (time.Time, bool, error) {
	rkey := s.rkey(key)
	vals, err := s.client.ZRevRangeWithScores(ctx, rkey, 0, 0).Result()
	if err != nil {
		return time.Time{}, false, err
	}
	if len(vals) == 0 {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(int64(vals[0].Score)), true, nil
}

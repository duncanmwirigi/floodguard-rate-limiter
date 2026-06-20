package anomaly

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCounter stores minute-bucketed counts in Redis sorted sets.
type RedisCounter struct {
	client *redis.Client
	prefix string
}

// NewRedisCounter creates a Redis-backed Counter.
func NewRedisCounter(client *redis.Client, prefix string) *RedisCounter {
	if prefix == "" {
		prefix = "anomaly"
	}
	return &RedisCounter{client: client, prefix: prefix}
}

func (c *RedisCounter) redisKey(metric string) string {
	return fmt.Sprintf("%s:%s", c.prefix, metric)
}

func (c *RedisCounter) Increment(ctx context.Context, metric string, at time.Time, delta int64) error {
	minute := at.UTC().Truncate(time.Minute).Unix() / 60
	key := c.redisKey(metric)
	member := fmt.Sprintf("%d", minute)
	pipe := c.client.Pipeline()
	pipe.ZIncrBy(ctx, key, float64(delta), member)
	pipe.Expire(ctx, key, 8*24*time.Hour) // retain ~7 days of buckets
	_, err := pipe.Exec(ctx)
	return err
}

func (c *RedisCounter) CountInWindow(ctx context.Context, metric string, from, to time.Time) (int64, error) {
	key := c.redisKey(metric)
	minFrom := from.UTC().Truncate(time.Minute).Unix() / 60
	minTo := to.UTC().Truncate(time.Minute).Unix()/60 - 1
	if minTo < minFrom {
		return 0, nil
	}

	members, err := c.client.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%d", minFrom),
		Max: fmt.Sprintf("%d", minTo),
	}).Result()
	if err != nil {
		return 0, err
	}

	var total int64
	for _, m := range members {
		score, err := c.client.ZScore(ctx, key, m).Result()
		if err != nil {
			return 0, err
		}
		total += int64(score)
	}
	return total, nil
}

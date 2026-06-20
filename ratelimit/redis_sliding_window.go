package ratelimit

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisSlidingWindow enforces a distributed rolling-window limit via Redis sorted sets.
type redisSlidingWindow struct {
	client *redis.Client
	prefix string
	cfg    SlidingWindowConfig
	script *redis.Script
	seq    atomic.Uint64
}

// NewRedisSlidingWindow returns a Limiter backed by Redis for multi-instance deployments.
func NewRedisSlidingWindow(client *redis.Client, keyPrefix string, cfg SlidingWindowConfig) (Limiter, error) {
	if client == nil {
		return nil, ErrNilClient
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if keyPrefix == "" {
		keyPrefix = "floodguard:ratelimit"
	}

	return &redisSlidingWindow{
		client: client,
		prefix: keyPrefix,
		cfg:    cfg,
		script: redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window_start = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member = ARGV[4]
local ttl_ms = tonumber(ARGV[5])

redis.call("ZREMRANGEBYSCORE", key, "0", window_start)
redis.call("ZADD", key, now, member)
local count = redis.call("ZCARD", key)
if count > limit then
  redis.call("ZREM", key, member)
  return 0
end
redis.call("PEXPIRE", key, ttl_ms)
return 1
`),
	}, nil
}

func (s *redisSlidingWindow) Allow(ctx context.Context, key string) (bool, error) {
	if key == "" {
		return false, ErrKeyRequired
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	now := time.Now()
	rkey := fmt.Sprintf("%s:sw:%s", s.prefix, key)
	member := fmt.Sprintf("%d-%d", now.UnixNano(), s.seq.Add(1))
	ttl := s.cfg.Window + time.Second

	res, err := s.script.Run(ctx, s.client, []string{rkey},
		now.UnixMilli(),
		now.Add(-s.cfg.Window).UnixMilli(),
		s.cfg.Limit,
		member,
		ttl.Milliseconds(),
	).Int()
	if err != nil {
		return false, fmt.Errorf("ratelimit: redis allow: %w", err)
	}
	return res == 1, nil
}

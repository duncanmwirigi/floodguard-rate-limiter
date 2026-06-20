package idempotency

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisStore struct {
	client *redis.Client
	prefix string
	claim  *redis.Script
	done   *redis.Script
}

// NewRedisStore returns a distributed idempotency Store backed by Redis.
// Claim uses SETNX atomically via Lua — no check-then-set race.
func NewRedisStore(client *redis.Client, keyPrefix string) Store {
	if keyPrefix == "" {
		keyPrefix = "floodguard:idempotency"
	}

	return &redisStore{
		client: client,
		prefix: keyPrefix,
		claim: redis.NewScript(`
local inflight = KEYS[1]
local result = KEYS[2]
local ttl_ms = tonumber(ARGV[1])

local cached = redis.call("GET", result)
if cached then
  return {1, cached}
end

local claimed = redis.call("SET", inflight, "1", "NX", "PX", ttl_ms)
if claimed then
  return {0, ""}
end

return {2, ""}
`),
		done: redis.NewScript(`
local inflight = KEYS[1]
local result = KEYS[2]
local ttl_ms = tonumber(ARGV[1])
local body = ARGV[2]

redis.call("SET", result, body, "PX", ttl_ms)
redis.call("DEL", inflight)
return 1
`),
	}
}

func (s *redisStore) Claim(ctx context.Context, key string, ttl time.Duration) (ClaimResult, error) {
	if key == "" {
		return ClaimResult{}, ErrKeyRequired
	}
	inflight := fmt.Sprintf("%s:inflight:%s", s.prefix, key)
	resultKey := fmt.Sprintf("%s:result:%s", s.prefix, key)

	raw, err := s.claim.Run(ctx, s.client, []string{inflight, resultKey}, ttl.Milliseconds()).Slice()
	if err != nil {
		return ClaimResult{}, fmt.Errorf("idempotency: redis claim: %w", err)
	}

	status, err := redisInt(raw[0])
	if err != nil {
		return ClaimResult{}, err
	}

	switch status {
	case 0:
		return ClaimResult{Process: true}, nil
	case 1:
		switch v := raw[1].(type) {
		case string:
			return ClaimResult{Cached: []byte(v)}, nil
		case []byte:
			out := make([]byte, len(v))
			copy(out, v)
			return ClaimResult{Cached: out}, nil
		default:
			return ClaimResult{}, fmt.Errorf("idempotency: unexpected cached type %T", raw[1])
		}
	case 2:
		return ClaimResult{InFlight: true}, nil
	default:
		return ClaimResult{}, fmt.Errorf("idempotency: unexpected claim status %d", status)
	}
}

func (s *redisStore) Complete(ctx context.Context, key string, response []byte, ttl time.Duration) error {
	inflight := fmt.Sprintf("%s:inflight:%s", s.prefix, key)
	resultKey := fmt.Sprintf("%s:result:%s", s.prefix, key)
	_, err := s.done.Run(ctx, s.client, []string{inflight, resultKey}, ttl.Milliseconds(), response).Result()
	if err != nil {
		return fmt.Errorf("idempotency: redis complete: %w", err)
	}
	return nil
}

func redisInt(v interface{}) (int, error) {
	switch n := v.(type) {
	case int64:
		return int(n), nil
	case int:
		return n, nil
	default:
		return 0, fmt.Errorf("idempotency: expected integer status, got %T", v)
	}
}

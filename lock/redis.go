package lock

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is a Redlock-style distributed lock Client backed by a single Redis node.
// Each lock uses SET NX with a unique token; Release deletes only when the token matches.
type Redis struct {
	client *redis.Client
	prefix string
}

type redisLock struct {
	client *redis.Client
	key    string
	token  string
}

var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

// NewRedis returns a distributed lock Client backed by Redis.
func NewRedis(client *redis.Client, keyPrefix string) *Redis {
	if keyPrefix == "" {
		keyPrefix = "floodguard:lock"
	}
	return &Redis{client: client, prefix: keyPrefix}
}

// Acquire attempts to take lock for resourceKey with ttl.
func (r *Redis) Acquire(ctx context.Context, resourceKey string, ttl time.Duration) (Lock, error) {
	if resourceKey == "" {
		return nil, ErrKeyRequired
	}
	if ttl <= 0 {
		return nil, ErrInvalidTTL
	}

	token, err := randomToken()
	if err != nil {
		return nil, err
	}

	rkey := fmt.Sprintf("%s:%s", r.prefix, resourceKey)
	ok, err := r.client.SetNX(ctx, rkey, token, ttl).Result()
	if err != nil {
		return nil, fmt.Errorf("lock: redis setnx: %w", err)
	}
	if !ok {
		return nil, ErrNotAcquired
	}

	return &redisLock{client: r.client, key: rkey, token: token}, nil
}

func (l *redisLock) Release(ctx context.Context) error {
	_, err := releaseScript.Run(ctx, l.client, []string{l.key}, l.token).Result()
	if err != nil {
		return fmt.Errorf("lock: redis release: %w", err)
	}
	return nil
}

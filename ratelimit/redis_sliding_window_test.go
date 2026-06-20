package ratelimit_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/duncanmwirigi/floodguard-rate-limiter/ratelimit"
	"github.com/redis/go-redis/v9"
)

func newTestRedisSlidingWindow(t *testing.T, limit int, window time.Duration) (ratelimit.Limiter, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	lim, err := ratelimit.NewRedisSlidingWindow(client, "test", ratelimit.SlidingWindowConfig{
		Limit:  limit,
		Window: window,
	})
	if err != nil {
		t.Fatalf("NewRedisSlidingWindow: %v", err)
	}
	return lim, mr
}

func TestRedisSlidingWindow_Allow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit int
		calls int
		want  []bool
	}{
		{
			name:  "allows up to limit then denies",
			limit: 3,
			calls: 4,
			want:  []bool{true, true, true, false},
		},
		{
			name:  "single request limit",
			limit: 1,
			calls: 2,
			want:  []bool{true, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lim, _ := newTestRedisSlidingWindow(t, tt.limit, time.Minute)
			ctx := context.Background()
			key := "user-1"

			for i := 0; i < tt.calls; i++ {
				got, err := lim.Allow(ctx, key)
				if err != nil {
					t.Fatalf("call %d: %v", i, err)
				}
				if got != tt.want[i] {
					t.Fatalf("call %d: got %v, want %v", i, got, tt.want[i])
				}
			}
		})
	}
}

func TestRedisSlidingWindow_PerKeyIsolation(t *testing.T) {
	t.Parallel()

	lim, _ := newTestRedisSlidingWindow(t, 1, time.Minute)
	ctx := context.Background()

	for _, key := range []string{"acct-1", "acct-2"} {
		ok, err := lim.Allow(ctx, key)
		if err != nil || !ok {
			t.Fatalf("first request for %q should pass", key)
		}
	}

	for _, key := range []string{"acct-1", "acct-2"} {
		ok, err := lim.Allow(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatalf("second request for %q should be denied", key)
		}
	}
}

func TestRedisSlidingWindow_WindowExpiry(t *testing.T) {
	t.Parallel()

	lim, _ := newTestRedisSlidingWindow(t, 1, 200*time.Millisecond)
	ctx := context.Background()
	key := "user-window"

	if ok, err := lim.Allow(ctx, key); err != nil || !ok {
		t.Fatalf("first allow failed: ok=%v err=%v", ok, err)
	}
	if ok, err := lim.Allow(ctx, key); err != nil || ok {
		t.Fatalf("second allow should be denied: ok=%v err=%v", ok, err)
	}

	time.Sleep(250 * time.Millisecond)

	if ok, err := lim.Allow(ctx, key); err != nil || !ok {
		t.Fatalf("after window expiry allow should succeed: ok=%v err=%v", ok, err)
	}
}

func TestRedisSlidingWindow_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	const (
		limit      = 5
		goroutines = 25
	)

	lim, _ := newTestRedisSlidingWindow(t, limit, time.Minute)
	ctx := context.Background()
	key := "user-concurrent"

	var allowed atomic.Int32
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			ok, err := lim.Allow(ctx, key)
			if err != nil {
				t.Errorf("Allow error: %v", err)
				return
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := int(allowed.Load()); got != limit {
		t.Fatalf("allowed %d requests, want exactly %d", got, limit)
	}
}

func TestRedisSlidingWindow_Validation(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	_, err := ratelimit.NewRedisSlidingWindow(nil, "test", ratelimit.SlidingWindowConfig{Limit: 1, Window: time.Minute})
	if !errors.Is(err, ratelimit.ErrNilClient) {
		t.Fatalf("expected ErrNilClient, got %v", err)
	}

	_, err = ratelimit.NewRedisSlidingWindow(client, "test", ratelimit.SlidingWindowConfig{Limit: 0, Window: time.Minute})
	if !errors.Is(err, ratelimit.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}

	_, err = ratelimit.NewRedisSlidingWindow(client, "test", ratelimit.SlidingWindowConfig{Limit: 1, Window: 0})
	if !errors.Is(err, ratelimit.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestRedisSlidingWindow_EmptyKey(t *testing.T) {
	t.Parallel()

	lim, _ := newTestRedisSlidingWindow(t, 5, time.Minute)
	_, err := lim.Allow(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

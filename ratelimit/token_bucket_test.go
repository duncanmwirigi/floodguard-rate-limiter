package ratelimit_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ultimateprogrammer/floodguard/ratelimit"
	"golang.org/x/time/rate"
)

func TestTokenBucket_Allow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     ratelimit.TokenBucketConfig
		key     string
		calls   int
		want    []bool
		wantErr bool
	}{
		{
			name:  "allows up to burst then denies",
			cfg:   ratelimit.TokenBucketConfig{Rate: 0, Burst: 3}, // rate 0 = no refill
			key:   "user-1",
			calls: 4,
			want:  []bool{true, true, true, false},
		},
		{
			name:  "independent keys get independent buckets",
			cfg:   ratelimit.TokenBucketConfig{Rate: 0, Burst: 1},
			key:   "user-a",
			calls: 2,
			want:  []bool{true, false},
		},
		{
			name:    "empty key is rejected",
			cfg:     ratelimit.TokenBucketConfig{Rate: 10, Burst: 10},
			key:     "",
			calls:   1,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lim := ratelimit.NewTokenBucket(tt.cfg)
			ctx := context.Background()

			for i := 0; i < tt.calls; i++ {
				got, err := lim.Allow(ctx, tt.key)
				if tt.wantErr {
					if err == nil {
						t.Fatal("expected error for empty key")
					}
					return
				}
				if err != nil {
					t.Fatalf("call %d: unexpected error: %v", i, err)
				}
				if got != tt.want[i] {
					t.Fatalf("call %d: got %v, want %v", i, got, tt.want[i])
				}
			}
		})
	}
}

func TestTokenBucket_PerKeyIsolation(t *testing.T) {
	t.Parallel()

	lim := ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{Rate: 0, Burst: 1})
	ctx := context.Background()

	for _, key := range []string{"10.0.0.1", "10.0.0.2"} {
		allowed, err := lim.Allow(ctx, key)
		if err != nil || !allowed {
			t.Fatalf("first request for %q should pass", key)
		}
	}

	for _, key := range []string{"10.0.0.1", "10.0.0.2"} {
		allowed, err := lim.Allow(ctx, key)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", key, err)
		}
		if allowed {
			t.Fatalf("second request for %q should be denied", key)
		}
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	t.Parallel()

	lim := ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
		Rate:  rate.Limit(10), // one token every 100ms
		Burst: 1,
	})
	ctx := context.Background()
	key := "user-refill"

	if ok, err := lim.Allow(ctx, key); err != nil || !ok {
		t.Fatalf("first allow: ok=%v err=%v", ok, err)
	}
	if ok, err := lim.Allow(ctx, key); err != nil || ok {
		t.Fatalf("second allow should be denied: ok=%v err=%v", ok, err)
	}

	time.Sleep(150 * time.Millisecond)

	if ok, err := lim.Allow(ctx, key); err != nil || !ok {
		t.Fatalf("after refill allow should succeed: ok=%v err=%v", ok, err)
	}
}

func TestTokenBucket_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	const (
		burst      = 5
		goroutines = 20
	)

	lim := ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
		Rate:  0, // no refill during test
		Burst: burst,
	})
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

	if got := int(allowed.Load()); got != burst {
		t.Fatalf("allowed %d requests, want exactly %d", got, burst)
	}
}

func TestTokenBucket_ContextCancellation(t *testing.T) {
	t.Parallel()

	lim := ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{Rate: 10, Burst: 10})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := lim.Allow(ctx, "user-1")
	if err == nil {
		t.Fatal("expected context error")
	}
}

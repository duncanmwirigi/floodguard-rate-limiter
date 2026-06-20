package lock_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/ultimateprogrammer/floodguard/lock"
)

func TestConcurrentAcquire_OnlyOneAtATime(t *testing.T) {
	t.Parallel()

	clients := []struct {
		name   string
		client lock.Client
	}{
		{name: "memory", client: lock.NewMemory()},
	}

	mr := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	clients = append(clients, struct {
		name   string
		client lock.Client
	}{name: "redis", client: lock.NewRedis(redisClient, "test")})

	for _, tc := range clients {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runConcurrentAcquireTest(t, tc.client)
		})
	}
}

func runConcurrentAcquireTest(t *testing.T, client lock.Client) {
	t.Helper()

	const (
		workers = 20
		key     = "account:42"
		ttl     = time.Minute
	)

	ctx := context.Background()

	// Phase 1: many goroutines race — exactly one should acquire.
	start := make(chan struct{})
	var acquired atomic.Int32
	var rejected atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			l, err := client.Acquire(ctx, key, ttl)
			if errors.Is(err, lock.ErrNotAcquired) {
				rejected.Add(1)
				return
			}
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			defer func() { _ = l.Release(ctx) }()

			if acquired.Add(1) != 1 {
				t.Error("more than one goroutine acquired the lock simultaneously")
			}
			time.Sleep(20 * time.Millisecond)
		}()
	}

	close(start)
	wg.Wait()

	if acquired.Load() != 1 {
		t.Fatalf("acquired = %d, want 1", acquired.Load())
	}
	if rejected.Load() != workers-1 {
		t.Fatalf("rejected = %d, want %d", rejected.Load(), workers-1)
	}

	// Phase 2: sequential acquire → release → acquire — still one at a time.
	var peak atomic.Int32
	for i := 0; i < 5; i++ {
		innerStart := make(chan struct{})
		var innerWG sync.WaitGroup
		var innerHolding atomic.Int32

		for j := 0; j < workers; j++ {
			innerWG.Add(1)
			go func() {
				defer innerWG.Done()
				<-innerStart

				l, err := client.Acquire(ctx, key, ttl)
				if errors.Is(err, lock.ErrNotAcquired) {
					return
				}
				if err != nil {
					t.Errorf("Acquire: %v", err)
					return
				}
				defer func() { _ = l.Release(ctx) }()

				now := innerHolding.Add(1)
				if now > peak.Load() {
					peak.Store(now)
				}
				time.Sleep(5 * time.Millisecond)
				innerHolding.Add(-1)
			}()
		}

		close(innerStart)
		innerWG.Wait()
	}

	if peak.Load() != 1 {
		t.Fatalf("peak concurrent holders = %d, want 1", peak.Load())
	}
}

func TestAcquire_ReleaseAndReacquire(t *testing.T) {
	t.Parallel()

	client := lock.NewMemory()
	ctx := context.Background()
	key := "wallet:1"

	l1, err := client.Acquire(ctx, key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Acquire(ctx, key, time.Minute)
	if !errors.Is(err, lock.ErrNotAcquired) {
		t.Fatalf("second acquire: %v", err)
	}

	if err := l1.Release(ctx); err != nil {
		t.Fatal(err)
	}

	l2, err := client.Acquire(ctx, key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_ = l2.Release(ctx)
}

func TestAcquire_EmptyKey(t *testing.T) {
	t.Parallel()

	_, err := lock.NewMemory().Acquire(context.Background(), "", time.Minute)
	if !errors.Is(err, lock.ErrKeyRequired) {
		t.Fatalf("got %v, want ErrKeyRequired", err)
	}
}

func TestManager_With(t *testing.T) {
	t.Parallel()

	mgr := lock.New(lock.Config{Client: lock.NewMemory()})
	ctx := context.Background()
	key := "acct-1"

	var inside atomic.Bool
	err := mgr.With(ctx, key, func(context.Context) error {
		inside.Store(true)

		_, err := mgr.Acquire(ctx, key)
		if !errors.Is(err, lock.ErrNotAcquired) {
			t.Fatalf("nested acquire should fail: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !inside.Load() {
		t.Fatal("With did not run fn")
	}

	l, err := mgr.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire after With: %v", err)
	}
	_ = l.Release(ctx)
}

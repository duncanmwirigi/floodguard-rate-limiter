package idempotency_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/ultimateprogrammer/floodguard/idempotency"
)

func TestMemoryStore_ConcurrentSameKey(t *testing.T) {
	t.Parallel()
	runConcurrentClaimTest(t, idempotency.NewMemoryStore())
}

func TestRedisStore_ConcurrentSameKey(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	runConcurrentClaimTest(t, idempotency.NewRedisStore(client, "test"))
}

func runConcurrentClaimTest(t *testing.T, store idempotency.Store) {
	t.Helper()

	const (
		key = "idem-concurrent-1"
		ttl = time.Minute
	)

	ctx := context.Background()
	start := make(chan struct{})
	var processed atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			result, err := store.Claim(ctx, key, ttl)
			if err != nil {
				t.Errorf("Claim: %v", err)
				return
			}

			switch {
			case result.Process:
				if processed.Add(1) == 1 {
					time.Sleep(20 * time.Millisecond) // simulate handler work
					if err := store.Complete(ctx, key, []byte(`{"id":1}`), ttl); err != nil {
						t.Errorf("Complete: %v", err)
					}
				}
			case result.InFlight:
				// expected for the loser
			case len(result.Cached) > 0:
				t.Errorf("unexpected cached response before completion")
			}
		}()
	}

	close(start)
	wg.Wait()

	if got := processed.Load(); got != 1 {
		t.Fatalf("processed %d requests, want exactly 1", got)
	}

	// Subsequent claim should replay cached response.
	replay, err := store.Claim(ctx, key, ttl)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Process || replay.InFlight {
		t.Fatalf("expected cached replay, got %+v", replay)
	}
	if string(replay.Cached) != `{"id":1}` {
		t.Fatalf("cached = %q, want %q", replay.Cached, `{"id":1}`)
	}
}

func TestManager_BeginConcurrent(t *testing.T) {
	t.Parallel()

	mgr := idempotency.New(idempotency.Config{Store: idempotency.NewMemoryStore()})
	ctx := context.Background()
	key := "k-1"
	start := make(chan struct{})

	var winners atomic.Int32
	var inFlight atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			process, cached, err := mgr.Begin(ctx, key)
			switch {
			case err != nil:
				if errors.Is(err, idempotency.ErrInFlight) {
					inFlight.Add(1)
					return
				}
				t.Errorf("Begin: %v", err)
			case len(cached) > 0:
				t.Error("unexpected cached response")
			case process:
				winners.Add(1)
				time.Sleep(20 * time.Millisecond)
				if err := mgr.Complete(ctx, key, []byte("ok")); err != nil {
					t.Errorf("Complete: %v", err)
				}
			}
		}()
	}

	close(start)
	wg.Wait()

	if winners.Load() != 1 {
		t.Fatalf("winners = %d, want 1", winners.Load())
	}
	if inFlight.Load() != 1 {
		t.Fatalf("in-flight rejects = %d, want 1", inFlight.Load())
	}

	process, cached, err := mgr.Begin(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if process {
		t.Fatal("replay should not process again")
	}
	if string(cached) != "ok" {
		t.Fatalf("cached = %q, want ok", cached)
	}
}

func TestClaim_EmptyKey(t *testing.T) {
	t.Parallel()

	store := idempotency.NewMemoryStore()
	_, err := store.Claim(context.Background(), "", time.Minute)
	if !errors.Is(err, idempotency.ErrKeyRequired) {
		t.Fatalf("Claim empty key: %v", err)
	}

	mgr := idempotency.New(idempotency.Config{})
	_, _, err = mgr.Begin(context.Background(), "")
	if !errors.Is(err, idempotency.ErrKeyRequired) {
		t.Fatalf("Begin empty key: %v", err)
	}
}

func TestClaim_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setup      func(t *testing.T, store idempotency.Store)
		wantProcess bool
		wantCached  string
		wantFlight  bool
	}{
		{
			name:        "fresh key is claimed",
			wantProcess: true,
		},
		{
			name: "completed key returns cache",
			setup: func(t *testing.T, store idempotency.Store) {
				t.Helper()
				ctx := context.Background()
				if _, err := store.Claim(ctx, "k", time.Minute); err != nil {
					t.Fatal(err)
				}
				if err := store.Complete(ctx, "k", []byte("cached"), time.Minute); err != nil {
					t.Fatal(err)
				}
			},
			wantCached: "cached",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := idempotency.NewMemoryStore()
			if tt.setup != nil {
				tt.setup(t, store)
			}

			got, err := store.Claim(context.Background(), "k", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if got.Process != tt.wantProcess {
				t.Fatalf("Process = %v, want %v", got.Process, tt.wantProcess)
			}
			if string(got.Cached) != tt.wantCached {
				t.Fatalf("Cached = %q, want %q", got.Cached, tt.wantCached)
			}
			if got.InFlight != tt.wantFlight {
				t.Fatalf("InFlight = %v, want %v", got.InFlight, tt.wantFlight)
			}
		})
	}
}

package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ultimateprogrammer/floodguard"
	"github.com/ultimateprogrammer/floodguard/lock"
	"github.com/ultimateprogrammer/floodguard/middleware"
	"github.com/ultimateprogrammer/floodguard/ratelimit"
	"github.com/ultimateprogrammer/floodguard/velocity"
	"golang.org/x/time/rate"
)

type errLimiter struct{}

func (errLimiter) Allow(context.Context, string) (bool, error) {
	return false, errors.New("redis: connection refused")
}

func TestFailClosed_StoreUnreachable(t *testing.T) {
	t.Parallel()

	g := floodguard.New(floodguard.Config{RateLimiter: errLimiter{}})
	h := middleware.Handler(g, middleware.Options{
		KeyFunc: func(r *http.Request) string { return "acct-1" },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run when store is down")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/withdraw", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestSequentialWithdrawals_ExactBalance(t *testing.T) {
	t.Parallel()

	const (
		startBalance = 1000
		withdrawAmt  = 10
		requests     = startBalance / withdrawAmt
	)

	g := floodguard.New(floodguard.Config{
		RateLimiter: ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
			Rate: rate.Inf, Burst: requests + 1,
		}),
		Velocity: velocity.Config{
			Rules: []velocity.Rule{velocity.RateOverWindow{N: requests + 1, Window: time.Minute}},
		},
		Lock: lock.Config{Client: lock.NewMemory()},
	})

	balance := startBalance
	h := middleware.Handler(g, middleware.Options{
		KeyFunc:     func(r *http.Request) string { return "acct-1" },
		RequireLock: true,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if balance < withdrawAmt {
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		balance -= withdrawAmt
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < requests; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/withdraw", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status=%d", i, rec.Code)
		}
	}
	if balance != 0 {
		t.Fatalf("final balance = %d, want 0", balance)
	}
}

func TestConcurrentLockContention(t *testing.T) {
	t.Parallel()

	g := floodguard.New(floodguard.Config{
		RateLimiter: ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{Rate: rate.Inf, Burst: 100}),
		Lock:        lock.Config{Client: lock.NewMemory()},
	})

	var peak atomic.Int32
	h := middleware.Handler(g, middleware.Options{
		KeyFunc:     func(r *http.Request) string { return "acct-1" },
		RequireLock: true,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := peak.Add(1)
		defer peak.Add(-1)
		if cur > 1 {
			t.Errorf("concurrent handlers = %d, want 1", cur)
		}
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	const workers = 50
	start := make(chan struct{})
	var wg sync.WaitGroup
	var okCount, lockedCount atomic.Int32

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/withdraw", nil))
			switch rec.Code {
			case http.StatusOK:
				okCount.Add(1)
			case http.StatusConflict:
				lockedCount.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if okCount.Load() < 1 {
		t.Fatal("expected at least one successful handler")
	}
	if lockedCount.Load() < 1 {
		t.Fatal("expected lock contention (409) for concurrent callers")
	}
}

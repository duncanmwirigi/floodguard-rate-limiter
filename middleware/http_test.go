package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ultimateprogrammer/floodguard"
	"github.com/ultimateprogrammer/floodguard/middleware"
	"github.com/ultimateprogrammer/floodguard/ratelimit"
	"golang.org/x/time/rate"
)

func TestHandler_ChainOrder(t *testing.T) {
	g := floodguard.New(floodguard.Config{
		IPRateLimiter: ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
			Rate:  rate.Limit(100),
			Burst: 100,
		}),
		RateLimiter: ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
			Rate:  rate.Limit(100),
			Burst: 100,
		}),
	})

	var handlerCalled bool
	h := middleware.Handler(g, middleware.Options{
		KeyFunc: func(r *http.Request) string { return "acct-1" },
		IPKeyFunc: func(r *http.Request) string { return "10.0.0.1" },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	req := httptest.NewRequest(http.MethodPost, "/withdraw", nil)
	req.Header.Set("Idempotency-Key", "k-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("first request: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !handlerCalled {
		t.Fatal("handler was not called")
	}

	handlerCalled = false
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("replay: status=%d", rec2.Code)
	}
	if rec2.Header().Get("X-Idempotent-Replay") != "true" {
		t.Fatal("expected idempotent replay")
	}
	if handlerCalled {
		t.Fatal("handler should not run on replay")
	}
}

func TestHandler_IPRateLimitBeforeAccount(t *testing.T) {
	g := floodguard.New(floodguard.Config{
		IPRateLimiter: ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
			Rate:  0,
			Burst: 1,
		}),
		RateLimiter: ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
			Rate:  rate.Limit(100),
			Burst: 100,
		}),
	})

	h := middleware.Handler(g, middleware.Options{
		KeyFunc:   func(r *http.Request) string { return "acct-1" },
		IPKeyFunc: func(r *http.Request) string { return "10.0.0.1" },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/withdraw", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("X-Floodguard-Layer") != "ip_rate_limit" {
		t.Fatalf("layer header = %q", rec.Header().Get("X-Floodguard-Layer"))
	}
}

func TestHandler_AccountRateLimit(t *testing.T) {
	g := floodguard.New(floodguard.Config{
		RateLimiter: ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
			Rate:  0,
			Burst: 1,
		}),
	})

	h := middleware.Handler(g, middleware.Options{
		KeyFunc: func(r *http.Request) string { return "acct-1" },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/withdraw", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("X-Floodguard-Layer") != "rate_limit" {
		t.Fatalf("layer header = %q", rec.Header().Get("X-Floodguard-Layer"))
	}
}

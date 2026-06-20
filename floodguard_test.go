package floodguard_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ultimateprogrammer/floodguard"
	"github.com/ultimateprogrammer/floodguard/middleware"
	"github.com/ultimateprogrammer/floodguard/ratelimit"
	"github.com/ultimateprogrammer/floodguard/velocity"
	"golang.org/x/time/rate"
)

func TestProtectRateLimit(t *testing.T) {
	g := floodguard.New(floodguard.Config{
		RateLimiter: ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
			Rate:  1,
			Burst: 1,
		}),
	})

	ctx := context.Background()
	req := floodguard.Request{Key: "acct-1"}

	r1, err := g.Protect(ctx, req)
	if err != nil || !r1.Allowed {
		t.Fatalf("first request should pass: %+v err=%v", r1, err)
	}

	r2, err := g.Protect(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Allowed || r2.Reason != floodguard.RejectRateLimit {
		t.Fatalf("second request should be rate limited: %+v", r2)
	}
}

func TestIdempotencyReplay(t *testing.T) {
	g := floodguard.New(floodguard.Config{})

	ctx := context.Background()
	req := floodguard.Request{Key: "acct-1", IdempotencyKey: "idem-1"}

	if _, err := g.Protect(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := g.CompleteIdempotency(ctx, "idem-1", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}

	r2, err := g.Protect(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Reason != floodguard.RejectCached || string(r2.CachedResponse) != `{"ok":true}` {
		t.Fatalf("expected cached replay, got %+v", r2)
	}
}

func TestVelocityThreshold(t *testing.T) {
	g := floodguard.New(floodguard.Config{
		Velocity: velocity.Config{
			Rules: []velocity.Rule{
				velocity.RateOverWindow{N: 2, Window: time.Minute, Label: "actions"},
			},
		},
	})

	ctx := context.Background()
	req := floodguard.Request{Key: "acct-1", Action: "bet"}

	for i := 0; i < 2; i++ {
		r, err := g.Protect(ctx, req)
		if err != nil || !r.Allowed {
			t.Fatalf("request %d should pass: %+v err=%v", i, r, err)
		}
	}

	r, err := g.Protect(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if r.Allowed || r.Reason != floodguard.RejectVelocity {
		t.Fatalf("expected velocity reject, got %+v", r)
	}
}

func TestHTTPMiddleware(t *testing.T) {
	g := floodguard.New(floodguard.Config{
		RateLimiter: ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
			Rate:  rate.Limit(100),
			Burst: 100,
		}),
	})

	handler := middleware.Handler(g, middleware.Options{
		KeyFunc: func(r *http.Request) string { return "user-1" },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))

	req := httptest.NewRequest(http.MethodPost, "/withdraw", nil)
	req.Header.Set("Idempotency-Key", "k-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected replay 200, got %d", rec2.Code)
	}
	if rec2.Header().Get("X-Idempotent-Replay") != "true" {
		t.Fatal("expected idempotent replay header")
	}
}

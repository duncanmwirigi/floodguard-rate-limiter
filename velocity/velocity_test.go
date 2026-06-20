package velocity_test

import (
	"context"
	"testing"
	"time"

	"github.com/duncanmwirigi/floodguard-rate-limiter/velocity"
)

type denyAllRule struct{}

func (denyAllRule) Check(_ context.Context, _ string, _ velocity.Store) (bool, string, error) {
	return true, "custom block", nil
}

func TestRateOverWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		n         int
		window    time.Duration
		attempts  int
		wantFlags []bool
	}{
		{
			name:      "allows up to N then flags",
			n:         3,
			window:    time.Minute,
			attempts:  4,
			wantFlags: []bool{false, false, false, true},
		},
		{
			name:      "single attempt limit",
			n:         1,
			window:    time.Minute,
			attempts:  2,
			wantFlags: []bool{false, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := velocity.NewMemoryStore()
			rule := velocity.RateOverWindow{N: tt.n, Window: tt.window, Label: "withdrawal attempts"}
			engine := velocity.New(velocity.Config{Store: store, Rules: []velocity.Rule{rule}})
			ctx := context.Background()
			key := "user-1"

			for i := 0; i < tt.attempts; i++ {
				allowed, reason, err := engine.Check(ctx, key)
				if err != nil {
					t.Fatalf("attempt %d: %v", i, err)
				}
				flagged := !allowed
				if flagged != tt.wantFlags[i] {
					t.Fatalf("attempt %d: flagged=%v want %v (reason=%q)", i, flagged, tt.wantFlags[i], reason)
				}
				if flagged && reason == "" {
					t.Fatalf("attempt %d: expected non-empty reason", i)
				}
			}
		})
	}
}

func TestRateOverWindow_WindowExpiry(t *testing.T) {
	t.Parallel()

	engine := velocity.New(velocity.Config{
		Store: velocity.NewMemoryStore(),
		Rules: []velocity.Rule{velocity.RateOverWindow{N: 1, Window: 100 * time.Millisecond, Label: "attempts"}},
	})
	ctx := context.Background()
	key := "user-1"

	if allowed, _, err := engine.Check(ctx, key); err != nil || !allowed {
		t.Fatalf("first check should pass: allowed=%v err=%v", allowed, err)
	}
	if allowed, _, err := engine.Check(ctx, key); err != nil || allowed {
		t.Fatalf("second check should flag: allowed=%v err=%v", allowed, err)
	}

	time.Sleep(150 * time.Millisecond)

	if allowed, _, err := engine.Check(ctx, key); err != nil || !allowed {
		t.Fatalf("after window expiry should pass: allowed=%v err=%v", allowed, err)
	}
}

func TestMinInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		min            time.Duration
		gap            time.Duration
		runSecondCheck bool
		wantFlag       bool
	}{
		{
			name:     "first action is always allowed",
			min:      200 * time.Millisecond,
			wantFlag: false,
		},
		{
			name:           "second action too fast is flagged",
			min:            200 * time.Millisecond,
			gap:            50 * time.Millisecond,
			runSecondCheck: true,
			wantFlag:       true,
		},
		{
			name:           "second action after min interval is allowed",
			min:            100 * time.Millisecond,
			gap:            150 * time.Millisecond,
			runSecondCheck: true,
			wantFlag:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			engine := velocity.New(velocity.Config{
				Store: velocity.NewMemoryStore(),
				Rules: []velocity.Rule{velocity.MinInterval{Min: tt.min, Label: "bet"}},
			})
			ctx := context.Background()
			key := "user-bet"

			if allowed, _, err := engine.Check(ctx, key); err != nil || !allowed {
				t.Fatalf("first check: allowed=%v err=%v", allowed, err)
			}

			if !tt.runSecondCheck {
				return
			}

			time.Sleep(tt.gap)

			allowed, reason, err := engine.Check(ctx, key)
			if err != nil {
				t.Fatal(err)
			}
			if !allowed != tt.wantFlag {
				t.Fatalf("second check: flagged=%v want %v reason=%q", !allowed, tt.wantFlag, reason)
			}
			if tt.wantFlag && reason == "" {
				t.Fatal("expected reason when flagged")
			}
		})
	}
}

func TestEngine_CustomRule(t *testing.T) {
	t.Parallel()

	engine := velocity.New(velocity.Config{Store: velocity.NewMemoryStore()})
	engine.AddRule(denyAllRule{})

	allowed, reason, err := engine.Check(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if allowed || reason != "custom block" {
		t.Fatalf("allowed=%v reason=%q", allowed, reason)
	}
}

func TestEngine_CombinedRules(t *testing.T) {
	t.Parallel()

	engine := velocity.New(velocity.Config{
		Store: velocity.NewMemoryStore(),
		Rules: []velocity.Rule{
			velocity.MinInterval{Min: 50 * time.Millisecond, Label: "bet"},
			velocity.RateOverWindow{N: 2, Window: time.Minute, Label: "bets"},
		},
	})
	ctx := context.Background()
	key := "user-1"

	for i := 0; i < 2; i++ {
		allowed, _, err := engine.Check(ctx, key)
		if err != nil || !allowed {
			t.Fatalf("check %d should pass", i)
		}
		time.Sleep(60 * time.Millisecond)
	}

	allowed, reason, err := engine.Check(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatalf("third check should be rate flagged, reason=%q", reason)
	}
}

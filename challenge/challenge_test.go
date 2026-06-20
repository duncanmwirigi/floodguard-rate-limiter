package challenge_test

import (
	"context"
	"testing"
	"time"

	"github.com/duncanmwirigi/floodguard-rate-limiter/challenge"
)

func TestChallengeRequired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mature := challenge.RiskSignals{AccountAge: 30 * 24 * time.Hour}

	if challenge.ChallengeRequired(ctx, mature) {
		t.Fatal("clean signals should not require challenge")
	}
	if !challenge.ChallengeRequired(ctx, challenge.RiskSignals{PlatformSpike: true}) {
		t.Fatal("platform spike should require challenge")
	}
	if !challenge.ChallengeRequired(ctx, challenge.RiskSignals{AccountAge: time.Hour}) {
		t.Fatal("account age 1h should require challenge")
	}
	if challenge.ChallengeRequired(ctx, challenge.RiskSignals{AccountAge: 30 * 24 * time.Hour}) {
		t.Fatal("mature account should not require challenge")
	}
	if !challenge.ChallengeRequired(ctx, challenge.RiskSignals{AccountAge: 0}) {
		t.Fatal("unknown account (age 0) should require challenge")
	}
}

func TestManager_SolveCaptcha(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := challenge.NewManager(challenge.StubVerifier{}, challenge.NewMemoryTokenStore())

	signals := challenge.RiskSignals{VelocityFlagged: true}
	token, err := mgr.Check(ctx, "a1", "withdraw", signals, "", "")
	if err != challenge.ErrChallengeRequired || token == "" {
		t.Fatalf("token=%q err=%v", token, err)
	}

	_, err = mgr.Check(ctx, "a1", "withdraw", signals, token, "valid-captcha")
	if err != nil {
		t.Fatalf("expected success after captcha: %v", err)
	}
}

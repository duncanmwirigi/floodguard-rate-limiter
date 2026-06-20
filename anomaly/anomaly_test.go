package anomaly_test

import (
	"context"
	"testing"
	"time"

	"github.com/duncanmwirigi/floodguard-rate-limiter/anomaly"
)

func TestDetectSpike_Sudden10x(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	counter := anomaly.NewMemoryCounter()
	det := anomaly.New(anomaly.Config{Counter: counter, Multiplier: 5.0})

	now := time.Now().UTC().Truncate(time.Minute)
	// Baseline: 1 event per minute for 60 minutes.
	for i := 0; i < 60; i++ {
		at := now.Add(-time.Duration(60-i) * time.Minute)
		if err := counter.Increment(ctx, anomaly.MetricRegistrations, at, 1); err != nil {
			t.Fatal(err)
		}
	}
	// Current minute: 10 events (10x baseline of ~1/min).
	if err := counter.Increment(ctx, anomaly.MetricRegistrations, now, 10); err != nil {
		t.Fatal(err)
	}

	spike, current, baseline, err := det.DetectSpike(ctx, anomaly.MetricRegistrations, 60)
	if err != nil {
		t.Fatal(err)
	}
	if !spike {
		t.Fatalf("expected spike current=%f baseline=%f", current, baseline)
	}
}

func TestDetectSpike_GradualIncreaseNoSpike(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	counter := anomaly.NewMemoryCounter()
	det := anomaly.New(anomaly.Config{Counter: counter, Multiplier: 5.0})

	now := time.Now().UTC().Truncate(time.Minute)
	// Gradual ramp 1..5 over 60 minutes, current minute = 5.
	for i := 0; i < 60; i++ {
		at := now.Add(-time.Duration(60-i) * time.Minute)
		delta := int64(1 + i/15) // slowly increases
		if err := counter.Increment(ctx, anomaly.MetricWithdrawalAttempts, at, delta); err != nil {
			t.Fatal(err)
		}
	}
	if err := counter.Increment(ctx, anomaly.MetricWithdrawalAttempts, now, 5); err != nil {
		t.Fatal(err)
	}

	spike, current, baseline, err := det.DetectSpike(ctx, anomaly.MetricWithdrawalAttempts, 60)
	if err != nil {
		t.Fatal(err)
	}
	if spike {
		t.Fatalf("gradual increase should not spike current=%f baseline=%f", current, baseline)
	}
}

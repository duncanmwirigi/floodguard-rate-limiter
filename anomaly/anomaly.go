// Package anomaly detects platform-wide metric spikes that per-IP/account
// limits cannot see (e.g. distributed botnets with low per-node rates).
package anomaly

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrKeyRequired is returned when metric name is empty.
var ErrKeyRequired = errors.New("anomaly: metric name is required")

// Metric names for platform-wide counters.
const (
	MetricRegistrations      = "registrations"
	MetricWithdrawalAttempts = "withdrawal_attempts"
)

// Counter records platform-wide events in time buckets.
type Counter interface {
	Increment(ctx context.Context, metric string, at time.Time, delta int64) error
	CountInWindow(ctx context.Context, metric string, from, to time.Time) (int64, error)
}

// Detector compares current rate to a trailing baseline and flags spikes.
type Detector struct {
	counter    Counter
	multiplier float64 // current must exceed baseline * multiplier
}

// Config configures spike detection.
type Config struct {
	Counter    Counter
	Multiplier float64 // default 5.0
}

// New creates a Detector.
func New(cfg Config) *Detector {
	if cfg.Counter == nil {
		cfg.Counter = NewMemoryCounter()
	}
	if cfg.Multiplier <= 0 {
		cfg.Multiplier = 5.0
	}
	return &Detector{counter: cfg.Counter, multiplier: cfg.Multiplier}
}

// Record increments a platform-wide metric for the current minute.
func (d *Detector) Record(ctx context.Context, metric string, delta int64) error {
	if metric == "" {
		return ErrKeyRequired
	}
	return d.counter.Increment(ctx, metric, time.Now().UTC(), delta)
}

// DetectSpike compares the rate in the last minute to the average per-minute
// rate over lookbackMinutes (excluding the current minute).
func (d *Detector) DetectSpike(ctx context.Context, metric string, lookbackMinutes int) (spike bool, currentRate, baselineRate float64, err error) {
	if metric == "" {
		return false, 0, 0, ErrKeyRequired
	}
	if lookbackMinutes < 1 {
		lookbackMinutes = 60
	}

	now := time.Now().UTC().Truncate(time.Minute)
	currentEnd := now.Add(time.Minute)
	currentCount, err := d.counter.CountInWindow(ctx, metric, now, currentEnd)
	if err != nil {
		return false, 0, 0, err
	}
	currentRate = float64(currentCount)

	from := now.Add(-time.Duration(lookbackMinutes) * time.Minute)
	baselineCount, err := d.counter.CountInWindow(ctx, metric, from, now)
	if err != nil {
		return false, 0, 0, err
	}
	baselineRate = float64(baselineCount) / float64(lookbackMinutes)

	if baselineRate <= 0 {
		// No history — only spike if current is unusually high in absolute terms.
		return currentRate >= d.multiplier, currentRate, baselineRate, nil
	}

	spike = currentRate >= baselineRate*d.multiplier
	return spike, currentRate, baselineRate, nil
}

// AlertHook is called when a spike is detected (wire to paging/Slack).
type AlertHook func(ctx context.Context, metric string, current, baseline float64)

// CheckAndAlert runs DetectSpike and invokes hook when true.
func (d *Detector) CheckAndAlert(ctx context.Context, metric string, lookback int, hook AlertHook) error {
	spike, current, baseline, err := d.DetectSpike(ctx, metric, lookback)
	if err != nil {
		return err
	}
	if spike && hook != nil {
		hook(ctx, metric, current, baseline)
	}
	return nil
}

func minuteKey(metric string, t time.Time) string {
	return fmt.Sprintf("%s:%d", metric, t.Unix()/60)
}

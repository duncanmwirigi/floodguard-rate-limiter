package velocity

import (
	"context"
	"fmt"
	"time"
)

// RateOverWindow flags when a key already has N or more actions within Window,
// meaning the current attempt would exceed the allowed rate.
type RateOverWindow struct {
	// N is the maximum allowed actions within Window before flagging.
	N int
	// Window is the rolling time window.
	Window time.Duration
	// Label appears in the reason string (e.g. "withdrawal attempts").
	Label string
}

// Check implements [Rule].
func (r RateOverWindow) Check(ctx context.Context, key string, store Store) (bool, string, error) {
	if r.N <= 0 {
		return false, "", fmt.Errorf("%w: RateOverWindow.N must be positive", ErrInvalidRule)
	}
	if r.Window <= 0 {
		return false, "", fmt.Errorf("%w: RateOverWindow.Window must be positive", ErrInvalidRule)
	}

	count, err := store.Count(ctx, key, r.Window)
	if err != nil {
		return false, "", err
	}
	if count >= r.N {
		label := r.Label
		if label == "" {
			label = "actions"
		}
		return true, fmt.Sprintf("more than %d %s within %s", r.N, label, r.Window), nil
	}
	return false, "", nil
}

// MinInterval flags when the previous action for key was less than Min ago,
// indicating superhuman speed (e.g. automated betting).
type MinInterval struct {
	// Min is the minimum elapsed time required between consecutive actions.
	Min time.Duration
	// Label appears in the reason string (e.g. "bet").
	Label string
}

// Check implements [Rule].
func (m MinInterval) Check(ctx context.Context, key string, store Store) (bool, string, error) {
	if m.Min <= 0 {
		return false, "", fmt.Errorf("%w: MinInterval.Min must be positive", ErrInvalidRule)
	}

	last, found, err := store.Last(ctx, key)
	if err != nil {
		return false, "", err
	}
	if !found {
		return false, "", nil
	}

	elapsed := time.Since(last)
	if elapsed < m.Min {
		label := m.Label
		if label == "" {
			label = "action"
		}
		return true, fmt.Sprintf("%s placed faster than minimum human interval %s (elapsed %s)",
			label, m.Min, elapsed.Truncate(time.Millisecond)), nil
	}
	return false, "", nil
}

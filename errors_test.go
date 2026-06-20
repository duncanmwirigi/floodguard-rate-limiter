package floodguard_test

import (
	"errors"
	"testing"

	"github.com/ultimateprogrammer/floodguard"
	"github.com/ultimateprogrammer/floodguard/idempotency"
)

func TestIsDuplicateInFlight(t *testing.T) {
	t.Parallel()

	wrapped := errors.Join(floodguard.ErrDuplicateInFlight, idempotency.ErrInFlight)
	if !floodguard.IsDuplicateInFlight(wrapped) {
		t.Fatal("expected duplicate in-flight")
	}
	if !errors.Is(wrapped, floodguard.ErrDuplicateInFlight) {
		t.Fatal("expected ErrDuplicateInFlight")
	}
	if !errors.Is(wrapped, idempotency.ErrInFlight) {
		t.Fatal("expected idempotency.ErrInFlight")
	}
}

func TestRejectedErrorIsAs(t *testing.T) {
	t.Parallel()

	err := floodguard.RejectedError{Reason: floodguard.RejectRateLimit}
	if !floodguard.IsRejected(err, floodguard.RejectRateLimit) {
		t.Fatal("expected rate limit rejection")
	}
	if floodguard.IsRejected(err, floodguard.RejectVelocity) {
		t.Fatal("unexpected velocity rejection")
	}

	var target floodguard.RejectedError
	if !errors.As(err, &target) {
		t.Fatal("expected errors.As to succeed")
	}
	if target.Reason != floodguard.RejectRateLimit {
		t.Fatalf("reason = %q", target.Reason)
	}
}

package floodguard

import (
	"errors"
	"fmt"

	"github.com/ultimateprogrammer/floodguard/idempotency"
)

// ErrKeyRequired is returned when a request is missing the account or resource key.
var ErrKeyRequired = errors.New("floodguard: key is required")

// ErrDuplicateInFlight indicates another request with the same idempotency key is active.
var ErrDuplicateInFlight = errors.New("floodguard: duplicate request in flight")

// duplicateInFlightError wraps ErrDuplicateInFlight and, when applicable, idempotency.ErrInFlight
// so callers can use errors.Is against either sentinel.
func duplicateInFlightError(cause error) error {
	if cause == nil {
		return ErrDuplicateInFlight
	}
	return fmt.Errorf("%w: %w", ErrDuplicateInFlight, cause)
}

// IsDuplicateInFlight reports whether err represents an in-flight idempotency conflict.
func IsDuplicateInFlight(err error) bool {
	return errors.Is(err, ErrDuplicateInFlight) || errors.Is(err, idempotency.ErrInFlight)
}

// IsRejected reports whether err is a RejectedError and, when reason is non-empty,
// whether it matches that reason.
func IsRejected(err error, reason RejectReason) bool {
	var rejected RejectedError
	if !errors.As(err, &rejected) {
		return false
	}
	if reason == "" {
		return true
	}
	return rejected.Reason == reason
}

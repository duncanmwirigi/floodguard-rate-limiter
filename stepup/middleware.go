package stepup

import (
	"encoding/json"
	"log"
	"net/http"
)

// Options configures step-up middleware.
type Options struct {
	AccountKey func(r *http.Request) string
	DeviceFP   func(r *http.Request) string
	Action     string
	// KnownDevice returns whether the device is trusted for the account.
	KnownDevice func(r *http.Request, accountID string) (bool, error)
	// VelocityFlagged returns whether floodguard velocity flagged this action.
	VelocityFlagged func(r *http.Request, accountID string) bool
	Logger          *log.Logger
}

// Middleware wraps sensitive handlers with step-up auth when risk is Medium or High.
func Middleware(mgr *Manager, opts Options) func(http.Handler) http.Handler {
	if mgr == nil {
		panic("stepup: manager is required")
	}
	if opts.AccountKey == nil {
		opts.AccountKey = func(r *http.Request) string { return r.Header.Get("X-Account-ID") }
	}
	if opts.DeviceFP == nil {
		opts.DeviceFP = func(r *http.Request) string { return r.Header.Get("X-Device-FP") }
	}
	if opts.KnownDevice == nil {
		opts.KnownDevice = func(r *http.Request, accountID string) (bool, error) { return false, nil }
	}
	if opts.VelocityFlagged == nil {
		opts.VelocityFlagged = func(r *http.Request, accountID string) bool { return false }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			accountID := opts.AccountKey(r)
			known, err := opts.KnownDevice(r, accountID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "device trust check failed")
				return
			}

			signals := Signals{
				AccountID:       accountID,
				DeviceFP:        opts.DeviceFP(r),
				Action:          opts.Action,
				KnownDevice:     known,
				VelocityFlagged: opts.VelocityFlagged(r, accountID),
			}

			token, level, err := mgr.Check(r.Context(), signals, HeaderToken(r))
			if err == nil {
				next.ServeHTTP(w, r)
				return
			}
			if err != ErrStepUpRequired {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Step-Up-Required", "true")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":           "step-up verification required",
				"risk_level":      level.String(),
				"challenge_token": token,
			})
		})
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

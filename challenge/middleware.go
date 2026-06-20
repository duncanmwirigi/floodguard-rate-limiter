package challenge

import (
	"encoding/json"
	"net/http"
)

// Options configures challenge middleware.
type Options struct {
	AccountKey      func(r *http.Request) string
	Action          string
	Signals         func(r *http.Request, accountID string) RiskSignals
}

// Middleware wraps handlers with conditional CAPTCHA when risk signals fire.
func Middleware(mgr *Manager, opts Options) func(http.Handler) http.Handler {
	if mgr == nil {
		panic("challenge: manager is required")
	}
	if opts.AccountKey == nil {
		opts.AccountKey = func(r *http.Request) string { return r.Header.Get("X-Account-ID") }
	}
	if opts.Signals == nil {
		opts.Signals = func(r *http.Request, accountID string) RiskSignals { return RiskSignals{} }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			accountID := opts.AccountKey(r)
			signals := opts.Signals(r, accountID)

			token, err := mgr.Check(r.Context(), accountID, opts.Action, signals, HeaderToken(r), CaptchaResponse(r))
			if err == nil {
				next.ServeHTTP(w, r)
				return
			}
			if err != ErrChallengeRequired {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Challenge-Required", "true")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":           "captcha required",
				"challenge_token": token,
			})
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

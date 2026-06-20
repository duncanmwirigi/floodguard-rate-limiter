// Package middleware provides HTTP middleware that chains floodguard protections:
//
//  1. IP rate limit
//  2. Account rate limit
//  3. Idempotency check
//  4. Velocity rules
//  5. Distributed lock (optional)
//  6. Handler → release lock → cache idempotent result
package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/duncanmwirigi/floodguard-rate-limiter"
	"github.com/duncanmwirigi/floodguard-rate-limiter/idempotency"
	"github.com/duncanmwirigi/floodguard-rate-limiter/lock"
)

// Options configures HTTP middleware behavior.
type Options struct {
	// IPKeyFunc derives the client IP for layer-1 rate limiting.
	IPKeyFunc func(r *http.Request) string
	// KeyFunc derives the account or wallet key for layers 2, 4, and 5.
	KeyFunc func(r *http.Request) string
	// LockKeyFunc derives the lock key when RequireLock is true.
	// Defaults to KeyFunc when nil (typically the wallet / account ID).
	LockKeyFunc func(r *http.Request) string
	// IdempotencyKeyFunc reads the idempotency key header (default: Idempotency-Key).
	IdempotencyKeyFunc func(r *http.Request) string
	// Action labels the request for velocity tracking (e.g. "withdraw", "bet").
	Action string
	// RequireLock acquires a distributed lock before the handler runs (layer 5).
	RequireLock bool
	// RequireIdempotencyKey rejects requests missing Idempotency-Key (recommended for withdrawals/bets).
	RequireIdempotencyKey bool
	// FailClosed denies the request when a shared store is unreachable.
	// Defaults to true (fail closed). Set explicitly to false only for non-critical reads.
	FailClosed *bool
	// BlockOnVelocity rejects the request when a velocity rule fires.
	// Defaults to true. Set to false to flag-only via OnVelocityFlag.
	BlockOnVelocity *bool
	// Logger receives human-readable traces of each protection layer (optional).
	Logger *log.Logger
	// Audit emits structured events for rejections, flags, and replays (optional).
	Audit func(e AuditEvent)
	// OnVelocityFlag is invoked when BlockOnVelocity is false and a rule fires.
	OnVelocityFlag func(w http.ResponseWriter, r *http.Request, reason string)
	// OnBlocked is called before writing the default error response.
	OnBlocked func(w http.ResponseWriter, r *http.Request, reason floodguard.RejectReason)
}

// AuditEvent is a structured observability record for a protection-layer decision.
type AuditEvent struct {
	Layer     string
	Decision  string // "allow", "reject", "replay", "flag"
	Reason    string
	AccountID string
	IP        string
	IdemKey   string
	Action    string
}

// Handler wraps an http.Handler with the full floodguard pipeline:
//
//	incoming request
//	       │
//	       ▼
//	1. IP rate limit        → reject if IP alone is hammering you
//	       │
//	       ▼
//	2. Account rate limit   → reject if account is over limit (catches IP rotation)
//	       │
//	       ▼
//	3. Idempotency check    → return cached result for duplicate keys
//	       │
//	       ▼
//	4. Velocity rules       → flag anomalous patterns (e.g. 3rd withdrawal in 60s)
//	       │
//	       ▼
//	5. Distributed lock     → serialize concurrent access to the wallet
//	       │
//	       ▼
//	handler (balance check + deduct, inside the lock)
//	       │
//	       ▼
//	6. Release lock, cache result under the idempotency key
func Handler(fg *floodguard.Guard, opts Options) func(http.Handler) http.Handler {
	if fg == nil {
		panic("middleware: guard is required")
	}
	opts = opts.withDefaults()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			ipKey := opts.IPKeyFunc(r)
			accountKey := opts.KeyFunc(r)
			lockKey := opts.lockKey(r)
			idemKey := opts.IdempotencyKeyFunc(r)

			if opts.RequireIdempotencyKey && idemKey == "" {
				opts.audit("idempotency", "reject", "missing idempotency key", accountKey, ipKey, idemKey)
				writeError(w, http.StatusBadRequest, "Idempotency-Key required", "")
				return
			}

			// --- Layer 1: IP rate limit ---------------------------------------------
			if ipRL := fg.IPRateLimiter(); ipRL != nil && ipKey != "" {
				allowed, err := ipRL.Allow(ctx, ipKey)
				if err != nil {
					opts.trace("ip rate limit: internal error: %v", err)
					opts.audit("ip_rate_limit", "reject", err.Error(), accountKey, ipKey, idemKey)
					writeStoreError(w, opts.failClosed())
					return
				}
				if !allowed {
					opts.trace("ip rate limit: REJECTED ip=%q", ipKey)
					opts.audit("ip_rate_limit", "reject", "limit exceeded", accountKey, ipKey, idemKey)
					opts.block(w, r, floodguard.RejectIPRateLimit)
					writeReject(w, floodguard.RejectIPRateLimit, "")
					return
				}
				opts.trace("ip rate limit: OK ip=%q", ipKey)
			}

			// --- Layer 2: account rate limit ----------------------------------------
			allowed, err := fg.RateLimiter().Allow(ctx, accountKey)
			if err != nil {
				opts.trace("account rate limit: internal error: %v", err)
				opts.audit("rate_limit", "reject", err.Error(), accountKey, ipKey, idemKey)
				writeStoreError(w, opts.failClosed())
				return
			}
			if !allowed {
				opts.trace("account rate limit: REJECTED account=%q", accountKey)
				opts.audit("rate_limit", "reject", "limit exceeded", accountKey, ipKey, idemKey)
				opts.block(w, r, floodguard.RejectRateLimit)
				writeReject(w, floodguard.RejectRateLimit, "")
				return
			}
			opts.trace("account rate limit: OK account=%q", accountKey)

			// --- Layer 3: idempotency -----------------------------------------------
			if idemKey != "" {
				process, cached, err := fg.Idempotency().Begin(ctx, idemKey)
				if err != nil {
					if errors.Is(err, idempotency.ErrInFlight) || floodguard.IsDuplicateInFlight(err) {
						opts.trace("idempotency: REJECTED in-flight key=%q", idemKey)
						writeError(w, http.StatusConflict, "request already in progress", "")
						return
					}
					opts.trace("idempotency: internal error: %v", err)
					opts.audit("idempotency", "reject", err.Error(), accountKey, ipKey, idemKey)
					writeStoreError(w, opts.failClosed())
					return
				}
				if len(cached) > 0 {
					opts.trace("idempotency: REPLAY key=%q", idemKey)
					opts.audit("idempotency", "replay", "cached response", accountKey, ipKey, idemKey)
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("X-Floodguard-Layer", "idempotency-replay")
					w.Header().Set("X-Idempotent-Replay", "true")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(cached)
					return
				}
				if !process {
					opts.trace("idempotency: REJECTED duplicate key=%q", idemKey)
					writeError(w, http.StatusConflict, "request already in progress", "")
					return
				}
				opts.trace("idempotency: OK claimed key=%q", idemKey)
			}

			// --- Layer 4: velocity --------------------------------------------------
			velKey := floodguard.VelocityKey(accountKey, opts.Action)
			velOK, velReason, err := fg.Velocity().Check(ctx, velKey)
			if err != nil {
				opts.trace("velocity: internal error: %v", err)
				opts.audit("velocity", "reject", err.Error(), accountKey, ipKey, idemKey)
				writeStoreError(w, opts.failClosed())
				return
			}
			if !velOK {
				opts.trace("velocity: REJECTED key=%q reason=%q", velKey, velReason)
				opts.audit("velocity", "flag", velReason, accountKey, ipKey, idemKey)
				if !opts.blockOnVelocity() {
					if opts.OnVelocityFlag != nil {
						opts.OnVelocityFlag(w, r, velReason)
					}
				} else {
					opts.block(w, r, floodguard.RejectVelocity)
					writeReject(w, floodguard.RejectVelocity, velReason)
					return
				}
			}
			opts.trace("velocity: OK key=%q", velKey)

			// --- Layers 5–6: lock → handler → release lock → cache result -----------
			runHandler := func() {
				opts.trace("handler: START account=%q", accountKey)
				if idemKey == "" {
					next.ServeHTTP(w, r)
					opts.trace("handler: DONE account=%q", accountKey)
					return
				}

				rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
				next.ServeHTTP(rec, r)

				if rec.status >= 200 && rec.status < 300 && len(rec.body) > 0 {
					if err := fg.CompleteIdempotency(ctx, idemKey, rec.body); err != nil {
						opts.trace("idempotency: complete error key=%q: %v", idemKey, err)
					} else {
						opts.trace("idempotency: STORED response key=%q", idemKey)
					}
				}
				opts.trace("handler: DONE account=%q status=%d", accountKey, rec.status)
			}

			if !opts.RequireLock {
				runHandler()
				return
			}

			opts.trace("lock: ACQUIRE key=%q", lockKey)
			release, err := fg.TryLock(ctx, lockKey)
			if err != nil {
				if errors.Is(err, lock.ErrNotAcquired) {
					opts.trace("lock: REJECTED key=%q (already held)", lockKey)
					opts.block(w, r, floodguard.RejectLocked)
					writeReject(w, floodguard.RejectLocked, "")
					return
				}
				opts.trace("lock: internal error: %v", err)
				opts.audit("lock", "reject", err.Error(), accountKey, ipKey, idemKey)
				writeStoreError(w, opts.failClosed())
				return
			}
			defer func() {
				_ = release()
				opts.trace("lock: RELEASE key=%q", lockKey)
			}()

			runHandler()
		})
	}
}

func (o Options) lockKey(r *http.Request) string {
	if o.LockKeyFunc != nil {
		return o.LockKeyFunc(r)
	}
	return o.KeyFunc(r)
}

func (o Options) withDefaults() Options {
	if o.IPKeyFunc == nil {
		o.IPKeyFunc = ClientIP
	}
	if o.KeyFunc == nil {
		o.KeyFunc = func(r *http.Request) string {
			if v := r.Header.Get("X-Account-ID"); v != "" {
				return v
			}
			return ClientIP(r)
		}
	}
	if o.IdempotencyKeyFunc == nil {
		o.IdempotencyKeyFunc = func(r *http.Request) string {
			return strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		}
	}
	if o.Logger == nil {
		o.Logger = log.Default()
	}
	return o
}

func (o Options) failClosed() bool {
	if o.FailClosed == nil {
		return true
	}
	return *o.FailClosed
}

func (o Options) blockOnVelocity() bool {
	if o.BlockOnVelocity == nil {
		return true
	}
	return *o.BlockOnVelocity
}

func (o Options) audit(layer, decision, reason, account, ip, idem string) {
	if o.Audit == nil {
		return
	}
	o.Audit(AuditEvent{
		Layer:     layer,
		Decision:  decision,
		Reason:    reason,
		AccountID: account,
		IP:        ip,
		IdemKey:   idem,
		Action:    o.Action,
	})
}

func writeStoreError(w http.ResponseWriter, failClosed bool) {
	if failClosed {
		writeError(w, http.StatusServiceUnavailable, "protection layer unavailable", "shared store unreachable; request denied")
		return
	}
	writeError(w, http.StatusInternalServerError, "protection check failed", "")
}

// ClientIP returns the client IP from X-Forwarded-For or RemoteAddr.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func (o Options) trace(format string, args ...any) {
	if o.Logger != nil {
		o.Logger.Printf("[floodguard] "+format, args...)
	}
}

func (o Options) block(w http.ResponseWriter, r *http.Request, reason floodguard.RejectReason) {
	if o.OnBlocked != nil {
		o.OnBlocked(w, r, reason)
	}
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	body   []byte
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return r.ResponseWriter.Write(b)
}

func writeReject(w http.ResponseWriter, reason floodguard.RejectReason, detail string) {
	w.Header().Set("X-Floodguard-Layer", string(reason))
	switch reason {
	case floodguard.RejectIPRateLimit:
		writeError(w, http.StatusTooManyRequests, "ip rate limit exceeded", detail)
	case floodguard.RejectRateLimit:
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded", detail)
	case floodguard.RejectVelocity:
		writeError(w, http.StatusTooManyRequests, "velocity threshold exceeded", detail)
	case floodguard.RejectLocked:
		writeError(w, http.StatusConflict, "resource is locked", detail)
	default:
		writeError(w, http.StatusForbidden, "request blocked", detail)
	}
}

func writeError(w http.ResponseWriter, status int, message, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]string{"error": message}
	if detail != "" {
		body["detail"] = detail
	}
	_ = json.NewEncoder(w).Encode(body)
}

// ProtectFunc runs [floodguard.Guard.Protect] programmatically and returns a callback
// to complete idempotency after the handler succeeds.
func ProtectFunc(fg *floodguard.Guard, key, idempotencyKey string) (context.Context, func([]byte) error, error) {
	result, err := fg.Protect(context.Background(), floodguard.Request{
		Key:            key,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return nil, nil, err
	}
	if !result.Allowed {
		return nil, nil, floodguard.RejectedError{Reason: result.Reason}
	}
	if len(result.CachedResponse) > 0 {
		return nil, nil, floodguard.RejectedError{Reason: floodguard.RejectCached, Cached: result.CachedResponse}
	}

	complete := func(body []byte) error {
		if idempotencyKey == "" {
			return nil
		}
		return fg.CompleteIdempotency(context.Background(), idempotencyKey, body)
	}
	return context.Background(), complete, nil
}

// DrainBody reads and restores r.Body so idempotent handlers can re-read it.
func DrainBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

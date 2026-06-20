# Production Readiness Checklist

Anti-abuse / rate-limiting audit for betting and withdrawal systems.

Use this before going live. It covers the **platform as a whole** — floodguard is one layer, not a complete security solution. Status codes:

| Symbol | Meaning |
|--------|---------|
| ✅ | Supported by floodguard today |
| ⚠️ | Partially supported — hook exists, platform must wire it |
| ❌ | Gap — not in floodguard; platform must implement |
| 🔲 | Outside floodguard — application / infra responsibility |

See [Honest limits](#what-this-checklist-deliberately-does-not-claim) at the bottom.

---

## 1. Request-Level Controls

| Item | Status | floodguard / platform notes |
|------|--------|----------------------------|
| Rate limiting per IP | ✅ | `Config.IPRateLimiter` + `middleware.Options.IPKeyFunc` (layer 1) |
| Rate limiting per account/user ID | ✅ | `Config.RateLimiter` + `Options.KeyFunc` (layer 2) |
| Rate limiting per session/device fingerprint | ⚠️ | Provide a custom `KeyFunc` / `IPKeyFunc` that reads a device header or fingerprint cookie |
| Rate limiting per endpoint | ⚠️ | Mount separate `middleware.Handler` instances with different limits per route (`/withdraw` vs `/balance`) |
| Limits configurable per environment without code changes | ⚠️ | Load limit values from env/config at startup; floodguard accepts `Limiter` interfaces — wire from YAML/env in your app |
| Shared store (Redis), not in-memory | ✅ | `ratelimit.NewRedisSlidingWindow`, Redis stores for idempotency/lock/velocity — **required for multi-instance prod** |

**Before go-live:** Confirm prod uses Redis backends. In-memory limiters silently break when you scale past one pod.

---

## 2. Idempotency

| Item | Status | floodguard / platform notes |
|------|--------|----------------------------|
| State-changing endpoints require idempotency key | ⚠️ | Set `middleware.Options.RequireIdempotencyKey: true` on withdrawal/bet routes |
| Check-and-reserve is atomic | ✅ | Redis Lua + SETNX; in-memory uses mutex |
| Duplicates return cached response | ✅ | Layer 3 replay with `X-Idempotent-Replay: true` |
| Sane TTL | ✅ | `idempotency.Config.TTL` (default 24h) |
| Concurrency: two simultaneous identical requests → one processed | ✅ | `idempotency/*_test.go` — run `go test -race -count=20 ./idempotency/...` |

---

## 3. Race Condition / Concurrency Safety

| Item | Status | floodguard / platform notes |
|------|--------|----------------------------|
| Balance ops wrapped in distributed lock | ✅ | `Options.RequireLock: true` (layer 5) |
| Lock release uses token-check (Lua) | ✅ | `lock/redis.go` compare-and-del script |
| Lock TTL covers operation, recovers on crash | ✅ | `lock.Config.TTL` (default 30s) — tune per handler latency |
| Load test: 50+ concurrent requests, exact final balance | ⚠️ | See `middleware/concurrency_test.go` — **extend with your real DB + `-count=20`** |
| No bypass path for "internal" calls | 🔲 | Code review: every money path must go through the same middleware or `Guard.WithLock` |

---

## 4. Behavioral / Anomaly Detection

| Item | Status | floodguard / platform notes |
|------|--------|----------------------------|
| Velocity: many withdrawals in short window | ✅ | `velocity.RateOverWindow` |
| Velocity: bets faster than human reaction | ✅ | `velocity.MinInterval` |
| Velocity: bet size / pattern anomalies | ❌ | Custom `velocity.Rule` — needs account history from your DB |
| Flagged events logged and queryable | ⚠️ | Use `Options.OnVelocityFlag` / `Options.Audit` callbacks → ship to your SIEM |
| Flagging triggers escalation (2FA, hold, review) | 🔲 | Implement in `OnVelocityFlag` / `OnBlocked` — call risk service, not silent block |
| Thresholds tunable without deploy | ❌ | Hot-reload rules from config service — floodguard accepts `Engine.AddRule` at runtime if you wire it |

**Tip:** Use `BlockOnVelocity: false` to **flag-only** on reads/analytics paths; keep `true` (default) on withdrawals.

---

## 5. Fail-Safe Behavior

| Item | Status | floodguard / platform notes |
|------|--------|----------------------------|
| Redis unreachable → fail closed on money endpoints | ✅ | Store errors return 503/500 — request never reaches handler (`FailClosed` default true) |
| Explicit test: store down → all checks deny | ✅ | `middleware/failclosed_test.go` |
| Circuit breaker on payment/banking APIs | 🔲 | Use `gobreaker` / similar in application layer |
| Graceful degradation documented (reads vs writes) | 🔲 | Document: balance read may use cache; withdrawal must fail closed |

---

## 6. Authentication & Session Security

| Item | Status | Notes |
|------|--------|-------|
| Session/token expiry and rotation | 🔲 | Auth service |
| Device binding / login anomaly detection | ✅ | `devicetrust` — fingerprint + `IsKnownDevice` / `MarkDeviceTrusted` |
| Step-up auth for withdrawals / after velocity flag | ✅ | `stepup` — composes known device + velocity; set `BlockOnVelocity: false` + `OnVelocityFlag` to feed signals |
| Account lockout on failed auth | 🔲 | Auth service |
| Notify owner on sensitive action from new device | ✅ | `notify.AfterSensitiveAction` (async, logs failures) |

Rate limiting a stolen valid session does nothing — device trust + step-up raise the cost of credential abuse. Production OTP/SMS delivery needs its own infra review.

---

## 7. Business Logic Integrity

| Item | Status | Notes |
|------|--------|-------|
| Check + deduct inside same locked operation | ⚠️ | floodguard serializes; use `wallet` + ledger in handler |
| Amount validated server-side against balance | ✅ | `example/wallet` — `int64` cents, overflow checks |
| No negative balance (DB constraint) | 🔲 | `CHECK (balance >= 0)` at DB layer still required |
| Immutable append-only ledger | ✅ | `ledger.RecordTransaction` + hash chain; enforce UPDATE/DELETE revoke in `ledger/migrations/` |
| Property-based invariant tests | ✅ | `example/wallet` gopter tests — balance ≥ 0, ledger sum = balance |

See [example/wallet/AUDIT.md](example/wallet/AUDIT.md) for regression findings on the legacy handler.

---

## 8. Observability

| Item | Status | floodguard / platform notes |
|------|--------|----------------------------|
| Log rejections with account, IP, reason, timestamp | ⚠️ | `Options.Logger` traces layers; use `Options.Audit` for structured events → metrics |
| Metrics/dashboards for rejection rates | 🔲 | Export `Audit` events to Prometheus/Datadog |
| Alerting on abnormal spikes | ✅ | `anomaly.DetectSpike` — wire `CheckAndAlert` to paging (alert only, do not block) |
| Conditional CAPTCHA under aggregate suspicion | ✅ | `challenge.ChallengeRequired` + middleware (stub verifier; wire real hCaptcha/Turnstile in prod) |

Suggested metric labels: `layer`, `reason`, `endpoint`.

---

## 9. Testing Discipline

| Item | Status | Command / location |
|------|--------|-------------------|
| `-race` on concurrency tests | ✅ | `go test -race ./...` |
| Run race tests many times | ⚠️ | CI: `go test -race -count=20 ./...` |
| Load test concurrent same-account traffic | ⚠️ | `middleware/concurrency_test.go` — add k6/vegeta in staging |
| Dependency failure scenarios tested | ✅ | `middleware/failclosed_test.go` |

---

## 10. Independent Review

| Item | Status | Notes |
|------|--------|-------|
| Lock + idempotency reviewed by distributed-systems / fintech engineer | 🔲 | Schedule before real funds |
| Compliance (AML, KYC, regional gambling rules) | 🔲 | Legal/compliance — rate limiting ≠ regulatory compliance |

---

## floodguard quick audit commands

```bash
# Unit + race tests
go test -race -count=20 ./...

# Vet + lint (matches CI)
go vet ./...
golangci-lint run ./...

# Example server (requires Redis)
redis-server &
go run example/main.go
```

---

## What this checklist deliberately does not claim

Even with every box checked, this does **not** fully prevent:

- A **stolen valid credential** at human speed on a trusted device (mitigated by devicetrust + stepup, not eliminated)
- An **insider** with direct database access (ledger makes tampering detectable; least-privilege DB roles are still mandatory)
- A **business-logic bug** nobody thought to test (property tests help; human review still required)
- A **large botnet** at low per-IP rates (anomaly + CAPTCHA buy visibility/friction; budget for a fraud vendor at scale)

Those require layered defenses plus organizational controls: DB grants, separation of duties, compliance review, and third-party fraud scoring at volume.

---

## Recommended production wiring

```go
// Inner handler stack (inside-out): handler → challenge → stepup → floodguard
inner := challenge.Middleware(challengeMgr, challengeOpts)(stepup.Middleware(stepUpMgr, stepUpOpts)(withdrawHandler))
http.Handle("/withdraw", middleware.Handler(guard, middleware.Options{
    Action:                "withdraw",
    RequireLock:           true,
    RequireIdempotencyKey: true,
    BlockOnVelocity:       ptr(false), // flag-only; step-up reads OnVelocityFlag context
    OnVelocityFlag:        setVelocityFlagOnContext,
    Audit:                 structuredLog.Emit,
})(inner))
```

See [README.md](README.md) and [example/main.go](example/main.go) for full setup with devicetrust, notify, and anomaly.

# Floodguard

[![Build](https://github.com/duncanmwirigi/floodguard-rate-limiter/actions/workflows/test.yml/badge.svg)](https://github.com/duncanmwirigi/floodguard-rate-limiter/actions/workflows/test.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/duncanmwirigi/floodguard-rate-limiter)](https://goreportcard.com/report/github.com/duncanmwirigi/floodguard-rate-limiter)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/duncanmwirigi/floodguard-rate-limiter.svg)](https://pkg.go.dev/github.com/duncanmwirigi/floodguard-rate-limiter)

**Floodguard** is an open-source Go library that protects HTTP and gRPC services from rapid or abusive traffic. It is built for high-stakes endpoints—withdrawals, bets, transfers, password resets—where duplicate or automated requests can drain accounts or degrade service for everyone else.

## Why Floodguard?

Financial and gaming APIs attract abuse that a plain rate limiter alone does not catch:

| Threat | Example | floodguard defense |
|--------|---------|-------------------|
| Request flooding | 500 bets/second from one account | **Rate limiting** (token bucket + sliding window) |
| Double-submit | User double-clicks "Withdraw" | **Idempotency keys** with cached responses |
| Race conditions | Parallel withdrawal requests | **Per-account distributed locks** |
| Anomaly / bot behavior | 30 withdrawals in 60 seconds | **Velocity rule engine** |

Each layer is swappable: use in-memory backends for development, or **Redis** for distributed production deployments.

## Install

```bash
go get github.com/duncanmwirigi/floodguard-rate-limiter
```

Requires Go 1.22+, Redis (example server), and for live scenarios: `curl` + `jq`.

## Verify it works (copy-paste)

**Step 1 — unit tests** (no Redis required for most packages):

```bash
cd floodguard
go test ./...
```

**Step 2 — live integration scenarios** (two terminals):

```bash
# Terminal 1: start Redis + example server
redis-server                          # if not already running
cp .env.example .env
set -a && source .env && set +a
go run ./example
```

```bash
# Terminal 2: run all 12 scenarios (expect 12 passed, 0 failed)
./scripts/run-scenarios.sh
```

Or use Make:

```bash
make test              # unit tests
make run               # start example server (Terminal 1)
make test-scenarios    # run ./scripts/run-scenarios.sh (Terminal 2)
```

**Step 3 — manual withdraw** (trust device first, then withdraw):

```bash
# Trust this device for acct-1001
curl -s -X POST http://localhost:8080/demo/trust-device \
  -H "X-Account-ID: acct-1001" \
  -H "X-Device-ID: device-trusted-1001" \
  -H "User-Agent: FloodguardTest/1.0" \
  -H "Accept-Language: en-US"

# Withdraw KES 10.00 (1000 cents)
curl -s -X POST http://localhost:8080/withdraw \
  -H "X-Account-ID: acct-1001" \
  -H "Idempotency-Key: wd-manual-001" \
  -H "X-Device-ID: device-trusted-1001" \
  -H "User-Agent: FloodguardTest/1.0" \
  -H "Accept-Language: en-US" \
  -H "Content-Type: application/json" \
  -d '{"amount_cents":1000}'

# Check balance
curl -s http://localhost:8080/demo/balance -H "X-Account-ID: acct-1001"
```

## Project structure

```
floodguard/
├── config/                  # Environment variable loader (.env.example)
├── ratelimit/               # Token bucket + Redis sliding window
├── idempotency/             # Atomic idempotency keys
├── lock/                    # Distributed locks
├── velocity/                # Behavioral rule engine
├── middleware/              # HTTP protection stack
├── devicetrust/             # Device fingerprinting
├── stepup/                  # Risk-based OTP/2FA
├── notify/                  # Async sensitive-action alerts
├── ledger/                  # Hash-chained audit log
├── anomaly/                 # Platform-wide spike detection
├── challenge/               # Conditional CAPTCHA
├── example/
│   ├── main.go              # Runnable demo server
│   ├── app/                 # Server wiring + integration tests
│   ├── wallet/              # KES cents + property tests
│   └── testdata/            # Demo accounts + scenario catalog
├── scripts/
│   └── run-scenarios.sh     # curl integration runner (12 scenarios)
├── Makefile                 # make test | run | test-scenarios
├── .env.example             # Configuration template
└── PRODUCTION_READINESS.md  # Pre-launch audit checklist
```

## Quickstart

```go
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/duncanmwirigi/floodguard-rate-limiter"
	"github.com/duncanmwirigi/floodguard-rate-limiter/middleware"
	"github.com/duncanmwirigi/floodguard-rate-limiter/ratelimit"
	"github.com/duncanmwirigi/floodguard-rate-limiter/velocity"
	"golang.org/x/time/rate"
)

func main() {
	g := floodguard.New(floodguard.Config{
		IPRateLimiter: ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
			Rate:  rate.Limit(50),
			Burst: 100,
		}),
		RateLimiter: ratelimit.NewTokenBucket(ratelimit.TokenBucketConfig{
			Rate:  rate.Limit(5),
			Burst: 10,
		}),
		Velocity: velocity.Config{
			Rules: []velocity.Rule{
				velocity.RateOverWindow{N: 20, Window: time.Minute, Label: "withdrawals"},
			},
		},
	})

	mux := http.NewServeMux()
	mux.Handle("/withdraw", middleware.Handler(g, middleware.Options{
		Action:      "withdraw",
		RequireLock: true,
	})(http.HandlerFunc(withdrawHandler)))

	log.Fatal(http.ListenAndServe(":8080", mux))
}

func withdrawHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"completed"}`))
}
```

Send mutating requests with an **`Idempotency-Key`** header. Account identity defaults to **`X-Account-ID`** (falls back to client IP). Override both via `middleware.Options`.

## Companion packages (Gaps 1–4)

Floodguard is the concurrency/abuse layer. These sibling packages close adjacent gaps:

| Package | Gap | Purpose |
|---------|-----|---------|
| [`devicetrust/`](devicetrust/) | Stolen credentials | Device fingerprinting + known-device detection |
| [`stepup/`](stepup/) | Stolen credentials | Risk-based OTP/2FA middleware (composes with velocity flags) |
| [`notify/`](notify/) | Stolen credentials | Fire-and-forget alerts on sensitive actions from new devices |
| [`ledger/`](ledger/) | Insider DB access | Append-only hash-chained audit log + tamper detection |
| [`anomaly/`](anomaly/) | Distributed botnet | Platform-wide spike detection (alert, not block) |
| [`challenge/`](challenge/) | Distributed botnet | Conditional CAPTCHA middleware (stub verifier for hCaptcha/Turnstile) |
| [`example/wallet/`](example/wallet/) | Business logic bugs | KES cents + property/regression tests |

**Recommended build order:** floodguard → wallet property tests → devicetrust + stepup → ledger → anomaly + challenge.

See [PRODUCTION_READINESS.md](PRODUCTION_READINESS.md) and [example/wallet/AUDIT.md](example/wallet/AUDIT.md).

## Architecture

A request passes through four layers before reaching your handler:

```mermaid
flowchart TD
    A([Incoming request]) --> B[1. IP rate limit]
    B -->|reject| R1[429 ip_rate_limit]
    B --> C[2. Account rate limit]
    C -->|reject| R2[429 rate_limit]
    C --> D[3. Idempotency check]
    D -->|replay| R3[200 cached response]
    D -->|in-flight| R4[409 conflict]
    D --> E[4. Velocity rules]
    E -->|reject| R5[429 velocity]
    E --> F[5. Distributed lock]
    F -->|reject| R6[409 locked]
    F --> G[Handler: balance check + deduct]
    G --> H[6. Release lock + cache result]
    H --> A2([Response])
```

| Package | Role |
|---------|------|
| [`floodguard`](floodguard.go) | `Guard` wires all subsystems; call `Protect` or use middleware |
| [`ratelimit/`](ratelimit/) | Per-key token bucket (memory) or sliding window (Redis) |
| [`idempotency/`](idempotency/) | Atomic claim + response cache by idempotency key |
| [`velocity/`](velocity/) | Composable rule engine for abuse patterns |
| [`lock/`](lock/) | Distributed lock per account/resource |
| [`middleware/`](middleware/) | HTTP middleware chaining all layers |
| [`config/`](config/) | Environment-based configuration loader (`.env.example`) |
| [`example/`](example/) | Runnable `POST /withdraw` demo with Redis |

Storage backends implement small interfaces (`Store`, `Limiter`, `Client`) so you can swap in-memory and Redis implementations without changing handler code.

## Redis production setup

```go
client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

rateLimiter, _ := ratelimit.NewRedisSlidingWindow(client, "myapp", ratelimit.SlidingWindowConfig{
	Limit:  100,
	Window: time.Minute,
})

g := floodguard.New(floodguard.Config{
	IPRateLimiter: ipRateLimiter,
	RateLimiter:   accountRateLimiter,
	Idempotency: idempotency.Config{Store: idempotency.NewRedisStore(client, "myapp")},
	Lock:        lock.Config{Client: lock.NewRedis(client, "myapp")},
	Velocity: velocity.Config{
		Store: velocity.NewRedisStore(client, "myapp"),
		Rules: []velocity.Rule{
			velocity.RateOverWindow{N: 5, Window: time.Minute, Label: "withdrawals"},
			velocity.MinInterval{Min: 300 * time.Millisecond, Label: "bet"},
		},
	},
})
```

## Example server

Copy [`.env.example`](.env.example) and load it before starting. See **[Verify it works](#verify-it-works-copy-paste)** for the full flow.

**Routes:**

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/health` | Liveness check |
| `POST` | `/withdraw` | Protected withdrawal (requires headers below) |
| `POST` | `/demo/trust-device` | Mark device trusted (demo/tests only) |
| `GET` | `/demo/balance` | Read demo account balance |

**Required headers for `/withdraw`:**

| Header | Example | Purpose |
|--------|---------|---------|
| `X-Account-ID` | `acct-1001` | Account identity |
| `Idempotency-Key` | `wd-001` | Duplicate-submit protection |
| `X-Device-ID` | `device-trusted-1001` | Device fingerprint input |
| `User-Agent` | `FloodguardTest/1.0` | Fingerprint input |
| `Accept-Language` | `en-US` | Fingerprint input |

Optional: `X-Step-Up-Token` (after 403 step-up), `X-Challenge-Token` + `X-Captcha-Response: valid-captcha` (after 403 CAPTCHA).

```bash
cp .env.example .env
set -a && source .env && set +a
go run ./example
```

Server logs trace each layer: rate limit → idempotency → velocity → lock → step-up → challenge → handler.

## Configuration

All tunable settings load from environment variables via [`config.Load()`](config/config.go). Defaults match `.env.example`.

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_ADDR` | `:8080` | HTTP listen address |
| `REDIS_ADDR` | `localhost:6379` | Redis connection |
| `REDIS_KEY_PREFIX` | `floodguard` | Namespace for all Redis keys |
| `REDIS_PING_TIMEOUT` | `3s` | Startup Redis health check timeout |
| `IP_RATE_LIMIT` | `30` | Max requests per IP per window |
| `IP_RATE_WINDOW` | `1m` | IP rate limit window |
| `ACCOUNT_RATE_LIMIT` | `10` | Max requests per account per window |
| `ACCOUNT_RATE_WINDOW` | `1m` | Account rate limit window |
| `IDEMPOTENCY_TTL` | `24h` | Cached idempotent response lifetime |
| `VELOCITY_WITHDRAW_MAX` | `3` | Max withdrawal attempts per velocity window |
| `VELOCITY_WINDOW` | `1m` | Velocity rule window |
| `VELOCITY_MIN_INTERVAL` | `200ms` | Minimum time between withdrawals |
| `LOCK_TTL` | `30s` | Distributed lock TTL |
| `BLOCK_ON_VELOCITY` | `false` | Block on velocity (false = flag-only for step-up) |
| `REQUIRE_IDEMPOTENCY_KEY` | `true` | Reject requests without Idempotency-Key |
| `REQUIRE_LOCK` | `true` | Acquire distributed lock before handler |
| `ANOMALY_SPIKE_MULTIPLIER` | `5` | Platform spike threshold (current vs baseline) |
| `ANOMALY_LOOKBACK_MINUTES` | `60` | Baseline lookback for spike detection |
| `CHALLENGE_NEW_ACCOUNT_MAX_AGE` | `24h` | Accounts younger than this require CAPTCHA |
| `STEPUP_TOKEN_TTL` | `10m` | Step-up challenge token lifetime |
| `DEVICE_TRUST_STORE` | `memory` | `memory` or `redis` |
| `NOTIFY_ENABLED` | `true` | Send alerts on sensitive actions from new devices |
| `DEMO_ACCOUNT_BALANCES` | `acct-1001=50000,...` | Demo seed balances (KES cents) |

In your own app, import `github.com/duncanmwirigi/floodguard-rate-limiter/config` and map the struct fields into each package's config at startup — no need to fork the loader.

## Testing

### Unit and property tests

```bash
go test ./...              # all packages
go test -race ./...        # with race detector
go test ./example/wallet/  # property-based balance invariants (gopter)
go test ./example/app/     # HTTP scenario tests (miniredis, no live server)
```

### Test data

Demo accounts and headers are defined in [`example/testdata/accounts.json`](example/testdata/accounts.json).

**Currency:** amounts use **KES minor units (cents)** — `50000` = KES 500.00, `1000` = KES 10.00.

| Account | Balance | Notes |
|---------|---------|-------|
| `acct-1001` | KES 500.00 (50000 cents) | Primary demo account, mature |
| `acct-1002` | KES 100.00 (10000 cents) | Secondary account |
| `acct-new` | KES 0 | Created on first request — triggers CAPTCHA |
| `acct-empty` | KES 1.00 (100 cents) | Insufficient-funds tests |

Trust a device before withdrawing from a mature account:

```bash
curl -s -X POST http://localhost:8080/demo/trust-device \
  -H "X-Account-ID: acct-1001" \
  -H "X-Device-ID: device-trusted-1001" \
  -H "User-Agent: FloodguardTest/1.0" \
  -H "Accept-Language: en-US"
```

Stub CAPTCHA token for tests: `valid-captcha` (see `challenge.StubVerifier`).

### Integration scenarios (live server)

[`example/testdata/scenarios.json`](example/testdata/scenarios.json) catalogs each scenario. [`scripts/run-scenarios.sh`](scripts/run-scenarios.sh) runs them against a live server (**requires `curl`, `jq`, Redis, and the example server running**):

```bash
# Terminal 1
redis-server
cp .env.example .env && set -a && source .env && set +a
go run ./example

# Terminal 2
./scripts/run-scenarios.sh
# Expected: ==> Results: 12 passed, 0 failed
```

| # | Scenario | Expected |
|---|----------|----------|
| 01 | Health check | `200 OK` |
| 02 | Trust device | `200` + device marked trusted |
| 03 | Withdraw (trusted device) | `200` + balance deducted |
| 04 | Idempotent replay | `200` + `X-Idempotent-Replay: true` |
| 05 | Insufficient funds | `402 Payment Required` |
| 06 | Invalid amount (negative) | `400 Bad Request` |
| 07 | Missing Idempotency-Key | `400 Bad Request` |
| 08 | Step-up (unknown device) | `403` + `challenge_token` |
| 08b | Step-up with token | `200` after `X-Step-Up-Token` |
| 09 | CAPTCHA (new account) | `403` + `X-Challenge-Required` |
| 09b | CAPTCHA solved | `402` (passed challenge; KES 0 balance) |
| 10 | Balance check | `200` + `balance_cents` |

The script pauses 300ms between withdrawals to avoid velocity `MinInterval` collisions. Override server URL: `BASE_URL=http://localhost:9090 ./scripts/run-scenarios.sh`

## Error handling

Sentinel errors support `errors.Is` / `errors.As` throughout:

```go
result, err := g.Protect(ctx, req)
if floodguard.IsDuplicateInFlight(err) {
	// 409 Conflict — same idempotency key already in flight
}
if errors.Is(err, floodguard.ErrKeyRequired) {
	// missing account key
}
if floodguard.IsRejected(err, floodguard.RejectRateLimit) {
	// programmatic rejection
}
```

Subpackages export their own sentinels (`ratelimit.ErrKeyRequired`, `lock.ErrNotAcquired`, `idempotency.ErrInFlight`, etc.). Operational failures are wrapped with `%w`.

## HTTP status codes

| Condition | Status |
|-----------|--------|
| IP rate limit exceeded | `429 Too Many Requests` (`ip_rate_limit`) |
| Account rate limit exceeded | `429 Too Many Requests` (`rate_limit`) |
| Velocity threshold exceeded | `429 Too Many Requests` |
| Resource locked | `409 Conflict` |
| Duplicate in-flight idempotency key | `409 Conflict` |
| Idempotent replay | `200 OK` + `X-Idempotent-Replay: true` |
| Step-up required (unknown device) | `403 Forbidden` + `challenge_token` |
| CAPTCHA required (new account) | `403 Forbidden` + `X-Challenge-Required` |
| Insufficient funds | `402 Payment Required` |

## Production readiness

Before handling real funds, walk through [PRODUCTION_READINESS.md](PRODUCTION_READINESS.md) — an audit checklist mapping each control to floodguard capabilities and platform responsibilities.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). CI runs `go vet`, `go test -race`, and `golangci-lint` on every push and pull request.

## Changelog

See [CHANGELOG.md](CHANGELOG.md).

## Author

**Duncan Mwirigi**  
GitHub: [github.com/duncanmwirigi](https://github.com/duncanmwirigi)  
X: https://x.com/AIStiqDan  
Website: https://bytecityinc.com

## License

MIT — see [LICENSE](LICENSE).

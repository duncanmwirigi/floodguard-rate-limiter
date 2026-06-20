# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`devicetrust`** — device fingerprinting, known-device detection, Redis/memory stores.
- **`stepup`** — risk-based step-up auth middleware (composes with velocity flags).
- **`notify`** — async `AfterSensitiveAction` alerts for new-device sensitive actions.
- **`ledger`** — append-only hash-chained audit log with `VerifyChainIntegrity` + Postgres migration.
- **`anomaly`** — platform-wide spike detection (`DetectSpike`) for distributed abuse patterns.
- **`challenge`** — conditional CAPTCHA middleware with stub verifier.
- **`example/wallet`** — `int64` cents wallet + gopter property tests + regression audit.
- **`config`** — environment variable loader with defaults (see `.env.example`).
- **`example/app`** — integration scenario tests + demo routes (`/demo/trust-device`, `/demo/balance`).
- **`example/testdata`** — demo accounts and scenario catalog for manual + scripted testing.
- **`scripts/run-scenarios.sh`** — curl integration runner against live example server.
- Example server wired with full stack: floodguard → stepup → challenge + devicetrust + notify + anomaly.
- Initial release of floodguard: rate limiting, idempotency, distributed locks, and velocity rules.
- HTTP middleware chaining all protection layers with optional distributed lock.
- Redis-backed stores for idempotency, locks, velocity, and sliding-window rate limits.
- In-memory backends for local development and tests.
- Runnable `example/main.go` demonstrating `POST /withdraw` protection.
- GitHub Actions CI (`go vet`, `go test`, `golangci-lint`).

### Security

- Idempotency claims use atomic SETNX (Lua) to prevent duplicate processing.
- Distributed locks use token-verified release to prevent accidental unlock by another caller.

## [0.1.0] - 2026-06-20

### Added

- First public release.

[Unreleased]: https://github.com/duncanmwirigi/floodguard-rate-limiter/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/duncanmwirigi/floodguard-rate-limiter/releases/tag/v0.1.0

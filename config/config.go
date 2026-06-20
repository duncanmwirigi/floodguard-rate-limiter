// Package config loads floodguard settings from environment variables with
// sensible defaults. Copy .env.example to .env (or export vars in your shell /
// container) and call config.Load() at startup.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all tunable floodguard settings for the example server and
// can be reused in production apps by mapping fields into package configs.
type Config struct {
	Server     Server
	Redis      Redis
	RateLimit  RateLimit
	Idempotency Idempotency
	Velocity   Velocity
	Lock       Lock
	Middleware Middleware
	Anomaly    Anomaly
	Challenge  Challenge
	StepUp     StepUp
	DeviceTrust DeviceTrust
	Notify     Notify
	Demo       Demo
}

// Server settings.
type Server struct {
	ListenAddr string
}

// Redis connection and key namespace.
type Redis struct {
	Addr       string
	KeyPrefix  string
	PingTimeout time.Duration
}

// RateLimit configures IP and account sliding windows.
type RateLimit struct {
	IPLimit       int
	IPWindow      time.Duration
	AccountLimit  int
	AccountWindow time.Duration
}

// Idempotency settings.
type Idempotency struct {
	TTL time.Duration
}

// Velocity rule thresholds.
type Velocity struct {
	WithdrawMaxPerWindow int
	Window               time.Duration
	MinInterval          time.Duration
}

// Lock TTL for distributed balance operations.
type Lock struct {
	TTL time.Duration
}

// Middleware toggles for the HTTP protection stack.
type Middleware struct {
	BlockOnVelocity       bool
	RequireIdempotencyKey bool
	RequireLock           bool
}

// Anomaly platform-wide spike detection.
type Anomaly struct {
	SpikeMultiplier  float64
	LookbackMinutes  int
}

// Challenge CAPTCHA / proof-of-work layer.
type Challenge struct {
	NewAccountMaxAge time.Duration
}

// StepUp OTP/2FA challenge token lifetime.
type StepUp struct {
	TokenTTL time.Duration
}

// DeviceTrust store backend selection.
type DeviceTrust struct {
	Store string // "memory" or "redis"
}

// Notify async alert delivery.
type Notify struct {
	Enabled bool
}

// Demo seed data for the example server.
type Demo struct {
	AccountBalances map[string]int64 // account_id -> KES cents
}

// Load reads configuration from environment variables.
func Load() (Config, error) {
	cfg := Defaults()
	var err error

	cfg.Server.ListenAddr = envString("LISTEN_ADDR", cfg.Server.ListenAddr)

	cfg.Redis.Addr = envString("REDIS_ADDR", cfg.Redis.Addr)
	cfg.Redis.KeyPrefix = envString("REDIS_KEY_PREFIX", cfg.Redis.KeyPrefix)
	cfg.Redis.PingTimeout, err = envDuration("REDIS_PING_TIMEOUT", cfg.Redis.PingTimeout)
	if err != nil {
		return Config{}, fmt.Errorf("REDIS_PING_TIMEOUT: %w", err)
	}

	cfg.RateLimit.IPLimit, err = envInt("IP_RATE_LIMIT", cfg.RateLimit.IPLimit)
	if err != nil {
		return Config{}, fmt.Errorf("IP_RATE_LIMIT: %w", err)
	}
	cfg.RateLimit.IPWindow, err = envDuration("IP_RATE_WINDOW", cfg.RateLimit.IPWindow)
	if err != nil {
		return Config{}, fmt.Errorf("IP_RATE_WINDOW: %w", err)
	}
	cfg.RateLimit.AccountLimit, err = envInt("ACCOUNT_RATE_LIMIT", cfg.RateLimit.AccountLimit)
	if err != nil {
		return Config{}, fmt.Errorf("ACCOUNT_RATE_LIMIT: %w", err)
	}
	cfg.RateLimit.AccountWindow, err = envDuration("ACCOUNT_RATE_WINDOW", cfg.RateLimit.AccountWindow)
	if err != nil {
		return Config{}, fmt.Errorf("ACCOUNT_RATE_WINDOW: %w", err)
	}

	cfg.Idempotency.TTL, err = envDuration("IDEMPOTENCY_TTL", cfg.Idempotency.TTL)
	if err != nil {
		return Config{}, fmt.Errorf("IDEMPOTENCY_TTL: %w", err)
	}

	cfg.Velocity.WithdrawMaxPerWindow, err = envInt("VELOCITY_WITHDRAW_MAX", cfg.Velocity.WithdrawMaxPerWindow)
	if err != nil {
		return Config{}, fmt.Errorf("VELOCITY_WITHDRAW_MAX: %w", err)
	}
	cfg.Velocity.Window, err = envDuration("VELOCITY_WINDOW", cfg.Velocity.Window)
	if err != nil {
		return Config{}, fmt.Errorf("VELOCITY_WINDOW: %w", err)
	}
	cfg.Velocity.MinInterval, err = envDuration("VELOCITY_MIN_INTERVAL", cfg.Velocity.MinInterval)
	if err != nil {
		return Config{}, fmt.Errorf("VELOCITY_MIN_INTERVAL: %w", err)
	}

	cfg.Lock.TTL, err = envDuration("LOCK_TTL", cfg.Lock.TTL)
	if err != nil {
		return Config{}, fmt.Errorf("LOCK_TTL: %w", err)
	}

	cfg.Middleware.BlockOnVelocity, err = envBool("BLOCK_ON_VELOCITY", cfg.Middleware.BlockOnVelocity)
	if err != nil {
		return Config{}, fmt.Errorf("BLOCK_ON_VELOCITY: %w", err)
	}
	cfg.Middleware.RequireIdempotencyKey, err = envBool("REQUIRE_IDEMPOTENCY_KEY", cfg.Middleware.RequireIdempotencyKey)
	if err != nil {
		return Config{}, fmt.Errorf("REQUIRE_IDEMPOTENCY_KEY: %w", err)
	}
	cfg.Middleware.RequireLock, err = envBool("REQUIRE_LOCK", cfg.Middleware.RequireLock)
	if err != nil {
		return Config{}, fmt.Errorf("REQUIRE_LOCK: %w", err)
	}

	cfg.Anomaly.SpikeMultiplier, err = envFloat("ANOMALY_SPIKE_MULTIPLIER", cfg.Anomaly.SpikeMultiplier)
	if err != nil {
		return Config{}, fmt.Errorf("ANOMALY_SPIKE_MULTIPLIER: %w", err)
	}
	cfg.Anomaly.LookbackMinutes, err = envInt("ANOMALY_LOOKBACK_MINUTES", cfg.Anomaly.LookbackMinutes)
	if err != nil {
		return Config{}, fmt.Errorf("ANOMALY_LOOKBACK_MINUTES: %w", err)
	}

	cfg.Challenge.NewAccountMaxAge, err = envDuration("CHALLENGE_NEW_ACCOUNT_MAX_AGE", cfg.Challenge.NewAccountMaxAge)
	if err != nil {
		return Config{}, fmt.Errorf("CHALLENGE_NEW_ACCOUNT_MAX_AGE: %w", err)
	}

	cfg.StepUp.TokenTTL, err = envDuration("STEPUP_TOKEN_TTL", cfg.StepUp.TokenTTL)
	if err != nil {
		return Config{}, fmt.Errorf("STEPUP_TOKEN_TTL: %w", err)
	}

	cfg.DeviceTrust.Store = envString("DEVICE_TRUST_STORE", cfg.DeviceTrust.Store)
	if cfg.DeviceTrust.Store != "memory" && cfg.DeviceTrust.Store != "redis" {
		return Config{}, fmt.Errorf("DEVICE_TRUST_STORE: must be memory or redis, got %q", cfg.DeviceTrust.Store)
	}

	cfg.Notify.Enabled, err = envBool("NOTIFY_ENABLED", cfg.Notify.Enabled)
	if err != nil {
		return Config{}, fmt.Errorf("NOTIFY_ENABLED: %w", err)
	}

	if raw := os.Getenv("DEMO_ACCOUNT_BALANCES"); raw != "" {
		balances, parseErr := parseAccountBalances(raw)
		if parseErr != nil {
			return Config{}, fmt.Errorf("DEMO_ACCOUNT_BALANCES: %w", parseErr)
		}
		cfg.Demo.AccountBalances = balances
	}

	return cfg, nil
}

// Defaults returns production-ish defaults used when env vars are unset.
func Defaults() Config {
	return Config{
		Server: Server{ListenAddr: ":8080"},
		Redis: Redis{
			Addr:        "localhost:6379",
			KeyPrefix:   "floodguard",
			PingTimeout: 3 * time.Second,
		},
		RateLimit: RateLimit{
			IPLimit:       30,
			IPWindow:      time.Minute,
			AccountLimit:  10,
			AccountWindow: time.Minute,
		},
		Idempotency: Idempotency{TTL: 24 * time.Hour},
		Velocity: Velocity{
			WithdrawMaxPerWindow: 3,
			Window:               time.Minute,
			MinInterval:          200 * time.Millisecond,
		},
		Lock: Lock{TTL: 30 * time.Second},
		Middleware: Middleware{
			BlockOnVelocity:       false,
			RequireIdempotencyKey: true,
			RequireLock:           true,
		},
		Anomaly: Anomaly{
			SpikeMultiplier: 5.0,
			LookbackMinutes: 60,
		},
		Challenge: Challenge{NewAccountMaxAge: 24 * time.Hour},
		StepUp:    StepUp{TokenTTL: 10 * time.Minute},
		DeviceTrust: DeviceTrust{Store: "memory"},
		Notify:    Notify{Enabled: true},
		Demo: Demo{AccountBalances: map[string]int64{
			"acct-1001": 50_000,
			"acct-1002": 10_000,
		}},
	}
}

// parseAccountBalances parses "acct-1=50000,acct-2=10000" into a map.
func parseAccountBalances(raw string) (map[string]int64, error) {
	out := make(map[string]int64)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("expected account=cents pairs, got %q", part)
		}
		cents, err := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cents for %q: %w", kv[0], err)
		}
		out[strings.TrimSpace(kv[0])] = cents
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no balances parsed")
	}
	return out, nil
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func envFloat(key string, fallback float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	return strconv.ParseFloat(v, 64)
}

func envBool(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	return strconv.ParseBool(v)
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	return time.ParseDuration(v)
}

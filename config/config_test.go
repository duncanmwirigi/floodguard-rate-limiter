package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/ultimateprogrammer/floodguard/config"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("REDIS_ADDR", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Redis.Addr != "localhost:6379" {
		t.Fatalf("redis addr=%q", cfg.Redis.Addr)
	}
	if cfg.RateLimit.IPLimit != 30 {
		t.Fatalf("ip limit=%d", cfg.RateLimit.IPLimit)
	}
}

func TestLoad_OverrideFromEnv(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":9090")
	t.Setenv("REDIS_ADDR", "redis:6379")
	t.Setenv("REDIS_KEY_PREFIX", "prod")
	t.Setenv("IP_RATE_LIMIT", "100")
	t.Setenv("IP_RATE_WINDOW", "2m")
	t.Setenv("ACCOUNT_RATE_LIMIT", "20")
	t.Setenv("VELOCITY_WITHDRAW_MAX", "5")
	t.Setenv("VELOCITY_MIN_INTERVAL", "500ms")
	t.Setenv("LOCK_TTL", "45s")
	t.Setenv("IDEMPOTENCY_TTL", "48h")
	t.Setenv("BLOCK_ON_VELOCITY", "true")
	t.Setenv("ANOMALY_SPIKE_MULTIPLIER", "8")
	t.Setenv("ANOMALY_LOOKBACK_MINUTES", "120")
	t.Setenv("DEVICE_TRUST_STORE", "redis")
	t.Setenv("DEMO_ACCOUNT_BALANCES", "acct-a=1000,acct-b=2000")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Server.ListenAddr != ":9090" {
		t.Fatalf("listen=%q", cfg.Server.ListenAddr)
	}
	if cfg.Redis.KeyPrefix != "prod" {
		t.Fatalf("prefix=%q", cfg.Redis.KeyPrefix)
	}
	if cfg.RateLimit.IPLimit != 100 || cfg.RateLimit.IPWindow != 2*time.Minute {
		t.Fatalf("rate limit=%+v", cfg.RateLimit)
	}
	if cfg.Velocity.WithdrawMaxPerWindow != 5 || cfg.Velocity.MinInterval != 500*time.Millisecond {
		t.Fatalf("velocity=%+v", cfg.Velocity)
	}
	if cfg.Lock.TTL != 45*time.Second {
		t.Fatalf("lock ttl=%v", cfg.Lock.TTL)
	}
	if !cfg.Middleware.BlockOnVelocity {
		t.Fatal("expected block on velocity")
	}
	if cfg.Anomaly.SpikeMultiplier != 8 || cfg.Anomaly.LookbackMinutes != 120 {
		t.Fatalf("anomaly=%+v", cfg.Anomaly)
	}
	if cfg.DeviceTrust.Store != "redis" {
		t.Fatalf("device store=%q", cfg.DeviceTrust.Store)
	}
	if cfg.Demo.AccountBalances["acct-a"] != 1000 || cfg.Demo.AccountBalances["acct-b"] != 2000 {
		t.Fatalf("demo balances=%v", cfg.Demo.AccountBalances)
	}
}

func TestLoad_InvalidEnv(t *testing.T) {
	t.Setenv("IP_RATE_LIMIT", "not-a-number")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error for invalid IP_RATE_LIMIT")
	}
}

func TestLoad_InvalidDeviceStore(t *testing.T) {
	t.Setenv("DEVICE_TRUST_STORE", "postgres")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error for invalid DEVICE_TRUST_STORE")
	}
}

func TestMain(m *testing.M) {
	// Clear env vars that other tests may set so defaults test is isolated.
	for _, k := range []string{
		"LISTEN_ADDR", "REDIS_ADDR", "REDIS_KEY_PREFIX", "IP_RATE_LIMIT",
		"IP_RATE_WINDOW", "ACCOUNT_RATE_LIMIT", "VELOCITY_WITHDRAW_MAX",
		"VELOCITY_MIN_INTERVAL", "LOCK_TTL", "IDEMPOTENCY_TTL", "BLOCK_ON_VELOCITY",
		"ANOMALY_SPIKE_MULTIPLIER", "ANOMALY_LOOKBACK_MINUTES", "DEVICE_TRUST_STORE",
		"DEMO_ACCOUNT_BALANCES",
	} {
		_ = os.Unsetenv(k)
	}
	os.Exit(m.Run())
}

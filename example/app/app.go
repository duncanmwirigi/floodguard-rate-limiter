// Package app wires the floodguard example HTTP server for production demos and tests.
package app

import (
	"context"
	"log"
	"net/http"

	"github.com/redis/go-redis/v9"
	"github.com/ultimateprogrammer/floodguard"
	"github.com/ultimateprogrammer/floodguard/anomaly"
	"github.com/ultimateprogrammer/floodguard/challenge"
	"github.com/ultimateprogrammer/floodguard/config"
	"github.com/ultimateprogrammer/floodguard/devicetrust"
	"github.com/ultimateprogrammer/floodguard/idempotency"
	"github.com/ultimateprogrammer/floodguard/lock"
	"github.com/ultimateprogrammer/floodguard/middleware"
	"github.com/ultimateprogrammer/floodguard/notify"
	"github.com/ultimateprogrammer/floodguard/ratelimit"
	"github.com/ultimateprogrammer/floodguard/stepup"
	"github.com/ultimateprogrammer/floodguard/velocity"
)

// Server is the runnable example application.
type Server struct {
	Mux         *http.ServeMux
	DeviceTrust *devicetrust.Client
	StepUp      *stepup.Manager
	Challenge   *challenge.Manager
	Anomaly     *anomaly.Detector
	Service     *Service
	Redis       *redis.Client
}

// New builds the full middleware stack and demo routes.
func New(cfg config.Config, client *redis.Client) (*Server, error) {
	prefix := cfg.Redis.KeyPrefix

	ipRateLimiter, err := ratelimit.NewRedisSlidingWindow(client, prefix+":ip", ratelimit.SlidingWindowConfig{
		Limit:  cfg.RateLimit.IPLimit,
		Window: cfg.RateLimit.IPWindow,
	})
	if err != nil {
		return nil, err
	}
	accountRateLimiter, err := ratelimit.NewRedisSlidingWindow(client, prefix+":acct", ratelimit.SlidingWindowConfig{
		Limit:  cfg.RateLimit.AccountLimit,
		Window: cfg.RateLimit.AccountWindow,
	})
	if err != nil {
		return nil, err
	}

	idemStore := idempotency.NewRedisStore(client, prefix)
	velStore := velocity.NewRedisStore(client, prefix)
	lockClient := lock.NewRedis(client, prefix)

	guard := floodguard.New(floodguard.Config{
		IPRateLimiter: ipRateLimiter,
		RateLimiter:   accountRateLimiter,
		Idempotency:   idempotency.Config{Store: idemStore, TTL: cfg.Idempotency.TTL},
		Velocity: velocity.Config{
			Store: velStore,
			Rules: []velocity.Rule{
				velocity.RateOverWindow{
					N:      cfg.Velocity.WithdrawMaxPerWindow,
					Window: cfg.Velocity.Window,
					Label:  "withdrawal attempts",
				},
				velocity.MinInterval{
					Min:   cfg.Velocity.MinInterval,
					Label: "withdrawal",
				},
			},
		},
		Lock: lock.Config{Client: lockClient, TTL: cfg.Lock.TTL},
	})

	deviceTrust := newDeviceTrust(cfg, client)
	stepUp := stepup.NewManager(stepup.DefaultAssessor{}, stepup.NewMemoryChallengeStore(),
		stepup.WithTokenTTL(cfg.StepUp.TokenTTL))

	var notifier *notify.Notifier
	if cfg.Notify.Enabled {
		notifier = notify.New(nil, log.Default())
	}

	anomalyDet := anomaly.New(anomaly.Config{
		Counter:    anomaly.NewRedisCounter(client, prefix+":anomaly"),
		Multiplier: cfg.Anomaly.SpikeMultiplier,
	})
	challengeMgr := challenge.NewManager(challenge.StubVerifier{}, challenge.NewMemoryTokenStore())

	svc := NewService(cfg.Demo.AccountBalances, deviceTrust, notifier, anomalyDet)

	velocityFlagged := func(r *http.Request, _ string) bool {
		return r.Context().Value(velocityFlagKey{}) != nil
	}

	newAccountMaxAge := cfg.Challenge.NewAccountMaxAge
	lookback := cfg.Anomaly.LookbackMinutes

	var inner http.Handler = http.HandlerFunc(svc.Withdraw)
	inner = challenge.Middleware(challengeMgr, challenge.Options{
		Action: "withdraw",
		Signals: func(r *http.Request, accountID string) challenge.RiskSignals {
			spike, _, _, _ := anomalyDet.DetectSpike(r.Context(), anomaly.MetricWithdrawalAttempts, lookback)
			return challenge.RiskSignals{
				AccountAge:       svc.AccountAge(accountID),
				PlatformSpike:    spike,
				VelocityFlagged:  velocityFlagged(r, accountID),
				NewAccountMaxAge: newAccountMaxAge,
			}
		},
	})(inner)
	inner = stepup.Middleware(stepUp, stepup.Options{
		Action: "withdraw",
		DeviceFP: func(r *http.Request) string {
			return devicetrust.FingerprintFromRequest(r, "")
		},
		KnownDevice: func(r *http.Request, accountID string) (bool, error) {
			fp := devicetrust.FingerprintFromRequest(r, "")
			return deviceTrust.IsKnownDevice(r.Context(), accountID, fp)
		},
		VelocityFlagged: velocityFlagged,
		Logger:          log.Default(),
	})(inner)

	blockVelocity := cfg.Middleware.BlockOnVelocity
	withdrawChain := middleware.Handler(guard, middleware.Options{
		Action:                "withdraw",
		RequireLock:           cfg.Middleware.RequireLock,
		RequireIdempotencyKey: cfg.Middleware.RequireIdempotencyKey,
		BlockOnVelocity:       &blockVelocity,
		Logger:                log.Default(),
		OnVelocityFlag: func(w http.ResponseWriter, r *http.Request, reason string) {
			log.Printf("[velocity-flag] account=%s reason=%s", r.Header.Get("X-Account-ID"), reason)
			*r = *r.WithContext(context.WithValue(r.Context(), velocityFlagKey{}, true))
		},
	})(inner)

	mux := http.NewServeMux()
	mux.Handle("/withdraw", withdrawChain)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/demo/trust-device", svc.TrustDevice)
	mux.HandleFunc("/demo/balance", svc.Balance)

	return &Server{
		Mux:         mux,
		DeviceTrust: deviceTrust,
		StepUp:      stepUp,
		Challenge:   challengeMgr,
		Anomaly:     anomalyDet,
		Service:     svc,
		Redis:       client,
	}, nil
}

func newDeviceTrust(cfg config.Config, client *redis.Client) *devicetrust.Client {
	switch cfg.DeviceTrust.Store {
	case "redis":
		return devicetrust.New(devicetrust.NewRedisStore(client, cfg.Redis.KeyPrefix+":devicetrust"))
	default:
		return devicetrust.New(devicetrust.NewMemoryStore())
	}
}

type velocityFlagKey struct{}

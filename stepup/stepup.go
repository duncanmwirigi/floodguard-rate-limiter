// Package stepup requires OTP/2FA verification for sensitive actions when
// risk signals (unknown device, velocity flags, etc.) exceed a threshold.
package stepup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"
)

// RiskLevel classifies how much verification a sensitive action needs.
type RiskLevel int

const (
	RiskLow RiskLevel = iota
	RiskMedium
	RiskHigh
)

func (l RiskLevel) String() string {
	switch l {
	case RiskLow:
		return "low"
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "high"
	default:
		return "unknown"
	}
}

// RequiresStepUp reports whether OTP/2FA must be completed before proceeding.
func (l RiskLevel) RequiresStepUp() bool {
	return l >= RiskMedium
}

// Signals feeds a [RiskAssessor] with context about the current request.
type Signals struct {
	AccountID       string
	DeviceFP        string
	Action          string
	KnownDevice     bool
	VelocityFlagged bool
}

// RiskAssessor computes risk for a sensitive action.
type RiskAssessor interface {
	Assess(ctx context.Context, signals Signals) (RiskLevel, error)
}

// DefaultAssessor applies compositional rules aligned with floodguard velocity flags.
type DefaultAssessor struct{}

// Assess implements [RiskAssessor].
func (DefaultAssessor) Assess(_ context.Context, s Signals) (RiskLevel, error) {
	switch {
	case !s.KnownDevice && s.VelocityFlagged:
		return RiskHigh, nil
	case !s.KnownDevice:
		return RiskMedium, nil
	case s.VelocityFlagged:
		return RiskMedium, nil
	default:
		return RiskLow, nil
	}
}

// ChallengeStore tracks issued and verified step-up tokens.
type ChallengeStore interface {
	Issue(ctx context.Context, accountID, action string, level RiskLevel, ttl time.Duration) (token string, err error)
	Verify(ctx context.Context, accountID, action, token string) (bool, error)
	Consume(ctx context.Context, accountID, action, token string) error
}

// ErrStepUpRequired is returned when verification is required.
var ErrStepUpRequired = errors.New("stepup: verification required")

// Manager coordinates risk assessment and challenge tokens.
type Manager struct {
	assessor RiskAssessor
	store    ChallengeStore
	ttl      time.Duration
}

// NewManager creates a Manager with default assessor and in-memory challenges.
func NewManager(assessor RiskAssessor, store ChallengeStore, opts ...ManagerOption) *Manager {
	if assessor == nil {
		assessor = DefaultAssessor{}
	}
	if store == nil {
		store = NewMemoryChallengeStore()
	}
	m := &Manager{assessor: assessor, store: store, ttl: 10 * time.Minute}
	for _, o := range opts {
		o(m)
	}
	return m
}

// ManagerOption configures a Manager.
type ManagerOption func(*Manager)

// WithTokenTTL sets challenge token lifetime.
func WithTokenTTL(ttl time.Duration) ManagerOption {
	return func(m *Manager) {
		if ttl > 0 {
			m.ttl = ttl
		}
	}
}

// Check evaluates risk and returns a challenge token when step-up is required.
// If headerToken is valid, consumption succeeds and nil is returned.
func (m *Manager) Check(ctx context.Context, signals Signals, headerToken string) (challengeToken string, level RiskLevel, err error) {
	level, err = m.assessor.Assess(ctx, signals)
	if err != nil {
		return "", level, err
	}
	if !level.RequiresStepUp() {
		return "", level, nil
	}
	if headerToken != "" {
		ok, err := m.store.Verify(ctx, signals.AccountID, signals.Action, headerToken)
		if err != nil {
			return "", level, err
		}
		if ok {
			_ = m.store.Consume(ctx, signals.AccountID, signals.Action, headerToken)
			return "", level, nil
		}
	}
	token, err := m.store.Issue(ctx, signals.AccountID, signals.Action, level, m.ttl)
	if err != nil {
		return "", level, err
	}
	return token, level, ErrStepUpRequired
}

// HeaderToken reads the step-up verification token from a request.
func HeaderToken(r *http.Request) string {
	return r.Header.Get("X-Step-Up-Token")
}

type memoryChallenge struct {
	level     RiskLevel
	expiresAt time.Time
}

type memoryChallengeStore struct {
	mu        sync.Mutex
	challenges map[string]memoryChallenge // key: account|action|token
}

// NewMemoryChallengeStore returns an in-memory [ChallengeStore].
func NewMemoryChallengeStore() ChallengeStore {
	return &memoryChallengeStore{challenges: make(map[string]memoryChallenge)}
}

func challengeKey(account, action, token string) string {
	return account + "|" + action + "|" + token
}

func (s *memoryChallengeStore) Issue(_ context.Context, accountID, action string, level RiskLevel, ttl time.Duration) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.challenges[challengeKey(accountID, action, token)] = memoryChallenge{
		level:     level,
		expiresAt: time.Now().Add(ttl),
	}
	return token, nil
}

func (s *memoryChallengeStore) Verify(_ context.Context, accountID, action, token string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.challenges[challengeKey(accountID, action, token)]
	if !ok || time.Now().After(ch.expiresAt) {
		return false, nil
	}
	return true, nil
}

func (s *memoryChallengeStore) Consume(_ context.Context, accountID, action, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.challenges, challengeKey(accountID, action, token))
	return nil
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

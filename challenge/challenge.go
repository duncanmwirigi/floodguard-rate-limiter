// Package challenge conditionally requires CAPTCHA/proof-of-work before
// sensitive or high-risk actions proceed.
package challenge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"
)

// ErrChallengeRequired is returned when CAPTCHA verification is needed.
var ErrChallengeRequired = errors.New("challenge: captcha required")

// RiskSignals feeds ChallengeRequired.
type RiskSignals struct {
	AccountAge        time.Duration // younger accounts are higher risk
	PlatformSpike     bool
	VelocityFlagged   bool
	NewAccountMaxAge  time.Duration // default 24h
}

// ChallengeRequired reports whether the client must complete CAPTCHA.
func ChallengeRequired(_ context.Context, s RiskSignals) bool {
	if s.NewAccountMaxAge <= 0 {
		s.NewAccountMaxAge = 24 * time.Hour
	}
	if s.PlatformSpike {
		return true
	}
	if s.VelocityFlagged {
		return true
	}
	// Age 0 means unknown/new account; mature accounts exceed NewAccountMaxAge.
	if s.AccountAge < s.NewAccountMaxAge {
		return true
	}
	return false
}

// Verifier validates a CAPTCHA response (stub for hCaptcha/Turnstile).
type Verifier interface {
	Verify(ctx context.Context, responseToken, remoteIP string) (bool, error)
}

// StubVerifier accepts token "valid-captcha" for tests.
type StubVerifier struct{}

func (StubVerifier) Verify(_ context.Context, token, _ string) (bool, error) {
	return token == "valid-captcha", nil
}

// TokenStore tracks issued challenge tokens until solved.
type TokenStore interface {
	Issue(ctx context.Context, accountID, action string, ttl time.Duration) (string, error)
	MarkSolved(ctx context.Context, accountID, action, token string) error
	IsSolved(ctx context.Context, accountID, action, token string) (bool, error)
}

type memoryToken struct {
	solved    bool
	expiresAt time.Time
}

type memoryTokenStore struct {
	mu     sync.Mutex
	tokens map[string]memoryToken
}

// NewMemoryTokenStore returns an in-memory TokenStore.
func NewMemoryTokenStore() TokenStore {
	return &memoryTokenStore{tokens: make(map[string]memoryToken)}
}

func tokenKey(account, action, token string) string {
	return account + "|" + action + "|" + token
}

func (s *memoryTokenStore) Issue(_ context.Context, accountID, action string, ttl time.Duration) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[tokenKey(accountID, action, token)] = memoryToken{
		expiresAt: time.Now().Add(ttl),
	}
	return token, nil
}

func (s *memoryTokenStore) MarkSolved(_ context.Context, accountID, action, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := tokenKey(accountID, action, token)
	t, ok := s.tokens[key]
	if !ok {
		return errors.New("challenge: unknown token")
	}
	t.solved = true
	s.tokens[key] = t
	return nil
}

func (s *memoryTokenStore) IsSolved(_ context.Context, accountID, action, token string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[tokenKey(accountID, action, token)]
	if !ok || time.Now().After(t.expiresAt) {
		return false, nil
	}
	return t.solved, nil
}

// Manager coordinates challenge issuance and verification.
type Manager struct {
	verifier Verifier
	store    TokenStore
	ttl      time.Duration
}

// NewManager creates a Manager.
func NewManager(verifier Verifier, store TokenStore, opts ...ManagerOption) *Manager {
	if verifier == nil {
		verifier = StubVerifier{}
	}
	if store == nil {
		store = NewMemoryTokenStore()
	}
	m := &Manager{verifier: verifier, store: store, ttl: 15 * time.Minute}
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

// Check evaluates signals. If challenge header token is solved, returns nil.
// Otherwise returns ErrChallengeRequired and a challenge token.
func (m *Manager) Check(ctx context.Context, accountID, action string, signals RiskSignals, headerToken, captchaResponse string) (challengeToken string, err error) {
	if !ChallengeRequired(ctx, signals) {
		return "", nil
	}
	if headerToken != "" {
		ok, err := m.store.IsSolved(ctx, accountID, action, headerToken)
		if err != nil {
			return "", err
		}
		if ok {
			return "", nil
		}
	}
	if captchaResponse != "" {
		valid, err := m.verifier.Verify(ctx, captchaResponse, "")
		if err != nil {
			return "", err
		}
		if valid && headerToken != "" {
			_ = m.store.MarkSolved(ctx, accountID, action, headerToken)
			return "", nil
		}
	}
	token, err := m.store.Issue(ctx, accountID, action, m.ttl)
	if err != nil {
		return "", err
	}
	return token, ErrChallengeRequired
}

// HeaderToken reads X-Challenge-Token from a request.
func HeaderToken(r *http.Request) string {
	return r.Header.Get("X-Challenge-Token")
}

// CaptchaResponse reads X-Captcha-Response from a request.
func CaptchaResponse(r *http.Request) string {
	return r.Header.Get("X-Captcha-Response")
}

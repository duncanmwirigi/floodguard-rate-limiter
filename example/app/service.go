package app

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/ultimateprogrammer/floodguard/anomaly"
	"github.com/ultimateprogrammer/floodguard/devicetrust"
	"github.com/ultimateprogrammer/floodguard/example/wallet"
	"github.com/ultimateprogrammer/floodguard/ledger"
	"github.com/ultimateprogrammer/floodguard/notify"
)

// Service holds demo account state and HTTP handlers.
type Service struct {
	mu       sync.Mutex
	accounts map[string]*wallet.Account
	ledger   *ledger.Ledger
	created  map[string]time.Time
	notifier *notify.Notifier
	anomaly  *anomaly.Detector
	devices  *devicetrust.Client
}

// NewService seeds demo accounts (balances in cents).
func NewService(seed map[string]int64, devices *devicetrust.Client, notifier *notify.Notifier, anomalyDet *anomaly.Detector) *Service {
	s := &Service{
		accounts: make(map[string]*wallet.Account),
		ledger:   ledger.New(ledger.NewMemoryStore()),
		created:  make(map[string]time.Time),
		notifier: notifier,
		anomaly:  anomalyDet,
		devices:  devices,
	}
	for id, bal := range seed {
		s.accounts[id] = wallet.NewAccount(bal)
		s.created[id] = time.Now().Add(-30 * 24 * time.Hour)
	}
	return s
}

// AccountAge returns how long the account has existed (for challenge signals).
// Unknown accounts return 0 so new-account CAPTCHA rules apply on first withdraw.
func (s *Service) AccountAge(accountID string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.created[accountID]; ok {
		return time.Since(t)
	}
	return 0
}

type withdrawRequest struct {
	AmountCents int64 `json:"amount_cents"`
}

// Withdraw handles POST /withdraw.
func (s *Service) Withdraw(w http.ResponseWriter, r *http.Request) {
	accountID := r.Header.Get("X-Account-ID")
	fp := devicetrust.FingerprintFromRequest(r, "")

	var req withdrawRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	_ = s.anomaly.Record(ctx, anomaly.MetricWithdrawalAttempts, 1)

	s.mu.Lock()
	acct, ok := s.accounts[accountID]
	if !ok {
		acct = wallet.NewAccount(0)
		s.accounts[accountID] = acct
		s.created[accountID] = time.Now()
	}
	s.mu.Unlock()

	if err := acct.Withdraw(ctx, s.ledger, req.AmountCents, accountID, "api"); err != nil {
		status := http.StatusPaymentRequired
		if err == wallet.ErrInvalidAmount || err == wallet.ErrOverflow {
			status = http.StatusBadRequest
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	known, _ := s.devices.IsKnownDevice(ctx, accountID, fp)
	if s.notifier != nil && !known {
		s.notifier.AfterSensitiveAction(ctx, accountID, "withdraw", fp, r.RemoteAddr)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":          "completed",
		"account_id":      accountID,
		"amount_cents":    req.AmountCents,
		"balance_cents":   acct.TotalCents(),
		"available_cents": acct.AvailableCents(),
	})
}

// TrustDevice marks the caller's device as trusted (demo helper for tests).
func (s *Service) TrustDevice(w http.ResponseWriter, r *http.Request) {
	accountID := r.Header.Get("X-Account-ID")
	fp := devicetrust.FingerprintFromRequest(r, "")
	if accountID == "" || fp == "" {
		http.Error(w, `{"error":"X-Account-ID and X-Device-ID required"}`, http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if _, ok := s.accounts[accountID]; !ok {
		s.accounts[accountID] = wallet.NewAccount(0)
		s.created[accountID] = time.Now()
	}
	s.mu.Unlock()

	if err := s.devices.MarkDeviceTrusted(r.Context(), accountID, fp); err != nil {
		http.Error(w, `{"error":"trust failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":      "trusted",
		"account_id":  accountID,
		"fingerprint": fp,
	})
}

// Balance returns current balance for a demo account.
func (s *Service) Balance(w http.ResponseWriter, r *http.Request) {
	accountID := r.Header.Get("X-Account-ID")
	if accountID == "" {
		http.Error(w, `{"error":"X-Account-ID required"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	acct, ok := s.accounts[accountID]
	s.mu.Unlock()
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account_id": accountID,
			"balance_cents": 0,
			"available_cents": 0,
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"account_id":      accountID,
		"balance_cents":   acct.TotalCents(),
		"available_cents": acct.AvailableCents(),
	})
}

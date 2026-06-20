// Package wallet implements balance operations in integer minor units (cents).
// All balance-affecting writes should go through ledger.RecordTransaction.
package wallet

import (
	"context"
	"errors"
	"math"
	"sync"

	"github.com/ultimateprogrammer/floodguard/ledger"
)

var (
	ErrInsufficientFunds = errors.New("wallet: insufficient funds")
	ErrInvalidAmount     = errors.New("wallet: amount must be positive")
	ErrOverflow          = errors.New("wallet: amount overflow")
)

// Account tracks total and available balance in minor units.
type Account struct {
	mu              sync.Mutex
	totalCents      int64
	pendingWithdraw int64 // reserved immediately on withdrawal request
}

// NewAccount creates an account with initial balance in cents.
func NewAccount(initialCents int64) *Account {
	return &Account{totalCents: initialCents}
}

// TotalCents returns total balance including pending holds.
func (a *Account) TotalCents() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.totalCents
}

// AvailableCents returns spendable balance (total minus pending withdrawals).
func (a *Account) AvailableCents() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.totalCents - a.pendingWithdraw
}

func validateAmount(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	if amount > math.MaxInt64/2 {
		return ErrOverflow
	}
	return nil
}

// Deposit adds funds and records in the append-only ledger.
func (a *Account) Deposit(ctx context.Context, lg *ledger.Ledger, amount int64, actorID, source string) error {
	if err := validateAmount(amount); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.totalCents > math.MaxInt64-amount {
		return ErrOverflow
	}
	if _, err := lg.RecordTransaction(ctx, "acct", amount, ledger.TxDeposit, actorID, source); err != nil {
		return err
	}
	a.totalCents += amount
	return nil
}

// Withdraw reserves funds immediately (reduces available) and records ledger entry.
func (a *Account) Withdraw(ctx context.Context, lg *ledger.Ledger, amount int64, actorID, source string) error {
	if err := validateAmount(amount); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	available := a.totalCents - a.pendingWithdraw
	if available < amount {
		return ErrInsufficientFunds
	}
	if a.totalCents < amount {
		return ErrInsufficientFunds
	}

	a.pendingWithdraw += amount
	defer func() { a.pendingWithdraw -= amount }()

	if _, err := lg.RecordTransaction(ctx, "acct", -amount, ledger.TxWithdrawal, actorID, source); err != nil {
		return err
	}
	a.totalCents -= amount
	return nil
}

// SettleBet applies a bet outcome delta (positive win, negative loss).
func (a *Account) SettleBet(ctx context.Context, lg *ledger.Ledger, delta int64, actorID, source string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if delta > 0 {
		if a.totalCents > math.MaxInt64-delta {
			return ErrOverflow
		}
	} else if delta < 0 {
		loss := -delta
		if a.totalCents < loss {
			return ErrInsufficientFunds
		}
	}

	if _, err := lg.RecordTransaction(ctx, "acct", delta, ledger.TxBetSettle, actorID, source); err != nil {
		return err
	}

	if delta > 0 {
		a.totalCents += delta
	} else if delta < 0 {
		a.totalCents -= -delta
	}
	return nil
}

// Op is one operation in a property-test sequence.
type Op struct {
	Kind   string // deposit, withdraw, settle
	Amount int64  // for deposit/withdraw; for settle this is delta
}

// Apply runs one operation on account + ledger.
func Apply(ctx context.Context, acct *Account, lg *ledger.Ledger, op Op) error {
	switch op.Kind {
	case "deposit":
		return acct.Deposit(ctx, lg, op.Amount, "test", "property")
	case "withdraw":
		return acct.Withdraw(ctx, lg, op.Amount, "test", "property")
	case "settle":
		return acct.SettleBet(ctx, lg, op.Amount, "test", "property")
	default:
		return nil
	}
}

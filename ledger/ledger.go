// Package ledger provides an append-only, hash-chained transaction log for
// balance-affecting operations. Tampering with any record breaks the chain.
package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrKeyRequired is returned when required fields are missing.
var ErrKeyRequired = errors.New("ledger: account id is required")

// TxType identifies the kind of balance movement.
type TxType string

const (
	TxDeposit    TxType = "deposit"
	TxWithdrawal TxType = "withdrawal"
	TxBetSettle  TxType = "bet_settlement"
)

// Record is one append-only ledger entry.
type Record struct {
	ID         int64
	AccountID  string
	Amount     int64 // minor units (cents)
	TxType     TxType
	ActorID    string
	Source     string
	PrevHash   string
	RecordHash string
	CreatedAt  time.Time
}

// Store appends hash-chained records. Implementations must not allow UPDATE/DELETE.
type Store interface {
	Append(ctx context.Context, rec Record) (Record, error)
	ListByAccount(ctx context.Context, accountID string) ([]Record, error)
	// SetRecordHashForTest mutates hash at index for tamper-detection tests only.
	SetRecordHashForTest(ctx context.Context, accountID string, index int, hash string) error
}

// Ledger is the single entry point for recording balance-affecting writes.
type Ledger struct {
	store Store
}

// New creates a Ledger.
func New(store Store) *Ledger {
	if store == nil {
		store = NewMemoryStore()
	}
	return &Ledger{store: store}
}

// RecordTransaction appends one hash-linked transaction for accountID.
func (l *Ledger) RecordTransaction(ctx context.Context, accountID string, amount int64, txType TxType, actorID, source string) (Record, error) {
	if accountID == "" {
		return Record{}, ErrKeyRequired
	}

	existing, err := l.store.ListByAccount(ctx, accountID)
	if err != nil {
		return Record{}, err
	}

	prevHash := genesisHash()
	if len(existing) > 0 {
		prevHash = existing[len(existing)-1].RecordHash
	}

	rec := Record{
		ID:        int64(len(existing) + 1),
		AccountID: accountID,
		Amount:    amount,
		TxType:    txType,
		ActorID:   actorID,
		Source:    source,
		PrevHash:  prevHash,
		CreatedAt: time.Now().UTC(),
	}
	rec.RecordHash = hashRecord(rec)
	return l.store.Append(ctx, rec)
}

func genesisHash() string {
	sum := sha256.Sum256([]byte("floodguard-ledger-genesis"))
	return hex.EncodeToString(sum[:])
}

func hashRecord(r Record) string {
	payload := strings.Join([]string{
		r.PrevHash,
		r.AccountID,
		strconv.FormatInt(r.Amount, 10),
		string(r.TxType),
		r.ActorID,
		r.Source,
		r.CreatedAt.Format(time.RFC3339Nano),
	}, "|")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// VerifyChainIntegrity walks the chain for accountID and detects tampering.
func (l *Ledger) VerifyChainIntegrity(ctx context.Context, accountID string) (ok bool, brokenAt int, err error) {
	recs, err := l.store.ListByAccount(ctx, accountID)
	if err != nil {
		return false, 0, err
	}
	if len(recs) == 0 {
		return true, 0, nil
	}

	expectedPrev := genesisHash()
	for i, rec := range recs {
		if rec.PrevHash != expectedPrev {
			return false, i + 1, fmt.Errorf("prev_hash mismatch at record %d", i+1)
		}
		if rec.RecordHash != hashRecord(rec) {
			return false, i + 1, fmt.Errorf("record_hash mismatch at record %d", i+1)
		}
		expectedPrev = rec.RecordHash
	}
	return true, 0, nil
}

// Balance returns the sum of ledger amounts for accountID (for tests and reconciliation).
func (l *Ledger) Balance(ctx context.Context, accountID string) (int64, error) {
	recs, err := l.store.ListByAccount(ctx, accountID)
	if err != nil {
		return 0, err
	}
	var sum int64
	for _, r := range recs {
		sum += r.Amount
	}
	return sum, nil
}

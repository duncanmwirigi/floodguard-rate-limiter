package ledger_test

import (
	"context"
	"testing"

	"github.com/ultimateprogrammer/floodguard/ledger"
)

func TestRecordTransaction_Chain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	lg := ledger.New(ledger.NewMemoryStore())

	if _, err := lg.RecordTransaction(ctx, "a1", 1000, ledger.TxDeposit, "system", "api"); err != nil {
		t.Fatal(err)
	}
	if _, err := lg.RecordTransaction(ctx, "a1", -100, ledger.TxWithdrawal, "user", "api"); err != nil {
		t.Fatal(err)
	}

	ok, broken, err := lg.VerifyChainIntegrity(ctx, "a1")
	if err != nil || !ok || broken != 0 {
		t.Fatalf("integrity ok=%v broken=%d err=%v", ok, broken, err)
	}

	bal, err := lg.Balance(ctx, "a1")
	if err != nil || bal != 900 {
		t.Fatalf("balance=%d err=%v", bal, err)
	}
}

func TestVerifyChainIntegrity_DetectsTamper(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := ledger.NewMemoryStore()
	lg := ledger.New(store)

	for i := 0; i < 5; i++ {
		if _, err := lg.RecordTransaction(ctx, "a1", 100, ledger.TxDeposit, "sys", "test"); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.SetRecordHashForTest(ctx, "a1", 2, "tampered"); err != nil {
		t.Fatal(err)
	}

	ok, brokenAt, err := lg.VerifyChainIntegrity(ctx, "a1")
	if err == nil {
		t.Fatal("expected integrity error")
	}
	if ok || brokenAt != 3 {
		t.Fatalf("ok=%v brokenAt=%d err=%v", ok, brokenAt, err)
	}
}

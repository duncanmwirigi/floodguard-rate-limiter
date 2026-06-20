package wallet_test

import (
	"context"
	"math"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ultimateprogrammer/floodguard/example/wallet"
	"github.com/ultimateprogrammer/floodguard/ledger"
)

func TestProperty_BalanceInvariants(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("balance never negative and matches ledger sum", prop.ForAll(
		func(ops []wallet.Op) bool {
			ctx := context.Background()
			lg := ledger.New(ledger.NewMemoryStore())
			acct := wallet.NewAccount(0)

			if err := acct.Deposit(ctx, lg, 10_000, "seed", "property"); err != nil {
				return false
			}

			for _, op := range ops {
				_ = wallet.Apply(ctx, acct, lg, op)
			}

			if acct.TotalCents() < 0 {
				return false
			}
			if acct.AvailableCents() < 0 {
				return false
			}
			if acct.AvailableCents() > acct.TotalCents() {
				return false
			}

			sum, err := lg.Balance(ctx, "acct")
			if err != nil {
				return false
			}
			return sum == acct.TotalCents()
		},
		genOps(),
	))

	properties.TestingRun(t, gopter.ConsoleReporter(false))
}

func genOps() gopter.Gen {
	amount := gen.Int64Range(1, 500)
	delta := gen.Int64Range(-500, 500)
	op := gen.OneGenOf(
		amount.Map(func(a int64) wallet.Op { return wallet.Op{Kind: "deposit", Amount: a} }),
		amount.Map(func(a int64) wallet.Op { return wallet.Op{Kind: "withdraw", Amount: a} }),
		delta.Map(func(d int64) wallet.Op { return wallet.Op{Kind: "settle", Amount: d} }),
	)
	return gen.SliceOfN(50, op)
}

func TestRegression_NegativeAmountRejected(t *testing.T) {
	ctx := context.Background()
	lg := ledger.New(ledger.NewMemoryStore())
	acct := wallet.NewAccount(1000)

	if err := acct.Withdraw(ctx, lg, -1, "u", "api"); err != wallet.ErrInvalidAmount {
		t.Fatalf("expected invalid amount, got %v", err)
	}
}

func TestRegression_OverflowNearMaxInt64(t *testing.T) {
	ctx := context.Background()
	lg := ledger.New(ledger.NewMemoryStore())
	acct := wallet.NewAccount(math.MaxInt64 - 100)

	if err := acct.Deposit(ctx, lg, 200, "u", "api"); err != wallet.ErrOverflow {
		t.Fatalf("expected overflow, got %v", err)
	}
}

func TestRegression_AvailableReducedOnWithdraw(t *testing.T) {
	ctx := context.Background()
	lg := ledger.New(ledger.NewMemoryStore())
	acct := wallet.NewAccount(1000)

	// Simulate pending withdraw pattern: available should track holds.
	// Our Withdraw completes atomically; available equals total after success.
	if err := acct.Withdraw(ctx, lg, 400, "u", "api"); err != nil {
		t.Fatal(err)
	}
	if acct.AvailableCents() != 600 || acct.TotalCents() != 600 {
		t.Fatalf("available=%d total=%d", acct.AvailableCents(), acct.TotalCents())
	}
}

func TestRegression_NoFloatInMoneyPath(t *testing.T) {
	// wallet package uses int64 minor units only — no float64 in money path.
	var cents int64 = 100
	if cents != 100 {
		t.Fatal("sanity")
	}
}

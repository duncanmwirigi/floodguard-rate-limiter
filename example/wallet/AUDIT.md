# Withdrawal handler audit (Gap 3)

Findings from auditing the original `example/main.go` withdrawal handler before migration to `example/wallet`.

## Findings

### 1. Integer overflow / underflow — `example/main.go` (legacy)

**Issue:** Balance and amount used `int` (platform-dependent width) with no overflow checks. A malicious client could pass amounts near `MaxInt` or negative values that pass JSON decode but fail only a `<= 0` check.

**Fix:** Use `int64` minor units (`amount_cents`), reject `amount <= 0`, and check `totalCents > MaxInt64-amount` before deposit. See `example/wallet/wallet.go`.

**Regression tests:** `TestRegression_NegativeAmountRejected`, `TestRegression_OverflowNearMaxInt64`

### 2. Currency unit mismatch — `example/main.go` (legacy)

**Issue:** Request field was `amount` (ambiguous whole units vs cents) while seed balances used bare integers (`500` meaning unknown unit).

**Fix:** Rename to `amount_cents` and document KES minor units everywhere. Seed balances as `50_000` cents (KES 500.00).

**Regression test:** API contract in updated `example/main.go` uses `amount_cents int64`.

### 3. Floating point in money — none found

**Issue:** No `float64` in money path in legacy handler (good).

**Note:** `example/wallet` enforces `int64` only. Property tests document this invariant.

### 4. Available vs total balance — `example/main.go` (legacy)

**Issue:** Single `balance` field; pending withdrawals did not reduce available balance until settlement completed.

**Fix:** `wallet.Account` tracks `pendingWithdraw` and exposes `AvailableCents()` separately from `TotalCents()`. Withdraw reserves immediately.

**Regression test:** `TestRegression_AvailableReducedOnWithdraw`

## Property-based tests

`TestProperty_BalanceInvariants` runs thousands of random deposit/withdraw/settle sequences and asserts:

- Balance never goes negative
- Available never exceeds total
- Sum of ledger entries equals current balance exactly

Run: `go test -run TestProperty ./example/wallet/...`

## What tests do not replace

Automated tests catch what you thought to test for. A second engineer reviewing withdrawal logic catches what nobody thought of. Required before real money.

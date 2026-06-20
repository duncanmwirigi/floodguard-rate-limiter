package devicetrust_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/duncanmwirigi/floodguard-rate-limiter/devicetrust"
)

func TestFingerprintStable(t *testing.T) {
	t.Parallel()
	fp1 := devicetrust.Fingerprint("Mozilla/5.0", "en-US", "device-abc")
	fp2 := devicetrust.Fingerprint("Mozilla/5.0", "en-US", "device-abc")
	if fp1 != fp2 {
		t.Fatalf("fingerprints differ: %q vs %q", fp1, fp2)
	}
	if fp1 == devicetrust.Fingerprint("Other", "en-US", "device-abc") {
		t.Fatal("expected different fingerprint for different UA")
	}
}

func TestFingerprintFromRequest(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "TestAgent/1.0")
	req.Header.Set("Accept-Language", "en-GB")
	req.Header.Set("X-Device-ID", "dev-1")

	fp := devicetrust.FingerprintFromRequest(req, "")
	if fp != devicetrust.Fingerprint("TestAgent/1.0", "en-GB", "dev-1") {
		t.Fatal("request fingerprint mismatch")
	}
}

func TestKnownUnknownDevice(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := devicetrust.New(devicetrust.NewMemoryStore())

	const (
		account = "acct-1"
		fp      = "fp-known"
	)

	known, err := client.IsKnownDevice(ctx, account, fp)
	if err != nil || known {
		t.Fatalf("new device should be unknown: known=%v err=%v", known, err)
	}

	if err := client.RecordSeen(ctx, account, fp); err != nil {
		t.Fatal(err)
	}
	known, err = client.IsKnownDevice(ctx, account, fp)
	if err != nil || known {
		t.Fatal("seen but untrusted device should still be unknown")
	}

	if err := client.MarkDeviceTrusted(ctx, account, fp); err != nil {
		t.Fatal(err)
	}
	known, err = client.IsKnownDevice(ctx, account, fp)
	if err != nil || !known {
		t.Fatalf("trusted device should be known: known=%v err=%v", known, err)
	}
}

func TestNewDeviceFlaggedOnExistingAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := devicetrust.New(devicetrust.NewMemoryStore())
	account := "acct-existing"

	if err := client.MarkDeviceTrusted(ctx, account, "old-device"); err != nil {
		t.Fatal(err)
	}

	newFP := devicetrust.Fingerprint("UA", "en", "new-device-id")
	known, err := client.IsKnownDevice(ctx, account, newFP)
	if err != nil || known {
		t.Fatal("new device on existing account must be flagged unknown")
	}
}

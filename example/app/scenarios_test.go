package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/ultimateprogrammer/floodguard/config"
	"github.com/ultimateprogrammer/floodguard/example/app"
)

func testConfig(prefix string) config.Config {
	cfg := config.Defaults()
	cfg.Redis.KeyPrefix = prefix
	cfg.Velocity.MinInterval = time.Millisecond
	cfg.Demo.AccountBalances = map[string]int64{
		"acct-1001": 50_000,
		"acct-1002": 10_000,
	}
	return cfg
}

func newTestServer(t *testing.T) (*httptest.Server, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cfg := testConfig("test-" + t.Name())
	srv, err := app.New(cfg, client)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(srv.Mux), mr
}

func TestScenario_Health(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestScenario_TrustAndWithdraw(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	trustReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/demo/trust-device", nil)
	trustReq.Header.Set("X-Account-ID", "acct-1001")
	trustReq.Header.Set("X-Device-ID", "device-test")
	trustReq.Header.Set("User-Agent", "Test/1.0")
	trustReq.Header.Set("Accept-Language", "en")

	resp, err := http.DefaultClient.Do(trustReq)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("trust status=%d", resp.StatusCode)
	}

	body, _ := json.Marshal(map[string]int64{"amount_cents": 1000})
	wdReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/withdraw", bytes.NewReader(body))
	wdReq.Header.Set("Content-Type", "application/json")
	wdReq.Header.Set("X-Account-ID", "acct-1001")
	wdReq.Header.Set("X-Device-ID", "device-test")
	wdReq.Header.Set("User-Agent", "Test/1.0")
	wdReq.Header.Set("Accept-Language", "en")
	wdReq.Header.Set("Idempotency-Key", "test-withdraw-1")

	resp, err = http.DefaultClient.Do(wdReq)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("withdraw status=%d body=%s", resp.StatusCode, b)
	}
}

func TestScenario_IdempotentReplay(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	// Trust device first
	trustReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/demo/trust-device", nil)
	trustReq.Header.Set("X-Account-ID", "acct-1001")
	trustReq.Header.Set("X-Device-ID", "device-idem")
	trustReq.Header.Set("User-Agent", "Test/1.0")
	trustReq.Header.Set("Accept-Language", "en")
	resp, _ := http.DefaultClient.Do(trustReq)
	resp.Body.Close()

	body, _ := json.Marshal(map[string]int64{"amount_cents": 500})
	do := func() *http.Response {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/withdraw", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Account-ID", "acct-1001")
		req.Header.Set("X-Device-ID", "device-idem")
		req.Header.Set("User-Agent", "Test/1.0")
		req.Header.Set("Accept-Language", "en")
		req.Header.Set("Idempotency-Key", "idem-key-1")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	r1 := do()
	r1.Body.Close()
	if r1.StatusCode != http.StatusOK {
		t.Fatalf("first status=%d", r1.StatusCode)
	}

	r2 := do()
	defer r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("replay status=%d", r2.StatusCode)
	}
	if r2.Header.Get("X-Idempotent-Replay") != "true" {
		t.Fatal("expected X-Idempotent-Replay header")
	}
}

func TestScenario_StepUpUnknownDevice(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]int64{"amount_cents": 100})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/withdraw", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Account-ID", "acct-1001")
	req.Header.Set("X-Device-ID", "unknown-device")
	req.Header.Set("User-Agent", "Test/1.0")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Idempotency-Key", "stepup-1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d want 403", resp.StatusCode)
	}
	var out map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["challenge_token"] == "" {
		t.Fatal("expected challenge_token")
	}
}

func TestScenario_InsufficientFunds(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	trustReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/demo/trust-device", nil)
	trustReq.Header.Set("X-Account-ID", "acct-1002")
	trustReq.Header.Set("X-Device-ID", "d1")
	trustReq.Header.Set("User-Agent", "Test/1.0")
	trustReq.Header.Set("Accept-Language", "en")
	resp, _ := http.DefaultClient.Do(trustReq)
	resp.Body.Close()

	body, _ := json.Marshal(map[string]int64{"amount_cents": 99_999_999})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/withdraw", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Account-ID", "acct-1002")
	req.Header.Set("X-Device-ID", "d1")
	req.Header.Set("User-Agent", "Test/1.0")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Idempotency-Key", "insuf-1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("status=%d want 402", resp.StatusCode)
	}
}

func TestScenario_CaptchaNewAccount(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	accountID := "acct-brand-new"
	deviceID := "device-new"

	trustReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/demo/trust-device", nil)
	trustReq.Header.Set("X-Account-ID", accountID)
	trustReq.Header.Set("X-Device-ID", deviceID)
	trustReq.Header.Set("User-Agent", "Test/1.0")
	trustReq.Header.Set("Accept-Language", "en")
	resp, _ := http.DefaultClient.Do(trustReq)
	resp.Body.Close()

	body, _ := json.Marshal(map[string]int64{"amount_cents": 100})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/withdraw", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Account-ID", accountID)
	req.Header.Set("X-Device-ID", deviceID)
	req.Header.Set("User-Agent", "Test/1.0")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Idempotency-Key", "captcha-1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d want 403", resp.StatusCode)
	}
	if resp.Header.Get("X-Challenge-Required") != "true" {
		t.Fatal("expected X-Challenge-Required")
	}
}

// Ensure Redis ping works in test setup.
func TestScenario_RedisPing(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
}

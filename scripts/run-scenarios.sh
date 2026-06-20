#!/usr/bin/env bash
# Run integration scenarios against a running floodguard example server.
#
# Usage:
#   Terminal 1: redis-server && set -a && source .env && set +a && go run ./example
#   Terminal 2: ./scripts/run-scenarios.sh
#
# Options:
#   BASE_URL=http://localhost:8080 ./scripts/run-scenarios.sh

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
RUN_ID="${RUN_ID:-$(date +%s)}"
PASS=0
FAIL=0

UA="FloodguardTest/1.0"
LANG="en-US"

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing required command: $1"; exit 1; }
}

need curl
need jq

assert_status() {
  local name="$1" want="$2" got="$3" body="$4"
  if [[ "$got" == "$want" ]]; then
    echo "  PASS  $name (HTTP $got)"
    PASS=$((PASS + 1))
  else
    echo "  FAIL  $name (want HTTP $want, got $got)"
    echo "        body: $body"
    FAIL=$((FAIL + 1))
  fi
}

trust_device() {
  local account="$1" device="$2"
  curl -s -o /dev/null -X POST "$BASE_URL/demo/trust-device" \
    -H "X-Account-ID: $account" \
    -H "X-Device-ID: $device" \
    -H "User-Agent: $UA" \
    -H "Accept-Language: $LANG"
}

# Avoid velocity MinInterval collisions between back-to-back withdraws.
pause_velocity() {
  sleep 0.3
}

echo "==> floodguard scenario runner"
echo "    base_url=$BASE_URL run_id=$RUN_ID"
echo

# 01 Health
code=$(curl -s -o /tmp/fg-health.json -w "%{http_code}" "$BASE_URL/health")
assert_status "01-health" 200 "$code" "$(cat /tmp/fg-health.json)"

# 02 Trust device
code=$(curl -s -o /tmp/fg-trust.json -w "%{http_code}" -X POST "$BASE_URL/demo/trust-device" \
  -H "X-Account-ID: acct-1001" \
  -H "X-Device-ID: device-trusted-1001" \
  -H "User-Agent: $UA" \
  -H "Accept-Language: $LANG")
assert_status "02-trust-device" 200 "$code" "$(cat /tmp/fg-trust.json)"

# 03 Successful withdraw
pause_velocity
IDEM="scenario-03-$RUN_ID"
code=$(curl -s -o /tmp/fg-wd.json -w "%{http_code}" -X POST "$BASE_URL/withdraw" \
  -H "X-Account-ID: acct-1001" \
  -H "X-Device-ID: device-trusted-1001" \
  -H "User-Agent: $UA" \
  -H "Accept-Language: $LANG" \
  -H "Idempotency-Key: $IDEM" \
  -H "Content-Type: application/json" \
  -d '{"amount_cents":1000}')
assert_status "03-withdraw-success" 200 "$code" "$(cat /tmp/fg-wd.json)"

# 04 Idempotent replay (trusted device, pause after prior withdraw)
pause_velocity
IDEM="scenario-04-$RUN_ID"
curl -s -o /dev/null -X POST "$BASE_URL/withdraw" \
  -H "X-Account-ID: acct-1001" \
  -H "X-Device-ID: device-trusted-1001" \
  -H "User-Agent: $UA" \
  -H "Accept-Language: $LANG" \
  -H "Idempotency-Key: $IDEM" \
  -H "Content-Type: application/json" \
  -d '{"amount_cents":500}'
replay_code=$(curl -s -D /tmp/fg-headers.txt -o /tmp/fg-replay.json -w "%{http_code}" -X POST "$BASE_URL/withdraw" \
  -H "X-Account-ID: acct-1001" \
  -H "X-Device-ID: device-trusted-1001" \
  -H "User-Agent: $UA" \
  -H "Accept-Language: $LANG" \
  -H "Idempotency-Key: $IDEM" \
  -H "Content-Type: application/json" \
  -d '{"amount_cents":500}')
if [[ "$replay_code" == "200" ]] && grep -qi "X-Idempotent-Replay: true" /tmp/fg-headers.txt; then
  echo "  PASS  04-idempotent-replay (HTTP 200 + X-Idempotent-Replay)"
  PASS=$((PASS + 1))
else
  echo "  FAIL  04-idempotent-replay (code=$replay_code body=$(cat /tmp/fg-replay.json))"
  FAIL=$((FAIL + 1))
fi

# 05 Insufficient funds
trust_device "acct-1002" "device-trusted-1002"
pause_velocity
code=$(curl -s -o /tmp/fg-insuf.json -w "%{http_code}" -X POST "$BASE_URL/withdraw" \
  -H "X-Account-ID: acct-1002" \
  -H "X-Device-ID: device-trusted-1002" \
  -H "User-Agent: $UA" \
  -H "Accept-Language: $LANG" \
  -H "Idempotency-Key: scenario-05-$RUN_ID" \
  -H "Content-Type: application/json" \
  -d '{"amount_cents":99999999}')
assert_status "05-insufficient-funds" 402 "$code" "$(cat /tmp/fg-insuf.json)"

# 06 Invalid amount (mature account + trusted device — reaches handler validation)
pause_velocity
code=$(curl -s -o /tmp/fg-inv.json -w "%{http_code}" -X POST "$BASE_URL/withdraw" \
  -H "X-Account-ID: acct-1001" \
  -H "X-Device-ID: device-trusted-1001" \
  -H "User-Agent: $UA" \
  -H "Accept-Language: $LANG" \
  -H "Idempotency-Key: scenario-06-$RUN_ID" \
  -H "Content-Type: application/json" \
  -d '{"amount_cents":-1}')
assert_status "06-invalid-amount" 400 "$code" "$(cat /tmp/fg-inv.json)"

# 07 Missing idempotency key
code=$(curl -s -o /tmp/fg-noidem.json -w "%{http_code}" -X POST "$BASE_URL/withdraw" \
  -H "X-Account-ID: acct-1001" \
  -H "X-Device-ID: device-trusted-1001" \
  -H "User-Agent: $UA" \
  -H "Accept-Language: en-US" \
  -H "Content-Type: application/json" \
  -d '{"amount_cents":100}')
assert_status "07-missing-idempotency" 400 "$code" "$(cat /tmp/fg-noidem.json)"

# 08 Step-up unknown device (mature acct-1002, minimal velocity history)
pause_velocity
code=$(curl -s -o /tmp/fg-stepup.json -w "%{http_code}" -X POST "$BASE_URL/withdraw" \
  -H "X-Account-ID: acct-1002" \
  -H "X-Device-ID: device-unknown" \
  -H "User-Agent: $UA" \
  -H "Accept-Language: $LANG" \
  -H "Idempotency-Key: scenario-08-$RUN_ID" \
  -H "Content-Type: application/json" \
  -d '{"amount_cents":100}')
token=$(jq -r '.challenge_token // empty' /tmp/fg-stepup.json 2>/dev/null || true)
if [[ "$code" == "403" && -n "$token" ]]; then
  echo "  PASS  08-step-up-unknown-device (HTTP 403 + challenge_token)"
  PASS=$((PASS + 1))
else
  echo "  FAIL  08-step-up-unknown-device (code=$code token=$token body=$(cat /tmp/fg-stepup.json))"
  FAIL=$((FAIL + 1))
fi

# 08b Step-up with token
if [[ -n "${token:-}" ]]; then
  pause_velocity
  code=$(curl -s -o /tmp/fg-stepup2.json -w "%{http_code}" -X POST "$BASE_URL/withdraw" \
    -H "X-Account-ID: acct-1002" \
    -H "X-Device-ID: device-unknown" \
    -H "User-Agent: $UA" \
    -H "Accept-Language: $LANG" \
    -H "Idempotency-Key: scenario-08b-$RUN_ID" \
    -H "X-Step-Up-Token: $token" \
    -H "Content-Type: application/json" \
    -d '{"amount_cents":100}')
  # Mature account with funds — 200 means step-up passed and withdraw succeeded
  if [[ "$code" == "200" || "$code" == "402" ]]; then
    echo "  PASS  08b-step-up-with-token (passed step-up, HTTP $code)"
    PASS=$((PASS + 1))
  else
    echo "  FAIL  08b-step-up-with-token (HTTP $code body=$(cat /tmp/fg-stepup2.json))"
    FAIL=$((FAIL + 1))
  fi
fi

# 09 CAPTCHA new account (trust device first so step-up does not fire)
NEW_ACCT="acct-scenario-new-$RUN_ID"
trust_device "$NEW_ACCT" "device-trusted-captcha"
pause_velocity
cap_code=$(curl -s -D /tmp/fg-cap-hdr.txt -o /tmp/fg-cap.json -w "%{http_code}" -X POST "$BASE_URL/withdraw" \
  -H "X-Account-ID: $NEW_ACCT" \
  -H "X-Device-ID: device-trusted-captcha" \
  -H "User-Agent: $UA" \
  -H "Accept-Language: $LANG" \
  -H "Idempotency-Key: scenario-09-$RUN_ID" \
  -H "Content-Type: application/json" \
  -d '{"amount_cents":100}')
cap_token=$(jq -r '.challenge_token // empty' /tmp/fg-cap.json 2>/dev/null || true)
if [[ "$cap_code" == "403" ]] && grep -qi "X-Challenge-Required: true" /tmp/fg-cap-hdr.txt; then
  echo "  PASS  09-captcha-new-account (HTTP 403 + X-Challenge-Required)"
  PASS=$((PASS + 1))
else
  echo "  FAIL  09-captcha-new-account (code=$cap_code body=$(cat /tmp/fg-cap.json))"
  FAIL=$((FAIL + 1))
fi

# 09b Solve captcha (stub token) — pause avoids velocity→step-up on retry
if [[ -n "${cap_token:-}" ]]; then
  pause_velocity
  code=$(curl -s -o /tmp/fg-cap2.json -w "%{http_code}" -X POST "$BASE_URL/withdraw" \
    -H "X-Account-ID: $NEW_ACCT" \
    -H "X-Device-ID: device-trusted-captcha" \
    -H "User-Agent: $UA" \
    -H "Accept-Language: $LANG" \
    -H "Idempotency-Key: scenario-09b-$RUN_ID" \
    -H "X-Challenge-Token: $cap_token" \
    -H "X-Captcha-Response: valid-captcha" \
    -H "Content-Type: application/json" \
    -d '{"amount_cents":100}')
  if [[ "$code" == "402" || "$code" == "200" ]]; then
    echo "  PASS  09b-captcha-solved (passed challenge layer, HTTP $code)"
    PASS=$((PASS + 1))
  else
    echo "  FAIL  09b-captcha-solved (HTTP $code body=$(cat /tmp/fg-cap2.json))"
    FAIL=$((FAIL + 1))
  fi
fi

# 10 Balance
code=$(curl -s -o /tmp/fg-bal.json -w "%{http_code}" "$BASE_URL/demo/balance" \
  -H "X-Account-ID: acct-1001")
if [[ "$code" == "200" ]] && jq -e '.balance_cents' /tmp/fg-bal.json >/dev/null 2>&1; then
  echo "  PASS  10-balance-check (HTTP 200 + balance_cents)"
  PASS=$((PASS + 1))
else
  echo "  FAIL  10-balance-check"
  FAIL=$((FAIL + 1))
fi

echo
echo "==> Results: $PASS passed, $FAIL failed"
[[ "$FAIL" -eq 0 ]]

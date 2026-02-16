#!/bin/bash
# test-rpc-integration.sh - Comprehensive RPC integration test for 4-node testnet
#
# Tests: input validation, token metadata validation, root chain send,
#        token creation, token send, sub-chain send, PoA sub-chain creation,
#        multi-node sync, wallet history, and edge cases.
#
# Prerequisites:
#   - 4-node testnet running (./scripts/start-testnet.sh)
#   - Chain height >= 25 (coinbase maturity)
#   - Wallet "validator" imported with password "test"
#
# Usage:
#   ./scripts/test-rpc-integration.sh
#   ./scripts/test-rpc-integration.sh --rpc http://127.0.0.1:8701  # custom RPC

set -e

RPC="${RPC:-http://127.0.0.1:8701}"
RPC2="${RPC2:-http://127.0.0.1:8702}"
RPC3="${RPC3:-http://127.0.0.1:8703}"
RPC4="${RPC4:-http://127.0.0.1:8704}"
WALLET="${WALLET:-validator}"
PASS="${PASS:-test}"

# Parse --rpc flag
while [[ $# -gt 0 ]]; do
  case "$1" in
    --rpc) RPC="$2"; shift 2 ;;
    --rpc2) RPC2="$2"; shift 2 ;;
    *) shift ;;
  esac
done

PASS_FAIL=0
PASS_OK=0
TESTS_RUN=0

# ── Helpers ──────────────────────────────────────────────────────────

rpc() {
  local url="$1" method="$2" params="$3"
  if [[ "$params" == "null" ]] || [[ -z "$params" ]]; then
    curl -sf -X POST "$url" -H "Content-Type: application/json" \
      -d "{\"jsonrpc\":\"2.0\",\"method\":\"$method\",\"id\":1}" 2>/dev/null || echo '{"error":{"code":-1,"message":"connection refused"}}'
  else
    curl -sf -X POST "$url" -H "Content-Type: application/json" \
      -d "{\"jsonrpc\":\"2.0\",\"method\":\"$method\",\"params\":$params,\"id\":1}" 2>/dev/null || echo '{"error":{"code":-1,"message":"connection refused"}}'
  fi
}

pass() { echo "  PASS  $1"; PASS_OK=$((PASS_OK+1)); TESTS_RUN=$((TESTS_RUN+1)); }
fail() { echo "  FAIL  $1"; PASS_FAIL=$((PASS_FAIL+1)); TESTS_RUN=$((TESTS_RUN+1)); }

expect_error() {
  local resp="$1" test_name="$2" expected_msg="${3:-}"
  if echo "$resp" | grep -q '"error"'; then
    if [[ -n "$expected_msg" ]] && ! echo "$resp" | grep -qi "$expected_msg"; then
      fail "$test_name (wrong error: $(echo "$resp" | grep -o '"message":"[^"]*"' | head -1))"
    else
      pass "$test_name"
    fi
  else
    fail "$test_name (expected error, got success)"
  fi
}

expect_success() {
  local resp="$1" test_name="$2"
  if echo "$resp" | grep -q '"result"' && ! echo "$resp" | grep -q '"error"'; then
    pass "$test_name"
  else
    local msg=$(echo "$resp" | grep -o '"message":"[^"]*"' | head -1 | cut -d'"' -f4)
    fail "$test_name (${msg:-unknown error})"
  fi
}

get_height() {
  rpc "$1" chain_getInfo | grep -o '"height":[0-9]*' | cut -d: -f2
}

wait_blocks() {
  local n="$1"
  local start=$(get_height "$RPC")
  local target=$((start + n))
  echo "  ...waiting $n blocks (height $start -> $target)..."
  for _ in $(seq 1 $((n * 4 + 10))); do
    local h=$(get_height "$RPC")
    if [[ "$h" -ge "$target" ]]; then return 0; fi
    sleep 1
  done
  echo "  ...timed out waiting for blocks"
}

wait_mempool_clear() {
  echo "  ...waiting for mempool to clear..."
  for _ in $(seq 1 30); do
    local c=$(rpc "$RPC" mempool_getInfo | grep -o '"count":[0-9]*' | cut -d: -f2)
    if [[ "$c" == "0" ]]; then return 0; fi
    sleep 1
  done
}

echo "================================================================"
echo "  COMPREHENSIVE RPC INTEGRATION TEST"
echo "  RPC: $RPC"
echo "================================================================"

# ── Preflight ────────────────────────────────────────────────────────

HEIGHT=$(get_height "$RPC")
if [[ -z "$HEIGHT" ]]; then
  echo "ERROR: Cannot connect to RPC at $RPC"
  exit 1
fi
echo "  Chain height: $HEIGHT"

# Ensure mempool is clear before starting
wait_mempool_clear

# Ensure coinbase maturity: wait if height < 25
if [[ "$HEIGHT" -lt 25 ]]; then
  echo "  Waiting for coinbase maturity (height >= 25)..."
  wait_blocks $((25 - HEIGHT + 1))
fi

# ── Part 1: Input Validation ───────────────────────────────────────
echo ""
echo "-- Part 1: Input Validation --"

# wallet_send
RESP=$(rpc $RPC wallet_send "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"to\":\"0000000000000000000000000000000000000000\",\"amount\":0}")
expect_error "$RESP" "wallet_send: amount=0 rejected"

RESP=$(rpc $RPC wallet_send "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"to\":\"\",\"amount\":1000000000000}")
expect_error "$RESP" "wallet_send: empty to rejected"

RESP=$(rpc $RPC wallet_send "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"to\":\"zz-invalid\",\"amount\":1000000000000}")
expect_error "$RESP" "wallet_send: invalid address rejected"

RESP=$(rpc $RPC wallet_send "{\"name\":\"$WALLET\",\"password\":\"wrong\",\"to\":\"0000000000000000000000000000000000000000\",\"amount\":1000000000000}")
expect_error "$RESP" "wallet_send: wrong password rejected"

RESP=$(rpc $RPC wallet_send "{\"name\":\"nosuch\",\"password\":\"$PASS\",\"to\":\"0000000000000000000000000000000000000000\",\"amount\":1000000000000}")
expect_error "$RESP" "wallet_send: non-existent wallet rejected"

# wallet_stake
RESP=$(rpc $RPC wallet_stake "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"amount\":0}")
expect_error "$RESP" "wallet_stake: amount=0 rejected"

# ── Part 2: Token Metadata Validation ──────────────────────────────
echo ""
echo "-- Part 2: Token Metadata Validation --"

# Bad names
for case_label_name in \
  "special_chars|Bad<Token>" \
  "XSS_injection|<script>alert(1)</script>" \
  "too_long|AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"; do
  LABEL="${case_label_name%%|*}"
  NAME="${case_label_name#*|}"
  RESP=$(rpc $RPC wallet_mintToken "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"token_name\":\"$NAME\",\"token_symbol\":\"TST\",\"decimals\":8,\"amount\":1000}")
  expect_error "$RESP" "mintToken name: $LABEL rejected" "token_name"
done

RESP=$(rpc $RPC wallet_mintToken "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"token_name\":\"\",\"token_symbol\":\"TST\",\"decimals\":8,\"amount\":1000}")
expect_error "$RESP" "mintToken: empty name rejected"

# Bad symbols
for case_label_sym in \
  "lowercase|abc" \
  "single_char|X" \
  "too_long|ABCDEFGHIJK" \
  "special_chars|A!B" \
  "spaces|A B" \
  "underscore|A_B"; do
  LABEL="${case_label_sym%%|*}"
  SYM="${case_label_sym#*|}"
  RESP=$(rpc $RPC wallet_mintToken "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"token_name\":\"Good\",\"token_symbol\":\"$SYM\",\"decimals\":8,\"amount\":1000}")
  expect_error "$RESP" "mintToken symbol: $LABEL rejected" "token_symbol"
done

RESP=$(rpc $RPC wallet_mintToken "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"token_name\":\"Good\",\"token_symbol\":\"\",\"decimals\":8,\"amount\":1000}")
expect_error "$RESP" "mintToken: empty symbol rejected"

# Bad decimals
RESP=$(rpc $RPC wallet_mintToken "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"token_name\":\"Good\",\"token_symbol\":\"TST\",\"decimals\":19,\"amount\":1000}")
expect_error "$RESP" "mintToken: decimals=19 rejected" "decimals"

RESP=$(rpc $RPC wallet_mintToken "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"token_name\":\"Good\",\"token_symbol\":\"TST\",\"decimals\":255,\"amount\":1000}")
expect_error "$RESP" "mintToken: decimals=255 rejected" "decimals"

# Bad amount
RESP=$(rpc $RPC wallet_mintToken "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"token_name\":\"Good\",\"token_symbol\":\"TST\",\"decimals\":8,\"amount\":0}")
expect_error "$RESP" "mintToken: amount=0 rejected"

# Valid edge cases (should NOT be rejected by validation)
RESP=$(rpc $RPC wallet_mintToken "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"token_name\":\"A\",\"token_symbol\":\"AB\",\"decimals\":0,\"amount\":1}")
if echo "$RESP" | grep -q '"token_name must be\|token_symbol must be\|decimals must be"'; then
  fail "mintToken: valid edge case wrongly rejected"
else
  pass "mintToken: valid edge case not rejected by validation"
fi

# ── Part 3: Root Chain Send ────────────────────────────────────────
echo ""
echo "-- Part 3: Root Chain Send --"

# Create receiver wallet (may already exist from previous run)
RESP=$(rpc $RPC wallet_create "{\"name\":\"testreceiver\",\"password\":\"recv\"}")
if echo "$RESP" | grep -q '"error"'; then
  RESP=$(rpc $RPC wallet_listAddresses "{\"name\":\"testreceiver\",\"password\":\"recv\"}")
fi
RECV_ADDR=$(echo "$RESP" | grep -o '"address":"[^"]*"' | head -1 | cut -d'"' -f4)
# Strip tkgx: prefix for raw hex
RECV_HEX="${RECV_ADDR#tkgx:}"
if [[ -z "$RECV_HEX" ]] || [[ ${#RECV_HEX} -ne 40 ]]; then
  fail "create receiver wallet"
  RECV_ADDR="tkgx:0000000000000000000000000000000000000001"
  RECV_HEX="0000000000000000000000000000000000000001"
else
  pass "receiver wallet ready ($RECV_ADDR)"
fi

# Ensure we wait for maturity of previous change outputs
wait_blocks 2

RESP=$(rpc $RPC wallet_send "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"to\":\"$RECV_HEX\",\"amount\":500000000000}")
if echo "$RESP" | grep -q '"result"' && ! echo "$RESP" | grep -q '"error"'; then
  pass "wallet_send: 0.5 KGX to receiver"
  SEND_TX=$(echo "$RESP" | grep -o '"tx_hash":"[^"]*"' | cut -d'"' -f4)
  echo "    tx: $SEND_TX"
else
  # Might be coinbase maturity issue — wait and retry
  echo "  (first attempt failed, waiting for maturity...)"
  wait_blocks 22
  RESP=$(rpc $RPC wallet_send "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"to\":\"$RECV_HEX\",\"amount\":500000000000}")
  expect_success "$RESP" "wallet_send: 0.5 KGX to receiver (retry)"
  SEND_TX=$(echo "$RESP" | grep -o '"tx_hash":"[^"]*"' | cut -d'"' -f4)
  echo "    tx: $SEND_TX"
fi

wait_blocks 2

RESP=$(rpc $RPC utxo_getBalance "{\"address\":\"$RECV_HEX\"}")
RECV_BAL=$(echo "$RESP" | grep -o '"balance":[0-9]*' | cut -d: -f2)
if [[ -n "$RECV_BAL" ]] && [[ "$RECV_BAL" -gt 0 ]]; then
  pass "receiver balance > 0 ($RECV_BAL)"
else
  fail "receiver balance = 0 (expected > 0)"
fi

# ── Part 4: Token Creation ─────────────────────────────────────────
echo ""
echo "-- Part 4: Token Creation --"

# Wait for change output maturity
wait_blocks 22

RESP=$(rpc $RPC wallet_mintToken "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"token_name\":\"Test Token\",\"token_symbol\":\"TTK\",\"decimals\":8,\"amount\":1000000}")
expect_success "$RESP" "mintToken: Test Token / TTK"
TOKEN_ID=$(echo "$RESP" | grep -o '"token_id":"[^"]*"' | cut -d'"' -f4)
echo "    token_id: $TOKEN_ID"

# Wait for confirmation
wait_blocks 22

RESP=$(rpc $RPC wallet_mintToken "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"token_name\":\"My Token-V2\",\"token_symbol\":\"TKV2\",\"decimals\":0,\"amount\":500}")
expect_success "$RESP" "mintToken: My Token-V2 / TKV2 (hyphens/spaces)"

wait_blocks 22

RESP=$(rpc $RPC wallet_mintToken "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"token_name\":\"Max Dec\",\"token_symbol\":\"MDT\",\"decimals\":18,\"amount\":100}")
expect_success "$RESP" "mintToken: decimals=18 (max boundary)"

wait_blocks 2

# Check token metadata
if [[ -n "$TOKEN_ID" ]]; then
  RESP=$(rpc $RPC token_getInfo "{\"token_id\":\"$TOKEN_ID\"}")
  if echo "$RESP" | grep -q '"name":"Test Token"'; then
    pass "token_getInfo: metadata correct"
  else
    fail "token_getInfo: wrong metadata"
  fi
fi

# ── Part 5: Token Send ──────────────────────────────────────────────
echo ""
echo "-- Part 5: Token Send --"

if [[ -n "$TOKEN_ID" ]]; then
  # Wait for maturity
  wait_blocks 22

  RESP=$(rpc $RPC wallet_sendToken "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"token_id\":\"$TOKEN_ID\",\"to\":\"$RECV_HEX\",\"amount\":100}")
  expect_success "$RESP" "sendToken: 100 TTK to receiver"

  RESP=$(rpc $RPC wallet_sendToken "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"token_id\":\"$TOKEN_ID\",\"to\":\"$RECV_HEX\",\"amount\":0}")
  expect_error "$RESP" "sendToken: amount=0 rejected"

  RESP=$(rpc $RPC wallet_sendToken "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"token_id\":\"badid\",\"to\":\"$RECV_HEX\",\"amount\":100}")
  expect_error "$RESP" "sendToken: invalid token_id rejected"

  # Wait for confirmation
  wait_blocks 2

  RESP=$(rpc $RPC token_getBalance "{\"token_id\":\"$TOKEN_ID\",\"address\":\"$RECV_HEX\"}")
  if echo "$RESP" | grep -q '"amount":100'; then
    pass "token_getBalance: receiver has 100 TTK"
  elif echo "$RESP" | grep -q '"amount"'; then
    pass "token_getBalance: receiver has tokens"
  else
    fail "token_getBalance: no tokens found"
  fi
else
  fail "token send: skipped (no token_id)"
fi

# ── Part 6: Sub-chain Operations ────────────────────────────────────
echo ""
echo "-- Part 6: Sub-chain Operations --"

CHAIN_ID=$(rpc $RPC subchain_list | grep -o '"chain_id":"[^"]*"' | head -1 | cut -d'"' -f4)
if [[ -z "$CHAIN_ID" ]]; then
  echo "  No existing sub-chain found, skipping sub-chain send tests"
else
  echo "  Existing sub-chain: $CHAIN_ID"

  RESP=$(rpc $RPC subchain_getInfo "{\"chain_id\":\"$CHAIN_ID\"}")
  expect_success "$RESP" "subchain_getInfo"
  echo "    height: $(echo $RESP | grep -o '"height":[0-9]*' | head -1 | cut -d: -f2)"

  RESP=$(rpc $RPC subchain_getBalance "{\"chain_id\":\"$CHAIN_ID\",\"address\":\"8f3a44b8056cafec368dea0cbe0ad1d9bc3f4305\"}")
  SC_BAL=$(echo "$RESP" | grep -o '"balance":[0-9]*' | cut -d: -f2)
  if [[ -n "$SC_BAL" ]] && [[ "$SC_BAL" -gt 0 ]]; then
    pass "subchain_getBalance: has funds ($SC_BAL)"
  else
    fail "subchain_getBalance: no funds"
  fi

  if [[ -n "$SC_BAL" ]] && [[ "$SC_BAL" -gt 0 ]]; then
    RESP=$(rpc $RPC subchain_send "{\"chain_id\":\"$CHAIN_ID\",\"name\":\"$WALLET\",\"password\":\"$PASS\",\"to\":\"$RECV_HEX\",\"amount\":100000000}")
    expect_success "$RESP" "subchain_send: send on sub-chain"
  fi

  RESP=$(rpc $RPC subchain_send "{\"chain_id\":\"$CHAIN_ID\",\"name\":\"$WALLET\",\"password\":\"$PASS\",\"to\":\"$RECV_HEX\",\"amount\":0}")
  expect_error "$RESP" "subchain_send: amount=0 rejected"

  RESP=$(rpc $RPC subchain_send "{\"chain_id\":\"0000000000000000000000000000000000000000000000000000000000000000\",\"name\":\"$WALLET\",\"password\":\"$PASS\",\"to\":\"$RECV_HEX\",\"amount\":100000000}")
  expect_error "$RESP" "subchain_send: unknown chain_id rejected"

  RESP=$(rpc $RPC subchain_send "{\"chain_id\":\"$CHAIN_ID\",\"name\":\"$WALLET\",\"password\":\"wrong\",\"to\":\"$RECV_HEX\",\"amount\":100000000}")
  expect_error "$RESP" "subchain_send: wrong password rejected"
fi

# ── Part 7: PoA Sub-chain Creation ──────────────────────────────────
echo ""
echo "-- Part 7: PoA Sub-chain Creation --"

VAL_PUB=$(rpc $RPC stake_getValidators | grep -o '"pubkey":"[^"]*"' | head -1 | cut -d'"' -f4)
echo "  Validator pubkey: $VAL_PUB"

# Wait for coinbase maturity
wait_blocks 22

RESP=$(rpc $RPC wallet_createSubChain "{
  \"name\":\"$WALLET\",\"password\":\"$PASS\",
  \"chain_name\":\"IntTestPoA\",\"symbol\":\"ITPA\",
  \"consensus_type\":\"poa\",\"block_time\":2,
  \"block_reward\":1000000000000,
  \"max_supply\":1000000000000000000,
  \"min_fee_rate\":10,
  \"validators\":[\"$VAL_PUB\"]
}")
expect_success "$RESP" "createSubChain: PoA sub-chain"
POA_CHAIN_ID=$(echo "$RESP" | grep -o '"chain_id":"[^"]*"' | cut -d'"' -f4)
echo "    chain_id: $POA_CHAIN_ID"

# Validation
RESP=$(rpc $RPC wallet_createSubChain "{
  \"name\":\"$WALLET\",\"password\":\"$PASS\",
  \"chain_name\":\"Bad<Name>\",\"symbol\":\"TST\",
  \"consensus_type\":\"poa\",\"block_time\":2,
  \"block_reward\":1000000000000,\"max_supply\":1000000000000000000,
  \"min_fee_rate\":10,\"validators\":[\"$VAL_PUB\"]
}")
expect_error "$RESP" "createSubChain: bad name rejected"

RESP=$(rpc $RPC wallet_createSubChain "{
  \"name\":\"$WALLET\",\"password\":\"$PASS\",
  \"chain_name\":\"Good\",\"symbol\":\"bad!\",
  \"consensus_type\":\"poa\",\"block_time\":2,
  \"block_reward\":1000000000000,\"max_supply\":1000000000000000000,
  \"min_fee_rate\":10,\"validators\":[\"$VAL_PUB\"]
}")
expect_error "$RESP" "createSubChain: bad symbol rejected"

RESP=$(rpc $RPC wallet_createSubChain "{
  \"name\":\"$WALLET\",\"password\":\"$PASS\",
  \"chain_name\":\"NoVals\",\"symbol\":\"NV\",
  \"consensus_type\":\"poa\",\"block_time\":2,
  \"block_reward\":1000000000000,\"max_supply\":1000000000000000000,
  \"min_fee_rate\":10
}")
expect_error "$RESP" "createSubChain: PoA without validators rejected"

# Wait for PoA sub-chain confirmation
wait_blocks 5

if [[ -n "$POA_CHAIN_ID" ]]; then
  RESP=$(rpc $RPC subchain_getInfo "{\"chain_id\":\"$POA_CHAIN_ID\"}")
  if echo "$RESP" | grep -q '"name":"IntTestPoA"'; then
    pass "PoA sub-chain active"
  else
    fail "PoA sub-chain not found"
  fi
fi

# ── Part 8: Multi-node Sync ─────────────────────────────────────────
echo ""
echo "-- Part 8: Multi-node Sync --"

sleep 2
H1=$(get_height "$RPC")
H2=$(get_height "$RPC2")
H3=$(get_height "$RPC3")
H4=$(get_height "$RPC4")
echo "  Heights: node1=$H1 node2=$H2 node3=$H3 node4=$H4"

ALL_CLOSE=true
for h in $H2 $H3 $H4; do
  if [[ -z "$h" ]]; then continue; fi
  D=$(( H1 > h ? H1 - h : h - H1 ))
  if [[ "$D" -gt 1 ]]; then ALL_CLOSE=false; fi
done
if $ALL_CLOSE; then
  pass "all nodes synced (within 1 block)"
else
  fail "nodes not synced"
fi

TIP1=$(rpc $RPC chain_getInfo | grep -o '"tip_hash":"[^"]*"' | cut -d'"' -f4)
TIP2=$(rpc $RPC2 chain_getInfo | grep -o '"tip_hash":"[^"]*"' | cut -d'"' -f4)
if [[ "$TIP1" == "$TIP2" ]]; then
  pass "node1 and node2 tip match"
else
  fail "tips differ"
fi

# ── Part 9: Wallet History ──────────────────────────────────────────
echo ""
echo "-- Part 9: Wallet History --"

RESP=$(rpc $RPC wallet_getHistory "{\"name\":\"$WALLET\",\"password\":\"$PASS\",\"limit\":10}")
TOTAL=$(echo "$RESP" | grep -o '"total":[0-9]*' | cut -d: -f2)
if [[ -n "$TOTAL" ]] && [[ "$TOTAL" -gt 0 ]]; then
  pass "wallet_getHistory: $TOTAL entries"
else
  fail "wallet_getHistory: no entries"
fi

if echo "$RESP" | grep -q '"type":"mined"'; then pass "history: has mined entries"; fi
if echo "$RESP" | grep -q '"type":"sent"'; then pass "history: has sent entries"; fi

# ── Part 10: Edge Cases ─────────────────────────────────────────────
echo ""
echo "-- Part 10: Edge Cases --"

RESP=$(rpc $RPC "nonexistent_method")
expect_error "$RESP" "unknown method rejected"

RESP=$(rpc $RPC wallet_send)
expect_error "$RESP" "wallet_send: no params rejected"

RESP=$(rpc $RPC wallet_create "{\"name\":\"$WALLET\",\"password\":\"$PASS\"}")
expect_error "$RESP" "wallet_create: duplicate name rejected"

# Create a broke wallet and try to send from it
rpc $RPC wallet_create "{\"name\":\"__broke__\",\"password\":\"b\"}" > /dev/null 2>&1
RESP=$(rpc $RPC wallet_send "{\"name\":\"__broke__\",\"password\":\"b\",\"to\":\"$RECV_HEX\",\"amount\":1000000000000}")
expect_error "$RESP" "wallet_send: broke wallet rejected"

RESP=$(rpc $RPC validator_getStatus)
expect_success "$RESP" "validator_getStatus"

# ── Summary ─────────────────────────────────────────────────────────
echo ""
echo "================================================================"
if [[ "$PASS_FAIL" -eq 0 ]]; then
  echo "  ALL PASSED: $PASS_OK/$TESTS_RUN tests"
else
  echo "  RESULTS: $PASS_OK passed, $PASS_FAIL FAILED, $TESTS_RUN total"
fi
echo "================================================================"

exit $PASS_FAIL

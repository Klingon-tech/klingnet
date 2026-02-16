#!/usr/bin/env bash
# test-cli.sh — Build and test the klingnet-cli against a live node.
# Usage: ./scripts/test-cli.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TESTDIR="/tmp/klingnet-cli-test"
RPC="http://127.0.0.1:8645"

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${CYAN}>>> $*${NC}"; }
pass()  { echo -e "${GREEN}  PASS: $*${NC}"; }
fail()  { echo -e "${RED}  FAIL: $*${NC}"; FAILED=1; }

FAILED=0
NODE_PID=""

cleanup() {
    if [ -n "$NODE_PID" ]; then
        info "Stopping node (PID $NODE_PID)"
        kill "$NODE_PID" 2>/dev/null || true
        wait "$NODE_PID" 2>/dev/null || true
    fi
    rm -rf "$TESTDIR"
}
trap cleanup EXIT

# ── Step 1: Build ────────────────────────────────────────────────────────
info "Building binaries..."
cd "$ROOT"
go build -o bin/klingnetd ./cmd/klingnetd
go build -o bin/klingnet-cli ./cmd/klingnet-cli
pass "Binaries built"

# ── Step 2: Setup testnet ────────────────────────────────────────────────
info "Setting up testnet directory..."
rm -rf "$TESTDIR"
mkdir -p "$TESTDIR/testnet"

# Generate validator key.
python3 -c "import secrets; print(secrets.token_hex(32))" > "$TESTDIR/validator.key"

# Derive pubkey and address using a small Go helper.
DERIVE_OUT=$(cd "$ROOT" && go run scripts/derive_key.go "$TESTDIR/validator.key")
PUBKEY=$(echo "$DERIVE_OUT" | grep "^pubkey=" | cut -d= -f2)
ADDRESS=$(echo "$DERIVE_OUT" | grep "^address=" | cut -d= -f2)

info "Validator pubkey:  $PUBKEY"
info "Validator address: $ADDRESS"

# Create genesis.json
cat > "$TESTDIR/testnet/genesis.json" <<GENESIS
{
  "chain_id": "klingnet-cli-test",
  "chain_name": "CLI Test",
  "timestamp": 0,
  "alloc": {
    "$ADDRESS": 200000000000000000
  },
  "protocol": {
    "consensus": {
      "type": "poa",
      "block_time": 2,
      "validators": ["$PUBKEY"],
      "block_reward": 1000000000,
      "max_supply": 1000000000000000000,
      "min_fee_rate": 10
    },
    "subchain": { "max_depth": 10, "max_per_parent": 100, "anchor_interval": 10 },
    "token": { "max_tokens_per_utxo": 1, "allow_minting": true }
  }
}
GENESIS
pass "Genesis created (200,000 coins to $ADDRESS)"

# ── Step 3: Start node ──────────────────────────────────────────────────
info "Starting klingnetd..."
"$ROOT/bin/klingnetd" \
    --network=testnet \
    --datadir="$TESTDIR" \
    --mine \
    --validator-key="$TESTDIR/validator.key" &
NODE_PID=$!
sleep 3

if ! kill -0 "$NODE_PID" 2>/dev/null; then
    fail "Node failed to start"
    exit 1
fi
pass "Node running (PID $NODE_PID)"

CLI="$ROOT/bin/klingnet-cli --rpc $RPC"

# ── Step 4: Test CLI commands ────────────────────────────────────────────
info "Testing 'status'..."
if OUTPUT=$($CLI status 2>&1); then
    echo "$OUTPUT"
    echo "$OUTPUT" | grep -q "Chain:" && pass "status" || fail "status: missing Chain"
else
    fail "status: $OUTPUT"
fi

echo

info "Testing 'block 0' (genesis)..."
if OUTPUT=$($CLI block 0 2>&1); then
    echo "$OUTPUT"
    echo "$OUTPUT" | grep -q "Height:" && pass "block 0" || fail "block 0: missing Height"
else
    fail "block 0: $OUTPUT"
fi

echo

info "Testing 'balance $ADDRESS'..."
if OUTPUT=$($CLI balance "$ADDRESS" 2>&1); then
    echo "$OUTPUT"
    echo "$OUTPUT" | grep -q "Balance:" && pass "balance" || fail "balance: missing Balance"
else
    fail "balance: $OUTPUT"
fi

echo

info "Testing 'mempool'..."
if OUTPUT=$($CLI mempool 2>&1); then
    echo "$OUTPUT"
    echo "$OUTPUT" | grep -q "Count:" && pass "mempool" || fail "mempool: missing Count"
else
    fail "mempool: $OUTPUT"
fi

echo

info "Testing 'peers'..."
if OUTPUT=$($CLI peers 2>&1); then
    echo "$OUTPUT"
    echo "$OUTPUT" | grep -q "Node ID:" && pass "peers" || fail "peers: missing Node ID"
else
    fail "peers: $OUTPUT"
fi

echo

# Wait for a block to be produced, then test block 1.
info "Waiting for block 1..."
sleep 3
if OUTPUT=$($CLI block 1 2>&1); then
    echo "$OUTPUT"
    echo "$OUTPUT" | grep -q "Height:       1" && pass "block 1 (mined)" || fail "block 1: wrong height"
else
    fail "block 1: $OUTPUT"
fi

echo

# Test wallet commands (local, no password prompt needed for list).
info "Testing 'wallet list' (empty)..."
if OUTPUT=$($CLI --datadir "$TESTDIR" wallet list 2>&1); then
    echo "$OUTPUT"
    echo "$OUTPUT" | grep -q "No wallets found" && pass "wallet list (empty)" || fail "wallet list"
else
    fail "wallet list: $OUTPUT"
fi

echo

# ── Summary ──────────────────────────────────────────────────────────────
echo
if [ "$FAILED" -eq 0 ]; then
    echo -e "${GREEN}All CLI tests passed!${NC}"
else
    echo -e "${RED}Some tests failed.${NC}"
    exit 1
fi

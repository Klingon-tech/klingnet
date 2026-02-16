#!/bin/bash
# start-testnet.sh - Wipe, init, and start a 4-node local testnet with sub-chain mining
#
# Uses the well-known "abandon...art" testnet validator identity.
# All nodes discover each other via mDNS (same machine).
#
# Ports:
#   Node 1 (validator):       P2P=30310  RPC=8701
#   Node 2 (peer):            P2P=30311  RPC=8702
#   Node 3 (peer):            P2P=30312  RPC=8703
#   Node 4 (sub-chain miner): P2P=30313  RPC=8704
#
# Workflow:
#   1. Build binaries
#   2. Wipe & init 4 nodes
#   3. Start nodes 1-3 (node1 = validator/miner, syncs sub-chains)
#   4. Import wallet on all 3 nodes
#   5. Wait for coinbase maturity (~25 blocks)
#   6. Create PoW sub-chain via wallet RPC
#   7. Start node 4 with --sync-subchains=all --mine-subchains=<chain_id>
#
# Usage:
#   ./scripts/start-testnet.sh              # build + start all
#   ./scripts/start-testnet.sh --no-build   # skip build step

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY="$PROJECT_DIR/klingnetd"

VALIDATOR_PRIVKEY="1f0717e6e34acc6721021f4dfed54558ec8452452b6195545d06dd348b220091"
COINBASE="8f3a44b8056cafec368dea0cbe0ad1d9bc3f4305"
MNEMONIC="abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"
WALLET_NAME="validator"
WALLET_PASS="test"

NODE1_DIR="/tmp/testnet-node1"
NODE2_DIR="/tmp/testnet-node2"
NODE3_DIR="/tmp/testnet-node3"
NODE4_DIR="/tmp/testnet-node4"

LOG1="/tmp/testnet-node1.log"
LOG2="/tmp/testnet-node2.log"
LOG3="/tmp/testnet-node3.log"
LOG4="/tmp/testnet-node4.log"

# ---------- build ----------
if [[ "$1" != "--no-build" ]]; then
    echo "==> Building klingnetd..."
    cd "$PROJECT_DIR"
    go build -o klingnetd ./cmd/klingnetd/
    go build -o klingnet-cli ./cmd/klingnet-cli/
fi

# ---------- clean + init ----------
echo "==> Wiping old data..."
rm -rf "$NODE1_DIR" "$NODE2_DIR" "$NODE3_DIR" "$NODE4_DIR"

echo "==> Initializing 4 nodes..."
"$BINARY" --init --network=testnet --datadir="$NODE1_DIR" 2>&1 | sed 's/^/  [init-1] /'
"$BINARY" --init --network=testnet --datadir="$NODE2_DIR" 2>&1 | sed 's/^/  [init-2] /'
"$BINARY" --init --network=testnet --datadir="$NODE3_DIR" 2>&1 | sed 's/^/  [init-3] /'
"$BINARY" --init --network=testnet --datadir="$NODE4_DIR" 2>&1 | sed 's/^/  [init-4] /'

# ---------- validator key ----------
echo -n "$VALIDATOR_PRIVKEY" > "$NODE1_DIR/validator.key"
chmod 600 "$NODE1_DIR/validator.key"
cp "$NODE1_DIR/validator.key" "$NODE4_DIR/validator.key"

# ---------- launch nodes 1-3 ----------
echo ""
echo "==> Starting 4-node testnet"
echo "    Node 1 (validator):       RPC http://127.0.0.1:8701  log $LOG1"
echo "    Node 2 (peer):            RPC http://127.0.0.1:8702  log $LOG2"
echo "    Node 3 (peer):            RPC http://127.0.0.1:8703  log $LOG3"
echo "    Node 4 (sub-chain miner): RPC http://127.0.0.1:8704  log $LOG4"
echo ""

# Node 1 — validator + miner + sync all sub-chains
"$BINARY" --network=testnet --datadir="$NODE1_DIR" \
    --p2p-port=30310 --rpc-port=8701 \
    --mine --coinbase="$COINBASE" --validator-key="$NODE1_DIR/validator.key" \
    --wallet --sync-subchains=all --log-level=info \
    > "$LOG1" 2>&1 &
PID1=$!

sleep 1

# Node 2 — sync-only peer
"$BINARY" --network=testnet --datadir="$NODE2_DIR" \
    --p2p-port=30311 --rpc-port=8702 \
    --wallet --log-level=info \
    > "$LOG2" 2>&1 &
PID2=$!

# Node 3 — sync-only peer
"$BINARY" --network=testnet --datadir="$NODE3_DIR" \
    --p2p-port=30312 --rpc-port=8703 \
    --wallet --log-level=info \
    > "$LOG3" 2>&1 &
PID3=$!

PID4=""
cleanup() {
    echo ""
    echo "==> Stopping nodes..."
    kill "$PID1" "$PID2" "$PID3" $PID4 2>/dev/null
    wait "$PID1" "$PID2" "$PID3" $PID4 2>/dev/null
    echo "Done."
}
trap cleanup INT TERM

# ---------- import wallet via RPC ----------
echo "==> Waiting for RPC to be ready..."
for i in $(seq 1 15); do
    if curl -s http://127.0.0.1:8701 -d '{"jsonrpc":"2.0","id":0,"method":"chain_getInfo","params":null}' | grep -q '"result"' 2>/dev/null; then
        break
    fi
    sleep 1
done

echo "==> Importing wallet '$WALLET_NAME' on nodes 1-3..."
for PORT in 8701 8702 8703; do
    RESP=$(curl -s http://127.0.0.1:$PORT -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"wallet_import\",\"params\":{\"name\":\"$WALLET_NAME\",\"password\":\"$WALLET_PASS\",\"mnemonic\":\"$MNEMONIC\"}}")
    if echo "$RESP" | grep -q '"error"'; then
        echo "  [node:$PORT] Warning: $(echo "$RESP" | grep -o '"message":"[^"]*"')"
    else
        ADDR=$(echo "$RESP" | grep -o '"address":"[^"]*"' | head -1 | cut -d'"' -f4)
        echo "  [node:$PORT] Wallet imported: $ADDR"
    fi
done

# ---------- wait for coinbase maturity ----------
echo "==> Waiting for coinbase maturity (height >= 25)..."
for i in $(seq 1 120); do
    HEIGHT=$(curl -s http://127.0.0.1:8701 -d '{"jsonrpc":"2.0","id":0,"method":"chain_getInfo","params":null}' 2>/dev/null \
        | grep -o '"height":[0-9]*' | head -1 | cut -d: -f2)
    if [[ -n "$HEIGHT" ]] && [[ "$HEIGHT" -ge 25 ]]; then
        echo "  Chain height: $HEIGHT (maturity reached)"
        break
    fi
    sleep 1
done

# ---------- create PoW sub-chain ----------
echo "==> Creating PoW sub-chain..."
SC_RESP=$(curl -s http://127.0.0.1:8701 -d '{
  "jsonrpc":"2.0","id":1,"method":"wallet_createSubChain",
  "params":{
    "name":"validator","password":"test",
    "chain_name":"TestPoW","symbol":"TPW",
    "consensus_type":"pow","block_time":5,
    "block_reward":1000000000000,
    "max_supply":1000000000000000000,
    "min_fee_rate":10,
    "initial_difficulty":1
  }
}')

CHAIN_ID=$(echo "$SC_RESP" | grep -o '"chain_id":"[^"]*"' | cut -d'"' -f4)
TX_HASH=$(echo "$SC_RESP" | grep -o '"tx_hash":"[^"]*"' | cut -d'"' -f4)

if [[ -z "$CHAIN_ID" ]]; then
    echo "  ERROR: Sub-chain creation failed: $SC_RESP"
    echo ""
    echo "Nodes 1-3 running (PIDs: $PID1 $PID2 $PID3). Ctrl+C to stop."
    wait
    exit 1
fi

echo "  Sub-chain TX: $TX_HASH"
echo "  Chain ID:     $CHAIN_ID"

# Wait for confirmation (2 blocks)
echo "==> Waiting for sub-chain confirmation..."
sleep 10

# Verify sub-chain is active
SC_HEIGHT=$(curl -s http://127.0.0.1:8701 -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"subchain_getInfo\",\"params\":{\"chain_id\":\"$CHAIN_ID\"}}" 2>/dev/null \
    | grep -o '"height":[0-9]*' | head -1 | cut -d: -f2)
echo "  Sub-chain confirmed (height: ${SC_HEIGHT:-unknown})"

# ---------- start node 4 (sub-chain miner) ----------
echo "==> Starting node 4 (sub-chain miner)..."
"$BINARY" --network=testnet --datadir="$NODE4_DIR" \
    --p2p-port=30313 --rpc-port=8704 \
    --coinbase="$COINBASE" --validator-key="$NODE4_DIR/validator.key" \
    --sync-subchains=all --mine-subchains="$CHAIN_ID" \
    --wallet --log-level=info \
    > "$LOG4" 2>&1 &
PID4=$!

# Import wallet on node 4
sleep 5
RESP=$(curl -s http://127.0.0.1:8704 -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"wallet_import\",\"params\":{\"name\":\"$WALLET_NAME\",\"password\":\"$WALLET_PASS\",\"mnemonic\":\"$MNEMONIC\"}}")
if echo "$RESP" | grep -q '"error"'; then
    echo "  [node:8704] Warning: $(echo "$RESP" | grep -o '"message":"[^"]*"')"
else
    echo "  [node:8704] Wallet imported"
fi

echo ""
echo "All 4 nodes running (PIDs: $PID1 $PID2 $PID3 $PID4)"
echo "Wallet: $WALLET_NAME (password: $WALLET_PASS)"
echo "Sub-chain: $CHAIN_ID"
echo "  Node 4 mines PoW sub-chain blocks"
echo "  Node 1 syncs sub-chain blocks via P2P"
echo "Ctrl+C to stop all."
echo ""
CLI="./klingnet-cli --rpc=http://127.0.0.1:8701 --datadir=/tmp/testnet-node1 --network=testnet"
echo "Quick commands:"
echo "  tail -f $LOG1                          # watch validator"
echo "  tail -f $LOG4                          # watch sub-chain miner"
echo "  $CLI status"
echo "  $CLI subchains"
echo "  $CLI subchain info --chain-id $CHAIN_ID"
echo "  $CLI wallet balance --wallet $WALLET_NAME"
echo ""

# Wait for any child to exit, then keep waiting
wait

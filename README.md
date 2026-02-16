# Klingnet Chain

A UTXO-based blockchain written in Go with flat sub-chain architecture, Schnorr signatures, PoA/PoW consensus, and validator staking.

## Status

**Work in progress.** Core internals are functional and tested (~500+ tests across 17 packages). The full node daemon (`klingnetd`) runs a real node with BadgerDB persistence, P2P networking (DHT + mDNS), PoA block production, validator staking/unstaking, sub-chain management, and a JSON-RPC API. The CLI tool (`klingnet-cli`) provides wallet management, transaction sending, staking, and sub-chain creation against a running node. A desktop GUI (`klingnet-qt`) built with Wails v2 + React is also available.

### What Works

| Component | Status | Notes |
|-----------|--------|-------|
| Types & primitives | Done | Hash, Address, Outpoint, Script, TokenData |
| Cryptography | Done | BLAKE3 hashing, Schnorr/secp256k1 signatures |
| HD Wallet | Done | BIP-39 mnemonic, BIP-32 derivation, Argon2+XChaCha20 keystore |
| Storage | Done | BadgerDB + in-memory + PrefixDB backend, UTXO store with address index |
| Transactions | Done | Builder, structural + UTXO-aware validation, fee calculation |
| Blocks | Done | Header, merkle tree, structural validation |
| Consensus | Done | PoA with validator signatures + PoW (BLAKE3 hash-target), deterministic random validator selection |
| Chain state | Done | Genesis init, block processing, block store, tip tracking, reorg, coinbase maturity (20 blocks), unstake cooldown (20 blocks) |
| Token system | Done | Mint/transfer/burn, conservation rule, metadata store, creation fee (50 KGX), validated in chain + mempool |
| Mempool | Done | UTXO validation on entry, conflict detection, fee-rate ordering, min fee, coinbase maturity, token validation |
| Block producer | Done | Coinbase + fee collection, merkle root, PoA sealing, supply cap, validator priority scheduling |
| P2P networking | Done | libp2p, GossipSub, mDNS + Kademlia DHT discovery, peer persistence, chain sync, height protocol |
| 2-node testnet | Done | Full integration — produces blocks, gossips, verifies convergence |
| 3-node testnet | Done | Shell script: build, init, start 3 nodes, import wallets, validator mining |
| Config system | Done | Genesis rules, node config, CLI flags, config file |
| Validator staking | Done | Lock coins to ScriptTypeStake UTXO, auto-register, unstake with cooldown, validator removal |
| Address format | Done | Bech32 encoding: `kgx1...` (mainnet), `tkgx1...` (testnet), with checksum |
| RPC server | Done | JSON-RPC 2.0 API — 38 endpoints (chain, UTXO, tx, mempool, net, stake, subchain, wallet, token, validator, mining) |
| CLI tool | Done | 18 commands — status, block, tx, send, sendmany, balance, mempool, peers, wallet, validators, stake, subchains |
| Desktop GUI | Done | Wails v2 + React TypeScript, 14 pages, connects to klingnetd via RPC |
| RPC client | Done | Reusable JSON-RPC 2.0 client library |
| Sub-chain system | Done | Registration (burn 1,000 KGX), spawning, PrefixDB isolation, PoA/PoW, dynamic validators |

### Deferred

- Light client / SPV support — not needed for initial launch
- Script evaluation engine — type-matching works fine for current use cases

## Quick Start

### Run the Local Testnet

This is the fastest way to see the chain running. It boots 2 in-process nodes, produces 10 blocks via PoA, gossips them over libp2p, and verifies both chains converge.

```bash
go run ./cmd/testnet/
```

Expected output:
```
=== Klingnet 2-Node Local Testnet ===
Validator key generated          validator_pub=a1b2c3... coinbase_addr=d4e5f6...
Genesis config created           chain_id=klingnet-testnet-local
Genesis initialized on both nodes node1_height=0 node2_height=0
P2P nodes started                node1_id=12D3Koo... node2_id=12D3Koo...
Nodes connected                  node1_peers=1 node2_peers=1
Starting block production        blocks=10 interval=3s
Block produced                   height=1 hash=ab12cd... txs=1 reward=1000000000
Block received and applied       height=1 hash=ab12cd...
...
Block produced                   height=10 ...
Final chain state                node1_height=10 node2_height=10 ...
SUCCESS: Both nodes converged — chains match!

  Blocks produced:  10
  Chain tip:        ab12cd34...
  Genesis alloc:    200000 coins
  Block reward:     0.02 coins
  Min fee:          0.000001 coins
  Max supply:       2000000 coins
  Decimals:         12
```

The testnet runs for ~30 seconds. Press `Ctrl+C` for early shutdown.

### Run a 3-Node Manual Testnet

For longer-running tests with wallet support:

```bash
# Build and start 3 nodes (validator + 2 peers)
./scripts/start-testnet.sh

# Skip build if already compiled
./scripts/start-testnet.sh --no-build
```

This starts 3 persistent nodes with BadgerDB storage, imports a wallet on all nodes, and the validator produces blocks continuously:

| Node | Role | P2P Port | RPC Port | Log |
|------|------|----------|----------|-----|
| Node 1 | Validator + Miner | 30310 | 8701 | /tmp/testnet-node1.log |
| Node 2 | Sync peer | 30311 | 8702 | /tmp/testnet-node2.log |
| Node 3 | Sync peer | 30312 | 8703 | /tmp/testnet-node3.log |

The script uses the well-known `abandon...art` BIP-39 mnemonic (wallet name: `validator`, password: `test`). Press `Ctrl+C` to stop all nodes.

```bash
# Example commands after startup
./klingnet-cli --rpc=http://127.0.0.1:8701 --network=testnet status
./klingnet-cli --rpc=http://127.0.0.1:8701 --datadir=/tmp/testnet-node1 --network=testnet wallet balance --wallet validator
./klingnet-cli --rpc=http://127.0.0.1:8701 --datadir=/tmp/testnet-node1 --network=testnet send --wallet validator --to <address> --amount 1.5
```

### What the Testnet Does

1. Generates a fresh validator key (Schnorr/secp256k1)
2. Creates a genesis config with 200,000 coin allocation to the validator
3. Boots 2 nodes with in-memory storage, both init from the same genesis
4. Node 1 is the block producer (PoA signer + miner)
5. Starts libp2p on random ports, connects nodes directly
6. Produces 10 blocks (3s apart), each broadcast via GossipSub
7. Node 2 receives blocks, validates, and applies them
8. Verifies both nodes have the same height and tip hash

### Run a Full Node

```bash
# Build
go build -o bin/klingnetd ./cmd/klingnetd

# Start a non-mining node (data dirs are created automatically)
bin/klingnetd --network=testnet
```

This creates:
```
~/.klingnet/
├── klingnet.conf              # Node configuration
└── testnet/                  # (or mainnet/)
    └── (BadgerDB files)      # Blocks + UTXO state
```

The node opens a BadgerDB, initializes the chain from genesis (or resumes if data exists), starts P2P networking with mDNS discovery, and listens for blocks and transactions from peers.

### Run a Validator (Block Producer)

PoA validators need a private key and must be listed in genesis. Here's how to set up a validator from scratch:

**1. Generate a validator key:**
```bash
# Generate a random 32-byte private key (hex-encoded)
openssl rand -hex 32 > validator.key
chmod 600 validator.key
```

**2. Get the public key and address:**

You need the public key (for genesis) and address (for coinbase). Use this Go snippet or build a helper:
```go
// Read key, derive pubkey + address
keyBytes, _ := hex.DecodeString(strings.TrimSpace(keyHex))
key, _ := crypto.PrivateKeyFromBytes(keyBytes)
pubkey := hex.EncodeToString(key.PublicKey())   // 33-byte compressed, for genesis validators
address := hex.EncodeToString(crypto.AddressFromPubKey(key.PublicKey())[:])  // 20-byte, for alloc
```

**3. Start the validator:**

Genesis configuration is hardcoded — no file editing needed. The testnet genesis includes the well-known testnet validator key by default.

**4. Start the validator:**
```bash
bin/klingnetd --network=testnet --mine --validator-key=validator.key
```

If `--coinbase` is omitted, the address is derived automatically from the validator key. The node will produce a block every `block_time` seconds (default: 3s) and broadcast it to peers.

**5. Start a second (non-mining) node:**
```bash
# In another terminal — will discover the validator via mDNS and sync blocks
bin/klingnetd --network=testnet
```

Both nodes must use the same genesis.json. The follower node discovers the validator via mDNS (on the same LAN) and receives blocks via GossipSub.

### External Mining (PoW Sub-chains)

The built-in miner handles PoW mining automatically via `--mine-subchains`. For external GPU/ASIC miners, two RPC endpoints are available:

**Mining workflow:**
1. **Get template:** `mining_getBlockTemplate` — returns a complete block (nonce=0) + hex target
2. **Mine:** Iterate nonce, compute `BLAKE3(header.SigningBytes())`, check `hash <= target`
3. **Submit:** `mining_submitBlock` — send the solved block back to the node

**RPC: `mining_getBlockTemplate`**
```json
{"method": "mining_getBlockTemplate", "params": {"chain_id": "<hex>", "coinbase_address": "<addr>"}}
```
Returns: `block` (full JSON block, nonce=0), `target` (64-char hex), `difficulty`, `height`, `prev_hash`.

**RPC: `mining_submitBlock`**
```json
{"method": "mining_submitBlock", "params": {"chain_id": "<hex>", "block": <solved_block_json>}}
```
Returns: `block_hash`, `height`. The node validates the block, updates the chain, and broadcasts via P2P.

**Block header format for mining** (100 bytes, little-endian):
```
version(4) | prev_hash(32) | merkle_root(32) | timestamp(8) | height(8) | difficulty(8) | nonce(8)
```

The hash function is BLAKE3-256. The target is `MaxUint256 / difficulty`. The miner iterates the nonce (and optionally the timestamp) until `BLAKE3(signingBytes) <= target`.

**CLI commands:**
```bash
klingnet-cli mining gettemplate --chain <id> --address <coinbase>
klingnet-cli mining submit --chain <id> --block solved.json
```

### Restart Behavior

The node persists chain state to BadgerDB. On restart:
- If the DB has existing blocks, it resumes from the last tip (no re-init)
- If the DB is empty, it initializes from genesis

```bash
# First run: "Chain initialized from genesis"
bin/klingnetd --network=testnet --mine --validator-key=validator.key

# After Ctrl+C and restart: "Chain resumed from database" at last height
bin/klingnetd --network=testnet --mine --validator-key=validator.key
```

### JSON-RPC API

The node starts a JSON-RPC 2.0 server (default: `127.0.0.1:8545` mainnet, `127.0.0.1:8645` testnet). Use `--rpc-addr` and `--rpc-port` to customize.

```bash
# Chain info
curl -s -X POST http://127.0.0.1:8645 \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"chain_getInfo","id":1}'

# Get block by height
curl -s -X POST http://127.0.0.1:8645 \
  -d '{"jsonrpc":"2.0","method":"chain_getBlockByHeight","params":{"height":0},"id":2}'

# Check balance (accepts bech32 or raw hex address)
curl -s -X POST http://127.0.0.1:8645 \
  -d '{"jsonrpc":"2.0","method":"utxo_getBalance","params":{"address":"kgx1..."},"id":3}'
```

**Available methods:**

| Method | Params | Description |
|--------|--------|-------------|
| `chain_getInfo` | none | Chain ID, symbol, height, tip hash |
| `chain_getBlockByHash` | `{hash}` | Full block by hash (includes block + tx hashes) |
| `chain_getBlockByHeight` | `{height}` | Full block by height (includes block + tx hashes) |
| `chain_getTransaction` | `{hash}` | Transaction by hash (includes tx hash) |
| `utxo_get` | `{tx_id, index}` | Single UTXO by outpoint |
| `utxo_getByAddress` | `{address}` | All UTXOs for an address |
| `utxo_getBalance` | `{address}` | Sum of UTXOs for an address |
| `tx_submit` | `{transaction}` | Submit signed tx to mempool + broadcast |
| `tx_validate` | `{transaction}` | Dry-run validation |
| `mempool_getInfo` | none | Pending tx count and min fee |
| `mempool_getContent` | none | List of pending tx hashes |
| `net_getPeerInfo` | none | Connected peers |
| `net_getNodeInfo` | none | Node ID and listen addresses |
| `stake_getInfo` | `{pubkey}` | Stake details for a validator pubkey |
| `stake_getValidators` | none | List all validators with genesis/stake status |
| `subchain_list` | none | List all registered sub-chains |
| `subchain_getInfo` | `{chain_id}` | Sub-chain details by chain ID hex |
| `wallet_create` | `{name, password}` | Create wallet, return mnemonic + address |
| `wallet_import` | `{name, password, mnemonic}` | Import wallet from mnemonic |
| `wallet_list` | none | List wallet names |
| `wallet_newAddress` | `{name, password}` | Derive next external address |
| `wallet_listAddresses` | `{name, password}` | List all wallet addresses |
| `wallet_send` | `{name, password, to, amount}` | Build, sign, submit transaction |
| `wallet_consolidate` | `{name, password, max_inputs?, chain_id?}` | Merge many small UTXOs into one spendable output |
| `wallet_sendMany` | `{name, password, recipients:[{to,amount},...]}` | Multi-output transaction (batch send) |
| `wallet_exportKey` | `{name, password, account, index}` | Export private key at BIP-32 path |
| `wallet_stake` | `{name, password, amount}` | Create staking tx to become validator |
| `wallet_unstake` | `{name, password}` | Withdraw all stake, return coins with cooldown |
| `wallet_mintToken` | `{name, password, token_name, ...}` | Mint a new token (50 KGX creation fee) |
| `wallet_sendToken` | `{name, password, token_id, to, amount}` | Transfer tokens |
| `wallet_createSubChain` | `{name, password, chain_name, ...}` | Create sub-chain (burns 1,000 KGX) |
| `wallet_getHistory` | `{name, password, limit?, offset?}` | Transaction history (sent/received/mined) |
| `wallet_rescan` | `{name, password, from_height?, derive_limit?, chain_id?}` | Re-derive/scans wallet addresses to recover funds |
| `subchain_getBalance` | `{chain_id, address}` | Balance on a sub-chain |
| `subchain_send` | `{chain_id, name, password, to, amount}` | Send on a sub-chain |
| `token_getInfo` | `{token_id}` | Token metadata |
| `token_getBalance` | `{address}` | Token balances for address |
| `token_list` | none | List all tokens |
| `mining_getBlockTemplate` | `{chain_id, coinbase_address}` | Get PoW block template for external mining |
| `mining_submitBlock` | `{chain_id, block}` | Submit a solved PoW block |
| `validator_getStatus` | `{pubkey?, chain_id?}` | Validator liveness, heartbeat, block/miss stats |

### CLI Tool (`klingnet-cli`)

The CLI is a thin command-line client that talks to a running node via JSON-RPC. It also manages local wallets (BIP-39 mnemonic, Argon2-encrypted keystore, BIP-32 HD derivation).

```bash
# Build
go build -o bin/klingnet-cli ./cmd/klingnet-cli

# Query commands (require a running node)
bin/klingnet-cli --rpc http://127.0.0.1:8645 status
bin/klingnet-cli --rpc http://127.0.0.1:8645 block 0
bin/klingnet-cli --rpc http://127.0.0.1:8645 block <hash>
bin/klingnet-cli --rpc http://127.0.0.1:8645 tx <hash>
bin/klingnet-cli --rpc http://127.0.0.1:8645 balance <address>
bin/klingnet-cli --rpc http://127.0.0.1:8645 mempool
bin/klingnet-cli --rpc http://127.0.0.1:8645 peers
bin/klingnet-cli --rpc http://127.0.0.1:8645 validators
bin/klingnet-cli --rpc http://127.0.0.1:8645 stake info <pubkey>
bin/klingnet-cli --rpc http://127.0.0.1:8645 subchains
bin/klingnet-cli --rpc http://127.0.0.1:8645 subchain info <chain_id>

# Wallet commands (local keystore at <datadir>/<network>/keystore)
bin/klingnet-cli --network testnet wallet create --name mywallet
bin/klingnet-cli --network testnet wallet list
bin/klingnet-cli --network testnet wallet address --wallet mywallet
bin/klingnet-cli --network testnet wallet new-address --wallet mywallet
bin/klingnet-cli --rpc http://127.0.0.1:8645 --network testnet wallet consolidate --wallet mywallet --max-inputs 500
# sub-chain consolidation
bin/klingnet-cli --rpc http://127.0.0.1:8645 --network testnet wallet consolidate --wallet mywallet --chain-id <chain_id_hex> --max-inputs 500
bin/klingnet-cli --rpc http://127.0.0.1:8645 --network testnet wallet rescan --wallet mywallet --from-height 0 --derive-limit 5000 --timeout 1800
bin/klingnet-cli --rpc http://127.0.0.1:8645 --network testnet wallet balance --wallet mywallet

# Send a transaction (decrypt → HD derive → coin select → sign → submit)
bin/klingnet-cli --rpc http://127.0.0.1:8645 --network testnet send \
  --wallet mywallet --to <address> --amount 10.5

# Send to multiple recipients in one transaction (saves fees vs multiple sends)
# recipients.json: [{"to": "<addr1>", "amount": "1.5"}, {"to": "<addr2>", "amount": "2.0"}]
bin/klingnet-cli --rpc http://127.0.0.1:8645 --network testnet sendmany \
  --wallet mywallet --recipients recipients.json

# Staking (become a validator / withdraw stake)
bin/klingnet-cli --rpc http://127.0.0.1:8645 --network testnet \
  stake create --wallet mywallet --amount 1000
bin/klingnet-cli --rpc http://127.0.0.1:8645 --network testnet \
  stake withdraw --wallet mywallet

# Create a sub-chain (burns 1,000 KGX, spawns a new chain when confirmed)
bin/klingnet-cli --rpc http://127.0.0.1:8645 --network testnet subchain create \
  --wallet mywallet --name "GameChain" --symbol "GAME" \
  --consensus poa --validators <pubkey_hex> \
  --block-reward 0.01 --max-supply 500000
```

**Global flags:**
- `--rpc <url>` — RPC endpoint (default: `http://127.0.0.1:8545`)
- `--datadir <path>` — Data directory (default: `~/.klingnet`)
- `--network <name>` — Network name: `mainnet` (default) or `testnet`

**Wallet create flow:**
1. Generates a 24-word BIP-39 mnemonic
2. Prompts for password (twice, no echo)
3. Encrypts seed with Argon2id + XChaCha20-Poly1305
4. Derives account 0 address via BIP-32 (m/44'/8888'/0'/0/0)
5. Stores encrypted wallet + address metadata in keystore

**Transaction send flow:**
1. Prompts for wallet password (no echo)
2. Decrypts wallet seed → derives wallet keys via BIP-32
3. Collects UTXOs across all known wallet addresses (external + change)
4. Runs coin selection (single-match + largest-first)
5. Builds transaction with change output if needed
6. Signs with Schnorr/secp256k1
7. Submits via `tx_submit` RPC

**Wallet behavior notes:**
- Wallet send/stake/sub-chain send/sub-chain create and token-fee coin selection now use only spendable native KGX UTXOs (exclude token UTXOs, immature coinbase, and locked outputs).
- CLI `wallet balance` reports spendable balance and, when applicable, also shows total/immature/locked breakdown.
- `wallet consolidate` creates a consolidation tx (single output) to reduce UTXO fragmentation; supports root and sub-chains via `--chain-id`.
- `wallet rescan` supports deep scans for exchange-style wallets with many addresses:
  - `--derive-limit <N>` max derived addresses per chain (external/change)
  - `--timeout <sec>` per-request RPC timeout for long scans
- RPC/GUI long rescan requests use extended timeouts to avoid 10s client timeouts.
- QT Receive tab lists deposit addresses by default and can optionally show change addresses.
- QT Send and Sub-Chain Send pages include a `Consolidate Small UTXOs` action.

**Example output:**
```
$ bin/klingnet-cli --rpc http://127.0.0.1:8645 status
Chain:   klingnet-cli-test
Height:  3
Tip:     a3f1b2c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2
Peers:   0

$ bin/klingnet-cli --rpc http://127.0.0.1:8645 balance kgx1eyw09f9sww0cpn9hm0lklqa68dz4tqj4txhqvy
Address: kgx1eyw09f9sww0cpn9hm0lklqa68dz4tqj4txhqvy
Balance: 200000.000003000000
```

### End-to-End CLI Test

A test script that builds both binaries, starts a node, and tests all non-interactive CLI commands:

```bash
./scripts/test-cli.sh
```

This creates a temporary testnet, generates a validator key, starts `klingnetd` with mining, and runs all query commands against the live node.

## Economics

All on-chain values are stored in base units. 1 coin = 10^12 base units.

| Parameter | Value | Notes |
|-----------|-------|-------|
| Symbol | KGX | Native coin |
| Decimals | 12 | 1 coin = 1,000,000,000,000 base units |
| Max supply | 2,000,000 KGX | Fixed cap |
| Block reward | 0.02 KGX | Paid to validator (coinbase) |
| Min tx fee | 0.000001 KGX | Protocol-enforced minimum |
| Validator stake | 2,000 KGX (mainnet) | Min stake to become validator |
| Sub-chain burn | 1,000 KGX | Burn to register a sub-chain |
| Token creation fee | 50 KGX | Min fee for mint transactions |
| Coinbase maturity | 20 blocks | Coinbase outputs locked for 20 confirmations |
| Unstake cooldown | 20 blocks | Returned coins locked after unstaking |
| Halving | None (configurable) | `halving_interval: 0` means no halving |
| Fee model | Implicit (Bitcoin-style) | fee = sum(inputs) - sum(outputs) |
| Validator income | Block reward + tx fees | Both go into coinbase output |

Denomination helpers in `config/genesis.go`:
```go
config.Coin      // 1_000_000_000_000 (10^12)
config.MilliCoin // 1_000_000_000     (10^9)
config.MicroCoin // 1_000_000         (10^6)
```

At 0.02 coins/block with 3-second blocks, the 2M supply takes ~9.5 years to fully emit through block rewards alone.

## Architecture

### Protocol Rules vs Node Settings

This is the most important architectural distinction.

**Protocol rules** are defined in `genesis.json` and are immutable after chain launch. All nodes must agree on these — changing them requires a hard fork:
- Consensus type (PoA/PoW), block time, validators
- Max supply, block reward, halving interval, min fee
- Sub-chain limits (max depth, anchor interval)
- Token rules

**Node settings** are in `klingnet.conf` or CLI flags and can vary per node:
- `--mine` / `--coinbase` / `--validator-key`
- P2P port, seed nodes, max peers
- RPC address/port, log level

**Rule of thumb:** If two nodes have different values, will they reject each other's blocks? Yes = protocol rule. No = node setting.

### Consensus

**Root chain: Proof of Authority (PoA)** with Schnorr/secp256k1 signatures:
- Validators listed by public key in genesis
- Each block sealed with the validator's Schnorr signature
- 3-second target block time
- Validator staking: lock 2,000 KGX to `ScriptTypeStake` UTXO to become a validator
- Unstaking: spend all stake UTXOs, returned coins locked for 20 blocks (cooldown), validator removed from set
- Genesis validators are always trusted (no stake required, cannot be removed)
- Deterministic random validator selection: `BLAKE3(prevHash || height)` seeds selection
- Selected validator produces immediately; non-selected wait 2x block time (grace period)
- Any active staker can produce (chain never stalls if selected validator is offline)

**Sub-chains: Configurable (PoA or PoW)**
- PoW uses BLAKE3 hash-target: `BLAKE3(header) <= MaxUint256 / Difficulty`
- Difficulty is stored in each block header (consensus-enforced, like Bitcoin's nBits)
- Bitcoin-style difficulty retargeting: every `difficulty_adjust` blocks, difficulty is recalculated from timestamps
- Clamped to [0.25x, 4x] per adjustment period, minimum difficulty of 1
- Sub-chain creator chooses consensus, block time, rewards, supply, validators/difficulty, and adjustment interval

### UTXO Model

Bitcoin-style unspent transaction outputs:
- Inputs reference previous outputs by `(TxID, Index)`
- Outputs lock value to a script (P2PKH, P2SH, Mint, Burn, etc.)
- Fees are implicit: `sum(input values) - sum(output values)`
- Address = first 20 bytes of `BLAKE3(compressed_pubkey)`, displayed as Bech32 (`kgx1...`)

### Cryptography

| Purpose | Algorithm |
|---------|-----------|
| Hashing | BLAKE3 (32-byte) |
| Signatures | EC-Schnorr-DCRv0 over secp256k1 |
| Key derivation | BIP-32 HD (path: `m/44'/8888'/account'/change/index`) |
| Mnemonic | BIP-39 (24 words, 256-bit entropy) |
| Wallet encryption | Argon2id + XChaCha20-Poly1305 |

### Sub-chains

Anyone can create a sub-chain by burning 1,000 KGX on the root chain. The sub-chain is an independent blockchain with its own consensus, token, and economics.

**Model:** Flat — root chain + sub-chains only (no sub-sub-chains, `MaxDepth=1`).

**Registration flow:**
1. Creator builds a transaction with a `ScriptTypeRegister` output (value = 1,000 KGX burn, data = JSON config)
2. Config specifies: name, symbol, consensus (PoA/PoW), block time, block reward, max supply, min fee, validators or difficulty
3. When the tx is confirmed in a block, the node's sub-chain manager automatically:
   - Derives a unique `ChainID = BLAKE3(txHash || outputIndex)`
   - Creates isolated storage via `PrefixDB` (`sc/<chainID_hex>/` prefix in the same BadgerDB)
   - Builds a genesis config and spawns the chain instance (chain + UTXO store + mempool + consensus engine)
4. The 1,000 KGX is burned (unspendable `ScriptTypeRegister` output)

**Dynamic Validators (PoA):** PoA sub-chains can enable dynamic validator staking by setting `validator_stake` > 0 in the registration config. When enabled, anyone can lock sub-chain coins in `ScriptTypeStake` UTXOs to join as a validator. The original genesis validators are always trusted. When a staker fully withdraws, they are removed from the validator set. On node restart, staked validators are automatically recovered from the UTXO store.

**Persistence:** Sub-chain registry is persisted to the parent DB. On node restart, all previously registered sub-chains are re-spawned automatically.

**Anchoring:** Sub-chains can anchor state commitments to the root chain via `ScriptTypeAnchor` outputs (72 bytes: ChainID + StateRoot + Height).

### Protocol Upgrades (Fork Schedule)

Klingnet uses a **fork schedule** in `genesis.json` to coordinate protocol upgrades without requiring simultaneous node restarts.

**How it works:**
1. A new fork is defined as a field in `ForkSchedule` with a block height
2. The new binary is released with logic gated behind `forks.IsActive(forkHeight, currentHeight)`
3. Validators update their nodes before the activation height
4. At the specified height, the new rules activate automatically

**Example** (future fork in genesis.json):
```json
{
  "protocol": {
    "forks": {
      "script_engine_height": 500000
    }
  }
}
```

**Block versioning:** Block validation accepts versions in the range `[1, MaxVersion]` rather than requiring an exact match. When a fork introduces new block semantics, `MaxVersion` is bumped to allow higher-version blocks.

### Bootnodes

Bootnodes are long-running seed nodes that help new peers join the network via DHT discovery.

**Setting up a bootnode:**
```bash
# Start a node in DHT server mode (recommended for bootnodes)
klingnetd --network=mainnet --dht-server
```

**Connecting to bootnodes:**
```bash
# Via --seeds flag (multiaddr format)
klingnetd --seeds "/ip4/203.0.113.1/tcp/30303/p2p/12D3KooW...,/dns4/seed1.klingnet.io/tcp/30303/p2p/12D3KooW..."
```

**DHT network isolation:** Each network (mainnet, testnet) uses a unique DHT rendezvous namespace derived from the chain ID (e.g., `klingnet/klingnet-mainnet-1`). This prevents mainnet and testnet nodes from discovering each other via DHT.

### P2P Networking

Built on libp2p:
- **Transport:** TCP with noise encryption
- **Pub/sub:** GossipSub for tx and block gossip
- **Discovery:** mDNS (local) + Kademlia DHT (wide-area)
- **Sync:** Custom stream protocol (`/klingnet/sync/1.0.0`) for block range requests
- **Height:** Custom stream protocol (`/klingnet/height/1.0.0`) for height queries
- **Peer persistence:** Peer records saved to BadgerDB, restored on restart (max 500, prune stale >24h)
- **Heartbeat:** GossipSub topic `/klingnet/heartbeat/1.0.0` for validator liveness (60s signed pings)
- **Topics:** `/klingnet/tx/1.0.0`, `/klingnet/block/1.0.0`, `/klingnet/heartbeat/1.0.0`

## CLI Flags

### `klingnetd` (Full Node)

```
Commands:
  --help, -h          Show help
  --version, -v       Show version

Core:
  --network           mainnet (default) or testnet
  --testnet           Shorthand for --network=testnet
  --datadir           Data directory (default: ~/.klingnet)
  --config, -c        Config file path

P2P:
  --p2p-port          Listen port (mainnet: 30303, testnet: 30304)
  --seeds             Seed nodes (comma-separated)
  --maxpeers          Max peers (default: 50)
  --nodiscover        Disable mDNS + DHT discovery
  --dht-server        Run DHT in server mode (for seeds/validators)
  --wallet            Enable wallet RPC endpoints

Mining/Validation:
  --mine              Enable block production
  --coinbase          Reward address (bech32 or hex)
  --validator-key     Path to validator private key file

Sub-chains:
  --sync-subchains    Which sub-chains to sync (all/none/comma-separated IDs)
  --mine-subchains    PoW sub-chain IDs to mine (comma-separated hex IDs)

RPC:
  --rpc-addr          RPC listen address (default: 127.0.0.1)
  --rpc-port          RPC listen port (mainnet: 8545, testnet: 8645)

Logging:
  --log-level         debug, info, warn, error (default: info)
  --log-file          Log to file instead of stdout
  --log-json          JSON log format
```

### `klingnet-cli` (CLI Client)

```
Global flags:
  --rpc               RPC endpoint URL (default: http://127.0.0.1:8545)
  --datadir           Data directory (default: ~/.klingnet)
  --network           mainnet (default) or testnet

Query commands:
  status              Chain height, tip hash, peer count
  block <hash|height> Block details by hash or height
  tx <hash>           Transaction details
  balance <address>   Address balance (human-readable)
  mempool             Pending transaction count and hashes
  peers               Node ID, listen addresses, connected peers
  validators          Show validator list and stake status

Wallet commands:
  wallet create       Create new wallet (--name <name>)
  wallet import       Import from mnemonic (--name <name>)
  wallet list         List wallets in keystore
  wallet address      Show wallet address (--wallet <name>)
  wallet new-address  Derive next HD address (--wallet <name>)
  wallet consolidate  Consolidate many small UTXOs (--wallet <name>)
  wallet rescan       Re-derive/scan wallet addresses (--wallet <name>)
  wallet balance      Show wallet balance (--wallet <name>)
  wallet export-key   Export private key (--wallet <name>)

Transaction commands:
  send                Build, sign, submit tx (--wallet, --to, --amount)
  sendmany            Multi-output tx from JSON file (--wallet, --recipients)
  tx send             Same as send (backward compat)

Staking commands:
  stake info <pubkey> Show stake info for a validator pubkey
  stake create        Create staking tx (--wallet <name>, --amount <amt>)
  stake withdraw      Withdraw all stake (--wallet <name>)

Sub-chain commands:
  subchains           List registered sub-chains
  subchain info <id>  Show sub-chain details
  subchain create     Create a new sub-chain (burns 1,000 KGX)

send flags:
  --wallet <name>     Wallet to sign with (required)
  --to <address>      Recipient address (required)
  --amount <amt>      Amount in coins, e.g. "10.5" (required)

sendmany flags:
  --wallet <name>         Wallet to sign with (required)
  --recipients <file>     Path to JSON file with recipients (required)
    JSON format: [{"to": "<address>", "amount": "1.5"}, ...]

subchain create flags:
  --wallet <name>       Wallet to pay burn fee from (required)
  --name <chain_name>   Sub-chain name, 1-64 chars (required)
  --symbol <SYM>        Token symbol, 2-10 uppercase chars (required)
  --consensus <type>    poa (default) or pow
  --block-time <secs>   Block time in seconds (default: 3)
  --block-reward <amt>  Block reward in coins (default: 0.001)
  --max-supply <amt>    Max supply in coins (default: 1000000)
  --min-fee <amt>       Min tx fee in coins (default: 0.000001)
  --burn <amt>          KGX to burn (default: 50, min 50)
  --validators <keys>   Comma-separated pubkey hex (poa only)
  --validator-stake <amt> Min stake for dynamic validators (poa only, 0=disabled)
  --difficulty <n>      Initial difficulty (pow only, default: 1000)
  --difficulty-adjust <n> Blocks between adjustments (pow only, 0=disabled, min 10)

wallet rescan flags:
  --wallet <name>      Wallet name (required)
  --from-height <N>    Start scanning from block height (default: 0)
  --derive-limit <N>   Max derived addresses per chain (default: auto)
  --timeout <sec>      RPC timeout for this rescan request (default: 600)
  --chain-id <hex>     Optional sub-chain ID to scan instead of root chain

wallet consolidate flags:
  --wallet <name>      Wallet name (required)
  --max-inputs <N>     Max UTXOs to merge in one tx (default: 500)
  --chain-id <hex>     Optional sub-chain ID to consolidate instead of root chain
```

## Project Structure

```
klingnet-chain/
├── cmd/
│   ├── klingnetd/              # Full node daemon
│   ├── klingnet-cli/           # CLI client (wallet + query commands)
│   ├── klingnet-qt/            # Desktop GUI (Wails v2 + React)
│   └── testnet/               # 2-node local testnet launcher
├── pkg/                       # Public API
│   ├── types/                 # Hash, Address, Outpoint, Script, TokenData
│   ├── tx/                    # Transaction, Builder, validation
│   ├── block/                 # Block, Header, Merkle, validation
│   └── crypto/                # BLAKE3, Schnorr, key generation
├── internal/                  # Private implementation
│   ├── chain/                 # Chain state, genesis, block processing
│   ├── consensus/             # PoA + PoW engines, block validator
│   ├── mempool/               # Tx pool, fee-rate ordering, eviction
│   ├── miner/                 # Block producer, coinbase, UTXO adapter
│   ├── p2p/                   # libp2p node, gossip, sync, discovery
│   ├── rpcclient/             # JSON-RPC 2.0 client library
│   ├── rpc/                   # JSON-RPC 2.0 server
│   ├── storage/               # BadgerDB + MemoryDB + PrefixDB backends
│   ├── utxo/                  # UTXO set, address index, commitments
│   ├── token/                 # Token validation + metadata store
│   ├── wallet/                # HD wallet, mnemonic, keystore, encryption
│   ├── log/                   # Structured logging (zerolog)
│   └── subchain/              # Sub-chain registration, spawning, anchoring, manager
├── config/                    # Genesis rules + node config
├── scripts/                   # Build/test scripts
│   ├── test-cli.sh            # End-to-end CLI test against live node
│   ├── start-testnet.sh       # 3-node manual testnet launcher
│   └── derive_key.go          # Helper: derive pubkey/address from key file
├── theory.txt                 # Original architecture discussion
├── TODO.md                    # Development task tracking
├── STRUCTURE.md               # Detailed directory rationale
└── CLAUDE.md                  # AI assistant instructions
```

## Development

```bash
# Run all tests (~500+ tests across 17 packages)
go test ./...

# Run tests verbose
make test

# Format + vet + test
make check

# Build both binaries
go build -o bin/klingnetd ./cmd/klingnetd
go build -o bin/klingnet-cli ./cmd/klingnet-cli

# Cross-compile static builds for all platforms
make release
# Outputs: dist/{linux,darwin,windows}-{amd64,arm64}/

# Build a single platform
make build-linux-amd64
make build-darwin-arm64
make build-windows-amd64

# Build QT (must be on target platform with SDK)
make build-qt-linux   # Requires webkit2gtk4.1-devel
make build-qt-darwin  # Requires Xcode CLI tools
make build-qt-windows # Requires WebView2 runtime

# Run the 2-node testnet (integration smoke test)
go run ./cmd/testnet/

# Run end-to-end CLI test against a live node
./scripts/test-cli.sh

# Run a specific test
go test -run TestMiner_ProduceBlock ./internal/miner/
```

## Dependencies

| Purpose | Library |
|---------|---------|
| Hashing | `github.com/zeebo/blake3` |
| Signatures | `github.com/decred/dcrd/dcrec/secp256k1/v4` + `/schnorr` |
| Storage | `github.com/dgraph-io/badger/v4` |
| Networking | `github.com/libp2p/go-libp2p` + `go-libp2p-pubsub` |
| Mnemonic | `github.com/tyler-smith/go-bip39` |
| HD keys | `github.com/tyler-smith/go-bip32` |
| Logging | `github.com/rs/zerolog` |
| Encryption | `golang.org/x/crypto` (argon2, chacha20poly1305) |
| Terminal | `golang.org/x/term` (password input without echo) |

## Networks

| Network | Chain ID | P2P Port | RPC Port |
|---------|----------|----------|----------|
| Mainnet | klingnet-mainnet-1 | 30303 | 8545 |
| Testnet | klingnet-testnet-1 | 30304 | 8645 |

## Roadmap

- [x] Wire `klingnetd` daemon (BadgerDB, chain, mempool, P2P, block production)
- [x] RPC server (JSON-RPC API — 38 endpoints for chain, UTXO, tx, mempool, net, stake, subchain, wallet, token)
- [x] CLI tool (`klingnet-cli` — 17 commands, wallet management, staking, transaction sending, sub-chains)
- [x] Desktop GUI (`klingnet-qt` — Wails v2 + React, 14 pages)
- [x] Validator staking + unstaking (lock/unlock coins, auto-register/remove validators, 20-block cooldown)
- [x] Supply cap enforcement in block producer
- [x] Chain reorganization handling (undo log, fork detection, rollback + replay)
- [x] PoW consensus engine (BLAKE3 hash-target, configurable per sub-chain)
- [x] Sub-chain system (burn 1,000 KGX to create, PrefixDB isolation, PoA/PoW, anchoring)
- [x] Bech32 addresses (`kgx1...` mainnet, `tkgx1...` testnet, BIP-173 checksum)
- [x] Coinbase maturity (20 blocks)
- [x] Deterministic random validator selection (BLAKE3-seeded from prevHash + height)
- [x] Token validation wiring (chain + mempool enforcement)
- [x] Token creation fee (50 KGX minimum)
- [x] `wallet_mintToken` RPC endpoint
- [x] `wallet_stake` + `wallet_unstake` RPC endpoints
- [x] Kademlia DHT peer discovery + peer persistence
- [x] Chain sync protocol + startup sync (height query, batch download)
- [x] Change address tracking (HD internal chain, auto-saved to wallet)
- [x] 3-node manual testnet script (`scripts/start-testnet.sh`)
- [x] PoW difficulty adjustment for sub-chains (Bitcoin-style retargeting)
- [x] Cross-compilation (static builds for Linux/macOS/Windows)
- [x] `mining_getBlockTemplate` + `mining_submitBlock` RPC for external PoW miners
- [x] Sub-chain PoA dynamic validators (stake-based validator join/leave)
- [x] Per-network DHT isolation (mainnet/testnet nodes don't cross-discover)
- [x] Fork activation schedule (ForkSchedule in genesis)
- [x] Block version range check (future-proof for protocol upgrades)
- [x] Hash fields in block/tx RPC responses (for block explorer / indexer integration)
- [x] Validator heartbeat + liveness tracking (`validator_getStatus` RPC, GossipSub heartbeat protocol)
- [x] Multi-output transactions (`wallet_sendMany` RPC, `sendmany` CLI command, SendMany QT page)
- [ ] Light client / SPV support (deferred)
- [ ] Script evaluation engine (deferred)

## License

MIT

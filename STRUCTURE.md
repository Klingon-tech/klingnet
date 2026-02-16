# Klingnet Chain - Project Structure

```
klingnet-chain/
├── cmd/
│   ├── klingnetd/              # Full node daemon
│   │   └── main.go
│   ├── klingnet-cli/           # Command-line wallet/tools
│   │   └── main.go
│   ├── klingnet-qt/            # GUI wallet (Wails v2 + React)
│   │   ├── main.go            # Wails entry, embed frontend/dist
│   │   ├── app.go             # App lifecycle, RPC endpoint, settings
│   │   ├── wallet_service.go  # Wallet CRUD, send tx, balance
│   │   ├── chain_service.go   # Chain/block/tx queries via RPC
│   │   ├── network_service.go # Peers, mempool, node info
│   │   ├── staking_service.go # Validator staking queries
│   │   ├── subchain_service.go# Sub-chain queries
│   │   ├── helpers.go         # Amount formatting, address validation
│   │   ├── helpers_test.go    # Unit tests
│   │   ├── wails.json         # Wails project config
│   │   └── frontend/          # React TypeScript (Vite)
│   │       ├── src/
│   │       │   ├── App.tsx, App.css
│   │       │   ├── components/ (Layout, Dashboard, Wallet, Explorer, ...)
│   │       │   ├── hooks/      (usePolling, useChain, useWallet)
│   │       │   └── utils/      (format.ts, types.ts)
│   │       └── wailsjs/       # Wails Go binding stubs
│   │
│   └── testnet/               # 2-node local testnet launcher
│       └── main.go
│
├── pkg/                       # Public library (importable by others)
│   ├── types/                 # Core primitives
│   │   ├── hash.go
│   │   ├── address.go
│   │   ├── outpoint.go
│   │   ├── script.go
│   │   └── token.go
│   │
│   ├── tx/                    # Transactions
│   │   ├── transaction.go
│   │   ├── input.go
│   │   ├── output.go
│   │   ├── builder.go         # Transaction construction
│   │   └── validate.go
│   │
│   ├── block/                 # Blocks
│   │   ├── block.go
│   │   ├── header.go
│   │   ├── merkle.go
│   │   └── validate.go
│   │
│   └── crypto/                # Crypto primitives for external use
│       ├── hash.go            # BLAKE3 wrapper
│       └── signature.go       # Sign/verify interface
│
├── internal/                  # Private implementation
│   ├── wallet/
│   │   ├── mnemonic.go        # BIP-39 generation/validation
│   │   ├── seed.go            # Seed derivation
│   │   ├── hdkey.go           # HD key derivation
│   │   ├── keystore.go        # Encrypted key storage
│   │   ├── encryption.go      # Argon2 + XChaCha20
│   │   ├── account.go         # Account management
│   │   ├── balance.go         # UTXO tracking per wallet
│   │   └── coinselect.go      # UTXO selection algorithms
│   │
│   ├── utxo/
│   │   ├── set.go             # UTXO set interface
│   │   ├── store.go           # Storage implementation
│   │   ├── index.go           # Indexing logic
│   │   └── commitment.go      # Merkle commitment
│   │
│   ├── chain/
│   │   ├── chain.go           # Main Chain struct
│   │   ├── genesis.go         # Genesis block creation
│   │   ├── state.go           # Chain state management
│   │   ├── processor.go       # Block processing pipeline
│   │   └── reorg.go           # Reorganization handling
│   │
│   ├── consensus/
│   │   ├── engine.go          # Consensus interface
│   │   ├── pow.go             # Proof of Work (BLAKE3 hash-target)
│   │   ├── poa.go             # Proof of Authority (Schnorr signatures)
│   │   ├── stake.go           # UTXO stake checker
│   │   └── validator.go       # Block validation rules
│   │
│   ├── mempool/
│   │   ├── pool.go            # Transaction pool
│   │   ├── policy.go          # Acceptance policy
│   │   └── eviction.go        # Eviction strategy
│   │
│   ├── p2p/
│   │   ├── node.go            # P2P node
│   │   ├── peer.go            # Peer connection
│   │   ├── protocol.go        # Message protocol
│   │   ├── discovery.go       # Peer discovery
│   │   ├── gossip.go          # Transaction/block gossip
│   │   └── sync.go            # Chain synchronization
│   │
│   ├── token/
│   │   ├── token.go            # TokenID derivation
│   │   ├── validate.go         # Token conservation, mint fee
│   │   ├── store.go            # Token metadata persistence
│   │   └── adapter.go          # UTXO → token adapter
│   │
│   ├── subchain/
│   │   ├── registry.go        # Sub-chain registry with persistence
│   │   ├── registration.go    # RegistrationData types, validation, ChainID derivation
│   │   ├── validate.go        # Registration tx validation
│   │   ├── anchor.go          # Anchor transaction encoding/decoding/validation
│   │   ├── spawn.go           # Sub-chain spawning (PrefixDB, genesis, engine)
│   │   └── manager.go         # Multi-chain lifecycle coordination
│   │
│   ├── rpc/
│   │   ├── server.go          # RPC server + dispatch
│   │   ├── handlers.go        # Chain/UTXO/tx/mempool/net/stake/subchain handlers
│   │   ├── wallet_handlers.go # Wallet RPC handlers (create/send/stake/unstake/mint)
│   │   └── types.go           # API types
│   │
│   ├── rpcclient/
│   │   ├── client.go          # JSON-RPC 2.0 client
│   │   └── client_test.go     # Client tests
│   │
│   └── storage/
│       ├── db.go              # Database interface
│       ├── badger.go          # Badger implementation
│       ├── memory.go          # In-memory (for tests)
│       └── prefix.go          # PrefixDB (namespace isolation for sub-chains)
│
├── config/
│   ├── config.go              # Configuration struct
│   ├── defaults.go            # Default values
│   └── genesis.json           # Genesis configuration
│
├── scripts/                   # Build/test scripts
│   ├── test-cli.sh            # End-to-end CLI test
│   ├── start-testnet.sh       # 3-node manual testnet launcher
│   └── derive_key.go          # Derive pubkey/address from key file
│
├── docs/                      # Documentation
│
├── testdata/                  # Test fixtures
│
├── go.mod
├── go.sum
├── Makefile
├── README.md
├── TODO.md
├── STRUCTURE.md
└── theory.txt
```

---

## Directory Rationale

### `cmd/` - Executables

| Binary | Purpose |
|--------|---------|
| `klingnetd` | Full node daemon. Runs chain, P2P, RPC server, mempool. |
| `klingnet-cli` | CLI for interacting with a running node. Send transactions, query state. |
| `klingnet-qt` | GUI wallet (Wails v2 + React). Connects to klingnetd via RPC. |

### `pkg/` - Public API

Code that external projects can import:
- **Light clients** need `pkg/types`, `pkg/block`, `pkg/tx`
- **Block explorers** need the same
- **External wallets** need `pkg/crypto` for signing

Keep this minimal and stable — breaking changes affect downstream users.

### `internal/` - Private Implementation

Everything else. Go enforces that `internal/` packages cannot be imported from outside the module. This gives you freedom to refactor without breaking external code.

| Package | Responsibility |
|---------|----------------|
| `wallet/` | Key management, encryption, coin selection |
| `utxo/` | UTXO set storage and indexing |
| `chain/` | Chain state, block processing, reorgs |
| `consensus/` | Consensus engines (PoA, PoW), validator staking |
| `mempool/` | Transaction pool management |
| `p2p/` | Networking, peer management, sync |
| `token/` | Token validation, metadata, creation fee |
| `subchain/` | Sub-chain registration, spawning, anchoring, lifecycle |
| `rpc/` | JSON-RPC 2.0 API server |
| `rpcclient/` | JSON-RPC 2.0 client (used by CLI) |
| `storage/` | Database abstraction (BadgerDB, MemoryDB, PrefixDB) |

### `config/`

Runtime configuration. Separate from code so it can be modified without recompiling.

---

## Package Dependencies

```
cmd/klingnetd
    ├── internal/chain
    │   ├── internal/utxo
    │   ├── internal/consensus
    │   ├── internal/mempool
    │   └── internal/subchain
    ├── internal/p2p
    ├── internal/rpc
    ├── internal/storage
    └── pkg/* (types, tx, block, crypto)

cmd/klingnet-cli
    ├── internal/rpcclient
    ├── internal/subchain (RegistrationData type)
    ├── internal/wallet
    └── pkg/* (types, tx, crypto)

cmd/klingnet-qt
    ├── internal/rpcclient
    ├── internal/rpc (types)
    ├── internal/wallet
    └── pkg/* (types, tx, crypto)

```

Key principle: **dependencies flow inward**. `pkg/types` depends on nothing. `internal/chain` depends on `pkg/types` but not vice versa.

---

## File Naming Conventions

| Pattern | Example | Purpose |
|---------|---------|---------|
| `thing.go` | `block.go` | Main type definition |
| `thing_test.go` | `block_test.go` | Tests for that file |
| `validate.go` | `validate.go` | Validation logic |
| `builder.go` | `builder.go` | Construction/builder pattern |
| `interface.go` or `engine.go` | `engine.go` | Interface definitions |

---

## Test Organization

Tests live alongside the code they test:

```
internal/wallet/
├── mnemonic.go
├── mnemonic_test.go
├── encryption.go
└── encryption_test.go
```

Integration tests that span multiple packages go in a top-level `test/` directory if needed.

`testdata/` contains fixtures: sample blocks, transactions, wallet files for testing.

---

## Build Artifacts

```
bin/                    # Built binaries (gitignored)
├── klingnetd
├── klingnet-cli
```

Use a `Makefile` for build commands:

```makefile
.PHONY: build test clean

build:
	go build -o bin/klingnetd ./cmd/klingnetd
	go build -o bin/klingnet-cli ./cmd/klingnet-cli

test:
	go test ./...

clean:
	rm -rf bin/
```

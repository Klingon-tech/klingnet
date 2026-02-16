# Claude Instructions for Klingnet Chain

## Project Context

This is a UTXO-based blockchain written in Go with:
- Flat sub-chain architecture (root + sub-chains, no nesting, burn 50 KGX to create)
- PoA (root chain) + configurable PoA/PoW (sub-chains) consensus
- Simple token system (colored coins style)
- Cross-chain bridging via lock/mint mechanism (planned)
- HD wallet with Argon2 encryption
- Validator staking (lock KGX to become a validator)

Key files:
- `STRUCTURE.md` — Project folder organization
- `config/genesis.go` — Protocol rules (immutable)
- `config/config.go` — Node settings (runtime)
- `cmd/testnet/main.go` — 2-node local testnet launcher
- `cmd/klingnet-cli/main.go` — CLI tool (wallet + query + sub-chain commands)
- `cmd/klingnetd/main.go` — Full node daemon
- `internal/rpcclient/client.go` — JSON-RPC 2.0 client library
- `internal/subchain/manager.go` — Sub-chain lifecycle manager
- `scripts/test-cli.sh` — End-to-end CLI test script

---

## CRITICAL: Protocol Rules vs Node Settings

This is the most important architectural distinction. Getting it wrong breaks consensus.

### Protocol Rules (genesis.json)
**Immutable after chain launch. All nodes MUST agree.**

- Consensus type (PoA vs PoW)
- Block time
- Sub-chain max depth
- Anchor interval
- Token rules
- Validator set (initial)

**These are NOT flags. They are hardcoded in genesis.**

### Node Settings (klingnet.conf / flags)
**Can vary per node. Operational choices.**

- `--mine` — Whether this node produces blocks
- `--coinbase` — Where rewards go
- `--rpc-port` — What port to listen on
- `--log-level` — How verbose to be

### Rule of Thumb
Ask: "If two nodes have different values, will they reject each other's blocks?"
- **Yes** → Protocol rule → Goes in genesis
- **No** → Node setting → Goes in config/flags

### Example Mistakes to Avoid

```go
// WRONG: Consensus type as a flag
fs.StringVar(&f.Consensus, "consensus", "poa", "Consensus type")

// RIGHT: Consensus type from genesis only
consensusType := genesis.Protocol.Consensus.Type
```

```go
// WRONG: Max depth as runtime config
cfg.SubChain.MaxDepth = 5

// RIGHT: Max depth from genesis
maxDepth := genesis.Protocol.SubChain.MaxDepth
```

---

## Before Writing Code

### 1. Check Existing Helpers

Before implementing any functionality, search for existing utilities:

```
pkg/crypto/        — Hash functions, signatures, AddressFromPubKey
pkg/types/         — Primitives (Hash, Address, Outpoint, Script)
internal/wallet/   — Key derivation, encryption, keystore, coin selection
internal/storage/  — Database operations (MemoryDB, BadgerDB, PrefixDB, ForEach)
internal/miner/    — Block production, UTXOAdapter (utxo.Set → tx.UTXOProvider)
internal/rpcclient — JSON-RPC 2.0 client (reusable outside CLI)
internal/rpc/      — JSON-RPC 2.0 server, handlers, response types
internal/subchain/ — Sub-chain registration, spawning, manager, anchoring
internal/consensus/ — PoA, PoW engines, stake checker
```

**Key helpers that already exist:**
- `crypto.AddressFromPubKey(pubKey) Address` — BLAKE3(pubkey)[:20]
- `crypto.Hash(data) Hash` — BLAKE3-256
- `miner.NewUTXOAdapter(utxo.Set)` — bridges utxo.Set to tx.UTXOProvider
- `miner.BuildCoinbase(addr, reward, height)` — creates coinbase tx with zero outpoint and height-encoded signature for unique tx hash
- `rpcclient.New(endpoint)` — creates RPC client, `client.Call(method, params, &result)` for RPC calls
- `wallet.SelectCoins(utxos, target)` — coin selection (single-match + largest-first)
- `wallet.NewKeystore(path)` — keystore for encrypted wallet files
- `wallet.AccountEntry{Index, Name, Address}` — exported struct for wallet account metadata

**Coinbase convention:** Coinbase inputs use a zero outpoint (`types.Outpoint{}`).
`tx.Validate()` and `tx.VerifySignatures()` skip zero-outpoint inputs (no sig/pubkey needed).

**Never duplicate:**
- Hash functions (use `pkg/crypto/hash.go`)
- Byte serialization (check if type already has `Bytes()` method)
- Validation helpers (look in `validate.go` files)
- Error types (check for existing error definitions)
- UTXO provider adapters (use `miner.UTXOAdapter`)

---

## Code Standards

### Go Idioms

```go
// Good: Return early on errors
func DoThing() error {
    if err := step1(); err != nil {
        return fmt.Errorf("step1: %w", err)
    }
    return step2()
}

// Bad: Deep nesting
func DoThing() error {
    if err := step1(); err == nil {
        if err := step2(); err == nil {
            // ...
        }
    }
}
```

### Error Handling

- Always wrap errors with context: `fmt.Errorf("operation: %w", err)`
- Define sentinel errors in the package that owns them
- Check errors immediately, don't defer error checking

### Naming

| Type | Convention | Example |
|------|------------|---------|
| Packages | lowercase, short | `utxo`, `tx`, `chain` |
| Interfaces | -er suffix when possible | `Validator`, `Signer` |
| Constructors | `New` prefix | `NewTransaction()` |
| Getters | No `Get` prefix | `block.Hash()` not `block.GetHash()` |

### File Organization

- One primary type per file (e.g., `block.go` defines `Block`)
- Keep files under 300 lines when possible
- Validation logic goes in `validate.go`
- Interfaces go at the top of files or in `interface.go`

---

## Security Checklist

### Always

- [ ] Zero sensitive memory after use (keys, seeds, passwords)
- [ ] Validate all inputs at system boundaries
- [ ] Use constant-time comparison for secrets
- [ ] Check integer overflow on arithmetic operations

### Never

- [ ] Log private keys, seeds, or mnemonics
- [ ] Use `math/rand` for cryptographic randomness (use `crypto/rand`)
- [ ] Trust data from peers without validation
- [ ] Hardcode keys, even for testing (use fixtures in `testdata/`)

---

## Testing Requirements

### MANDATORY: Every Implementation Needs Tests

**No code is considered complete without unit tests.** Write tests alongside the implementation, in the same commit. If you implement a function, you write its test. No exceptions.

### What to Test

- All public functions in `pkg/` and `internal/`
- Validation logic (valid and invalid inputs)
- Serialization/deserialization roundtrips
- Edge cases: empty inputs, zero values, max values, malformed data
- Error paths: ensure errors are returned when expected
- Cryptographic operations: use known test vectors when available

### Test File Location

Tests live next to the code they test:
```
pkg/crypto/hash.go               → pkg/crypto/hash_test.go
pkg/types/hash.go                → pkg/types/hash_test.go
internal/wallet/mnemonic.go      → internal/wallet/mnemonic_test.go
```

### Test Naming

```go
func TestTypeName_MethodName(t *testing.T) { }
func TestTypeName_MethodName_EdgeCase(t *testing.T) { }
```

### Table-Driven Tests

Prefer table-driven tests for functions with multiple input/output cases:

```go
func TestHash(t *testing.T) {
    tests := []struct {
        name  string
        input []byte
        want  types.Hash
    }{
        {"empty input", []byte{}, expectedHash},
        {"known vector", []byte("hello"), knownHash},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := crypto.Hash(tt.input)
            if got != tt.want {
                t.Errorf("Hash() = %x, want %x", got, tt.want)
            }
        })
    }
}
```

---

## Commit Habits

### Before Committing

1. Run `go fmt ./...`
2. Run `go vet ./...`
3. Run `go test ./...`

### Commit Message Format

```
component: short description

Longer explanation if needed.
- Detail 1
- Detail 2
```

Examples:
```
wallet: implement Argon2 key derivation
utxo: add merkle commitment generation
chain: fix reorg handling for depth > 1
```

---

## Dependencies

### Approved Libraries

| Purpose | Library |
|---------|---------|
| Hashing | `github.com/zeebo/blake3` |
| Storage | `github.com/dgraph-io/badger/v4` |
| Networking | `github.com/libp2p/go-libp2p` |
| BIP-39 | `github.com/tyler-smith/go-bip39` |
| BIP-32 | `github.com/tyler-smith/go-bip32` |
| Schnorr/secp256k1 | `github.com/decred/dcrd/dcrec/secp256k1/v4` + `/schnorr` |
| Logging | `github.com/rs/zerolog` (colored, structured) |
| Terminal | `golang.org/x/term` (password input without echo) |

### Stdlib Preferred

Use standard library when possible:
- `crypto/rand` — Cryptographic randomness
- `golang.org/x/crypto/argon2` — Password hashing
- `golang.org/x/crypto/chacha20poly1305` — Encryption
- `golang.org/x/term` — Password prompts (no echo)
- `encoding/json`, `encoding/binary` — Serialization

### Adding New Dependencies

1. Check if stdlib can do it
2. Check if existing dependency already provides it
3. Prefer well-maintained, focused libraries
4. Avoid dependencies with large transitive trees

---

## Common Patterns

### Interface Definition

```go
// Define interfaces where they're used, not where they're implemented
type UTXOStore interface {
    Get(outpoint types.Outpoint) (*UTXO, error)
    Put(utxo *UTXO) error
    Delete(outpoint types.Outpoint) error
    Has(outpoint types.Outpoint) (bool, error)
}
```

### Constructor Pattern

```go
func NewBlock(header *Header, txs []*Transaction) *Block {
    return &Block{
        Header:       header,
        Transactions: txs,
    }
}
```

### Options Pattern (for complex construction)

```go
type ChainOption func(*Chain)

func WithConsensus(c ConsensusEngine) ChainOption {
    return func(ch *Chain) { ch.consensus = c }
}

func NewChain(id ChainID, opts ...ChainOption) *Chain {
    ch := &Chain{ID: id}
    for _, opt := range opts {
        opt(ch)
    }
    return ch
}
```

---

## CLI Tool (`cmd/klingnet-cli/main.go`)

The CLI is a single-file binary (~900 lines) with no external CLI framework. Uses `os.Args` + `flag.NewFlagSet` per subcommand.

### Architecture
- **Global flags** (`--rpc`, `--datadir`) are parsed before subcommand dispatch via manual loop
- **RPC client** (`internal/rpcclient/client.go`) — thin HTTP JSON-RPC 2.0 client, `Call()` returns `*RPCError` for server errors
- **Wallet commands** are local-only (no RPC needed) — use `internal/wallet/` keystore
- **Transaction sending** uses the full wallet pipeline: Argon2 decrypt → BIP-32 HD derive → coin select → tx builder → Schnorr sign → RPC submit
- **Password input** via `golang.org/x/term.ReadPassword()` (no echo, reads from terminal fd)

### Key Patterns
```go
// RPC client usage
client := rpcclient.New("http://127.0.0.1:8645")
var result rpc.ChainInfoResult
if err := client.Call("chain_getInfo", nil, &result); err != nil {
    // err may be *rpcclient.RPCError (server error) or a network error
}

// Amount conversion (12 decimals)
raw := parseAmount("10.5")   // → 10_500_000_000_000
str := formatAmount(raw)      // → "10.500000000000"
```

### Testing
- `internal/rpcclient/client_test.go` — 6 tests against in-memory RPC server
- `scripts/test-cli.sh` — end-to-end test: builds binaries, starts node, runs all non-interactive commands

### Important Notes
- `wallet.AccountEntry` was exported (from `accountEntry`) so CLI can call `ks.AddAccount()`
- Password prompts require a real terminal fd — automated tests only cover non-interactive commands
- Seed bytes are zeroed immediately after key derivation (`for i := range seed { seed[i] = 0 }`)

---

## Testnet Launcher (`cmd/testnet/main.go`)

The testnet is the **integration smoke test** for the entire stack. Keep it working after every change.

```bash
# Run the 2-node testnet (produces 10 blocks, ~30s)
go run ./cmd/testnet/
```

**What it does:**
1. Generates a validator key + genesis config (PoA, 1 validator)
2. Boots 2 in-process nodes (MemoryDB, no disk cleanup needed)
3. Node-1 produces blocks, gossips via libp2p GossipSub to node-2
4. Verifies both chains converge at the same height + tip hash

**When to update the testnet:**
- Adding new consensus rules or validation checks
- Changing block structure, transaction format, or UTXO handling
- Modifying P2P gossip or chain processing
- Changing genesis config structure

**Components wired together:**
- `config.Genesis` → `chain.InitFromGenesis` → `chain.ProcessBlock`
- `consensus.PoA` → `miner.ProduceBlock` → `engine.Seal`
- `mempool.Pool` → `miner.SelectForBlock`
- `p2p.Node` → `BroadcastBlock` / `SetBlockHandler`
- `miner.UTXOAdapter` bridges `utxo.Set` to `tx.UTXOProvider`

**Key invariant:** After any code change, `go run ./cmd/testnet/` must print `SUCCESS: Both nodes converged`.

---

## Reminders

- [ ] Update `README.md` when adding new features or changing CLI
- [ ] Check for existing helpers before writing new ones
- [ ] **ALWAYS write unit tests with every implementation** (no exceptions)
- [ ] Keep functions small and focused
- [ ] Document exported functions in `pkg/`
- [ ] Run tests before pushing
- [ ] Ask if unsure about architecture decisions

---

## Documentation Requirements

### README.md

Keep `README.md` updated when:
- Adding new CLI flags or commands
- Changing configuration options
- Adding new features
- Modifying project structure
- Changing network defaults

### STRUCTURE.md

Update when:
- Adding new packages
- Reorganizing directories
- Adding new binaries

---

## Quick Reference

```bash
# Build all binaries
make build

# Run all tests
go test ./...

# Format code
go fmt ./...

# Check for issues
go vet ./...

# Run specific test
go test -run TestName ./path/to/package

# Run 2-node testnet (integration smoke test)
go run ./cmd/testnet/

# Run end-to-end CLI test (builds, starts node, tests commands)
./scripts/test-cli.sh
```

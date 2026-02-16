package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// =============================================================================
// Protocol Rules (immutable, defined in genesis)
// These MUST match across all nodes or consensus breaks.
// =============================================================================

// Consensus type constants.
const (
	ConsensusPoA = "poa" // Proof of Authority
	ConsensusPoW = "pow" // Proof of Work
)

// Denomination constants.
// 1 coin = 10^12 base units. All on-chain values are in base units.
const (
	Decimals  = 12
	Coin      = 1_000_000_000_000 // 10^12 base units per coin
	MilliCoin = 1_000_000_000     // 10^9
	MicroCoin = 1_000_000         // 10^6
)

// CoinbaseMaturity is the number of blocks a coinbase output must wait
// before it can be spent. Prevents issues during reorgs.
const CoinbaseMaturity uint64 = 20

// UnstakeCooldown is the number of blocks that unstake return outputs
// are locked before they can be spent. Prevents stake-and-withdraw attacks.
const UnstakeCooldown uint64 = 20

// TokenCreationFee is the minimum transaction fee (in base units) required
// for any transaction that mints new tokens.
const TokenCreationFee = 50 * Coin

// MaxTokenAmount is the maximum allowed amount for a single token output.
// Set to MaxUint64/1000 so that up to ~1000 UTXOs can be safely summed
// without overflowing uint64.
const MaxTokenAmount = math.MaxUint64 / 1000

// Block and transaction size limits (consensus-critical).
// These apply to both root chain and sub-chains.
const (
	MaxBlockSize  = 2_000_000 // 2 MB max block size (header + all tx signing bytes)
	MaxBlockTxs   = 500       // Max transactions per block (including coinbase)
	MaxTxInputs   = 2500      // Max inputs per transaction
	MaxTxOutputs  = 2500      // Max outputs per transaction
	MaxScriptData = 65_536    // 64 KB max script data per output
)

// Genesis holds the genesis block configuration and protocol rules.
// This is immutable after chain launch - changes require a hard fork.
type Genesis struct {
	// Chain identity
	ChainID   string `json:"chain_id"`
	ChainName string `json:"chain_name"`
	Symbol    string `json:"symbol,omitempty"` // Native coin symbol (e.g., "KGX")

	// Genesis block
	Timestamp uint64 `json:"timestamp"`
	ExtraData string `json:"extra_data,omitempty"`

	// Initial allocations (address -> balance in base units)
	Alloc map[string]uint64 `json:"alloc"`

	// Protocol rules
	Protocol ProtocolConfig `json:"protocol"`
}

// ForkSchedule defines block heights at which protocol upgrades activate.
// A zero value means the fork is not scheduled.
type ForkSchedule struct {
	// Future forks are added here as fields. Example:
	// ScriptEngineHeight uint64 `json:"script_engine_height,omitempty"`
}

// IsActive returns true if a fork at forkHeight has activated at currentHeight.
// Returns false if forkHeight is 0 (not scheduled).
func (f *ForkSchedule) IsActive(forkHeight, currentHeight uint64) bool {
	return forkHeight > 0 && currentHeight >= forkHeight
}

// ProtocolConfig holds consensus-critical rules.
// All nodes MUST agree on these values.
type ProtocolConfig struct {
	// Consensus
	Consensus ConsensusRules `json:"consensus"`

	// Sub-chains
	SubChain SubChainRules `json:"subchain"`

	// Tokens
	Token TokenRules `json:"token"`

	// Fork activation schedule
	Forks ForkSchedule `json:"forks,omitempty"`
}

// ConsensusRules defines how blocks are produced and validated.
type ConsensusRules struct {
	// Type: "poa" or "pow"
	Type string `json:"type"`

	// Block timing
	BlockTime int `json:"block_time"` // Target seconds between blocks

	// PoA settings
	Validators []string `json:"validators,omitempty"` // Initial validator public keys

	// PoW settings (only if Type == "pow")
	InitialDifficulty uint64 `json:"initial_difficulty,omitempty"`
	DifficultyAdjust  int    `json:"difficulty_adjust,omitempty"` // Blocks between adjustments

	// Economics
	BlockReward     uint64 `json:"block_reward"`               // Base units per block
	MaxSupply       uint64 `json:"max_supply"`                 // Total coin cap in base units (0 = unlimited)
	HalvingInterval uint64 `json:"halving_interval,omitempty"` // Blocks between reward halvings (0 = no halving)
	MinFeeRate      uint64 `json:"min_fee_rate"`                // Minimum fee rate (base units per byte of SigningBytes)

	// Staking
	ValidatorStake uint64 `json:"validator_stake,omitempty"` // Min stake to become validator (base units, 0 = no staking)
}

// SubChainRules defines sub-chain protocol limits.
type SubChainRules struct {
	// Whether sub-chains are enabled
	Enabled bool `json:"enabled"`

	// Maximum nesting depth (root = 0, first sub = 1, etc.)
	// Depth 5 means: root -> L1 -> L2 -> L3 -> L4 -> L5
	MaxDepth int `json:"max_depth"`

	// Maximum sub-chains per parent
	MaxPerParent int `json:"max_per_parent"`

	// Anchoring requirements
	AnchorInterval int  `json:"anchor_interval"` // Blocks between required anchors
	RequireAnchors bool `json:"require_anchors"` // Whether anchoring is mandatory
	AnchorTimeout  int  `json:"anchor_timeout"`  // Blocks before chain considered stale

	// Registration
	MinDeposit uint64 `json:"min_deposit"` // Minimum deposit to create sub-chain

	// Sub-chain consensus rules
	AllowPoW         bool   `json:"allow_pow"`         // Can sub-chains use PoW?
	DefaultConsensus string `json:"default_consensus"` // Default for new sub-chains
}

// TokenRules defines token protocol limits.
type TokenRules struct {
	// Maximum tokens per UTXO (0 = unlimited)
	MaxTokensPerUTXO int `json:"max_tokens_per_utxo"`

	// Whether tokens can be minted after genesis
	AllowMinting bool `json:"allow_minting"`
}

// =============================================================================
// Testnet Identity
//
// Derived from the well-known BIP-39 test mnemonic (DO NOT use on mainnet):
//
//	abandon abandon abandon abandon abandon abandon abandon abandon
//	abandon abandon abandon abandon abandon abandon abandon abandon
//	abandon abandon abandon abandon abandon abandon abandon art
//
// Derivation path: m/44'/8888'/0'/0/0 (no passphrase)
// =============================================================================

const (
	// TestnetMnemonic is the well-known seed phrase for the testnet validator.
	TestnetMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"

	// TestnetValidatorPubKey is the compressed public key (hex) derived from TestnetMnemonic.
	TestnetValidatorPubKey = "030bef68f8657df88098a0546da1712c88b459788bea1a6bbe964004166a25144f"

	// TestnetValidatorPrivKey is the private key (hex) derived from TestnetMnemonic.
	TestnetValidatorPrivKey = "1f0717e6e34acc6721021f4dfed54558ec8452452b6195545d06dd348b220091"

	// TestnetAddress is the address (bech32, tkgx) derived from TestnetMnemonic.
	// Address = BLAKE3(pubkey)[:20]
	TestnetAddress = "tkgx13uayfwq9djh7cd5dagxtuzk3mx7r7sc9xv4h52"
)

// =============================================================================
// Pre-defined genesis configurations
// =============================================================================

// MainnetGenesis returns the mainnet genesis configuration.
func MainnetGenesis() *Genesis {
	return &Genesis{
		ChainID:   "klingnet-mainnet-1",
		ChainName: "Klingnet Mainnet",
		Symbol:    "KGX",
		Timestamp: 1770734103, // 2026-02-10
		ExtraData: "Klingnet Genesis",
		Alloc: map[string]uint64{
			"kgx1a8tfl79jgres7t90tttkc7ytjmhs5lpdn5ag4l": 100_000 * Coin, // Genesis allocation for ERC-20 KGX swap
		},
		Protocol: ProtocolConfig{
			Consensus: ConsensusRules{
				Type:      ConsensusPoA,
				BlockTime: 3, // 3 second blocks
				Validators: []string{
					"03cba4d0ee4c55f5ea620393a6e6e9dafe959bfa6ddff964221126a3e41ad0487d",
				},
				BlockReward:     20 * MilliCoin,   // 0.02 coins per block
				MaxSupply:       2_000_000 * Coin, // 2,000,000 KGX total
				HalvingInterval: 0,                // No halving (configurable)
				MinFeeRate:      10_000,           // 10,000 base units per byte (~0.0000012 KGX for simple tx)
				ValidatorStake:  2000 * Coin,      // 2,000 KGX to become validator
			},
			SubChain: SubChainRules{
				Enabled:          true,
				MaxDepth:         1,      // Flat: root + sub-chains only (no sub-sub-chains)
				MaxPerParent:     10_000, // Max 10,000 sub-chains per parent
				AnchorInterval:   10,     // Anchor every 10 blocks
				RequireAnchors:   true,
				AnchorTimeout:    100,         // 100 blocks before stale
				MinDeposit:       1000 * Coin, // 1,000 KGX burn to create sub-chain
				AllowPoW:         true,        // Sub-chains can use PoW
				DefaultConsensus: ConsensusPoA,
			},
			Token: TokenRules{
				MaxTokensPerUTXO: 1, // One token type per UTXO
				AllowMinting:     true,
			},
		},
	}
}

// TestnetGenesis returns the testnet genesis configuration.
func TestnetGenesis() *Genesis {
	g := MainnetGenesis()
	g.ChainID = "klingnet-testnet-1"
	g.ChainName = "Klingnet Testnet"
	g.ExtraData = "Klingnet Testnet Genesis"

	// More relaxed rules for testnet.
	g.Protocol.SubChain.MinDeposit = Coin             // 1 coin deposit (much lower)
	g.Protocol.SubChain.MaxDepth = 10                 // Allow deeper nesting for testing
	g.Protocol.Consensus.MinFeeRate = 10              // 10 base units per byte (very low for testing)
	g.Protocol.Consensus.ValidatorStake = 1000 * Coin // Lower than mainnet (1,000 vs 2,000)

	// Testnet allocation: 200,000 KGX to the well-known testnet address.
	g.Alloc = map[string]uint64{
		TestnetAddress: 200_000 * Coin,
	}

	// Testnet validator: derived from the well-known mnemonic.
	g.Protocol.Consensus.Validators = []string{TestnetValidatorPubKey}

	return g
}

// GenesisFor returns the genesis config for the given network.
func GenesisFor(network NetworkType) *Genesis {
	switch network {
	case Testnet:
		return TestnetGenesis()
	default:
		return MainnetGenesis()
	}
}

// =============================================================================
// Genesis file I/O
// =============================================================================

// LoadGenesis loads genesis configuration from a file.
func LoadGenesis(path string) (*Genesis, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading genesis file: %w", err)
	}

	var g Genesis
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("parsing genesis file: %w", err)
	}

	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("invalid genesis: %w", err)
	}

	return &g, nil
}

// Save writes the genesis configuration to a file.
func (g *Genesis) Save(path string) error {
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding genesis: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing genesis file: %w", err)
	}

	return nil
}

// Validate checks that the genesis configuration is valid.
func (g *Genesis) Validate() error {
	if g.ChainID == "" {
		return fmt.Errorf("chain_id is required")
	}

	// Validate consensus
	switch g.Protocol.Consensus.Type {
	case ConsensusPoA:
		// PoA requires at least one validator
		// (Genesis may have zero, validators added via governance)
	case ConsensusPoW:
		if g.Protocol.Consensus.InitialDifficulty == 0 {
			return fmt.Errorf("pow requires initial_difficulty")
		}
	default:
		return fmt.Errorf("unknown consensus type: %s", g.Protocol.Consensus.Type)
	}

	if g.Protocol.Consensus.BlockTime <= 0 {
		return fmt.Errorf("block_time must be positive")
	}

	if g.Protocol.Consensus.BlockReward == 0 {
		return fmt.Errorf("block_reward must be positive")
	}

	// Validate alloc addresses and check total doesn't exceed max supply.
	var totalAlloc uint64
	for addrStr, v := range g.Alloc {
		if _, err := types.ParseAddress(addrStr); err != nil {
			return fmt.Errorf("invalid alloc address %q: %w", addrStr, err)
		}
		totalAlloc += v
	}
	if g.Protocol.Consensus.MaxSupply > 0 && totalAlloc > g.Protocol.Consensus.MaxSupply {
		return fmt.Errorf("genesis allocations (%d) exceed max_supply (%d)",
			totalAlloc, g.Protocol.Consensus.MaxSupply)
	}

	// Validate sub-chain rules (only when sub-chains are enabled).
	if g.Protocol.SubChain.Enabled {
		if g.Protocol.SubChain.MaxDepth < 1 || g.Protocol.SubChain.MaxDepth > 10 {
			return fmt.Errorf("max_depth must be between 1 and 10")
		}

		if g.Protocol.SubChain.MaxPerParent < 1 {
			return fmt.Errorf("max_per_parent must be at least 1")
		}

		if g.Protocol.SubChain.AnchorInterval < 1 {
			return fmt.Errorf("anchor_interval must be at least 1")
		}
	}

	return nil
}

// Hash returns a BLAKE3 hash of the genesis configuration.
// Used to identify the chain and detect genesis mismatches.
func (g *Genesis) Hash() (types.Hash, error) {
	data, err := json.Marshal(g)
	if err != nil {
		return types.Hash{}, err
	}
	return crypto.Hash(data), nil
}

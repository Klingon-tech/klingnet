// Package config handles application configuration.
//
// Configuration is split into two categories:
//   - Protocol rules: Defined in genesis, immutable, must match across all nodes
//   - Node settings: Runtime configuration, can vary per node
package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// NetworkType identifies mainnet or testnet.
type NetworkType string

const (
	Mainnet NetworkType = "mainnet"
	Testnet NetworkType = "testnet"
)

// =============================================================================
// Node Configuration (runtime, per-node settings)
// =============================================================================

// Config holds node-specific runtime configuration.
// These settings can vary between nodes without breaking consensus.
type Config struct {
	// Core
	Network NetworkType `conf:"network"`
	DataDir string      `conf:"datadir"`

	// P2P networking
	P2P P2PConfig

	// RPC server
	RPC RPCConfig

	// Wallet
	Wallet WalletConfig

	// Mining/Validation (operational, not consensus rules)
	Mining MiningConfig

	// Sub-chain sync (operational — which sub-chains to run locally)
	SubChainSync SubChainSyncConfig

	// Sub-chain mining (operational — which PoW sub-chains to mine)
	SubChainMineIDs []string // Hex chain IDs to mine (max MaxSubChainMiners)

	// Logging
	Log LogConfig

	// Maintenance (not persisted in config file)
	RebuildIndexes bool
}

// MaxSubChainMiners is the hard cap on concurrent sub-chain PoW miners.
// Each miner is CPU-intensive, so unlimited mining would be catastrophic.
const MaxSubChainMiners = 8

// SubChainSyncMode controls which sub-chains a node syncs.
type SubChainSyncMode string

const (
	SubChainSyncAll  SubChainSyncMode = "all"  // Sync every registered sub-chain (default)
	SubChainSyncNone SubChainSyncMode = "none" // Register only, don't spawn any sub-chain
	SubChainSyncList SubChainSyncMode = "list" // Sync only the chain IDs in the list
)

// SubChainSyncConfig holds sub-chain sync settings (per-node, not consensus).
type SubChainSyncConfig struct {
	Mode     SubChainSyncMode `conf:"subchain.sync"`      // all, none, or list
	ChainIDs []string         `conf:"subchain.chain_ids"` // Hex chain IDs (when mode=list)
}

// P2PConfig holds peer-to-peer network settings.
type P2PConfig struct {
	Enabled    bool     `conf:"p2p.enabled"`
	ListenAddr string   `conf:"p2p.listen"`
	Port       int      `conf:"p2p.port"`
	Seeds      []string `conf:"p2p.seeds"`
	MaxPeers   int      `conf:"p2p.maxpeers"`
	NoDiscover bool     `conf:"p2p.nodiscover"`
	DHTServer  bool     `conf:"p2p.dhtserver"` // Run DHT in server mode (for seeds/validators)
	ClearBans  bool     // Clear all peer bans on startup (not persisted in config file).
}

// RPCConfig holds RPC server settings.
type RPCConfig struct {
	Enabled     bool     `conf:"rpc.enabled"`
	Addr        string   `conf:"rpc.addr"`
	Port        int      `conf:"rpc.port"`
	AllowedIPs  []string `conf:"rpc.allowed"`
	CORSOrigins []string `conf:"rpc.cors"` // Allowed CORS origins ("*" = all).
}

// WalletConfig holds wallet settings.
type WalletConfig struct {
	Enabled  bool   `conf:"wallet.enabled"`
	FilePath string `conf:"wallet.file"`
}

// MiningConfig holds block production settings.
// Note: Whether to mine is a node choice; HOW to validate is protocol.
type MiningConfig struct {
	Enabled      bool   `conf:"mining.enabled"`
	Coinbase     string `conf:"mining.coinbase"`
	ValidatorKey string `conf:"mining.validatorkey"` // Path to validator private key (PoA)
	Threads      int    `conf:"mining.threads"`      // Mining threads (PoW)
}

// LogConfig holds logging settings.
type LogConfig struct {
	Level string `conf:"log.level"`
	File  string `conf:"log.file"`
	JSON  bool   `conf:"log.json"`
}

// =============================================================================
// Directory helpers
// =============================================================================

// DefaultDataDir returns the platform-specific default data directory.
//
//	Linux:   ~/.klingnet
//	macOS:   ~/Library/Application Support/Klingnet
//	Windows: %APPDATA%\Klingnet
func DefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".klingnet"
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Klingnet")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData != "" {
			return filepath.Join(appData, "Klingnet")
		}
		return filepath.Join(home, "AppData", "Roaming", "Klingnet")
	default:
		return filepath.Join(home, ".klingnet")
	}
}

// ChainDataDir returns the chain-specific data directory.
func (c *Config) ChainDataDir() string {
	return filepath.Join(c.DataDir, string(c.Network))
}

// BlocksDir returns the blocks storage directory.
func (c *Config) BlocksDir() string {
	return filepath.Join(c.ChainDataDir(), "blocks")
}

// UTXODir returns the UTXO database directory.
func (c *Config) UTXODir() string {
	return filepath.Join(c.ChainDataDir(), "utxo")
}

// WalletDir returns the wallet storage directory.
func (c *Config) WalletDir() string {
	return filepath.Join(c.ChainDataDir(), "wallet")
}

// KeystoreDir returns the keystore directory.
func (c *Config) KeystoreDir() string {
	return filepath.Join(c.ChainDataDir(), "keystore")
}

// LogsDir returns the logs directory.
func (c *Config) LogsDir() string {
	return filepath.Join(c.DataDir, "logs")
}

// SubChainsDir returns the sub-chains data directory.
func (c *Config) SubChainsDir() string {
	return filepath.Join(c.ChainDataDir(), "subchains")
}

// ConfigFile returns the config file path.
func (c *Config) ConfigFile() string {
	return filepath.Join(c.DataDir, "klingnet.conf")
}

package config

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// Flags holds parsed command-line flags.
type Flags struct {
	// Commands
	Help    bool
	Version bool

	// Core
	Network string
	DataDir string
	Config  string

	// P2P
	P2P        bool
	P2PPort    int
	Seeds      string
	MaxPeers   int
	NoDiscover bool
	DHTServer  bool

	// RPC
	RPC        bool
	RPCAddr    string
	RPCPort    int
	RPCAllowed string
	RPCCORS    string

	// Wallet
	Wallet     bool
	WalletFile string

	// Mining (operational only)
	Mine         bool
	Coinbase     string
	ValidatorKey string

	// Sub-chain sync
	SyncSubChains string
	MineSubChains string

	// Logging
	LogLevel string
	LogFile  string
	LogJSON  bool

	// Remaining args
	Args []string

	// Explicitly-set bool flags (for true/false overrides).
	SetP2P        bool
	SetRPC        bool
	SetNoDiscover bool
	SetWallet     bool
	SetMine       bool
	SetLogJSON    bool
}

// ParseFlags parses command-line flags.
func ParseFlags() *Flags {
	f := &Flags{}
	fs := flag.NewFlagSet("klingnet", flag.ContinueOnError)

	// Commands
	fs.BoolVar(&f.Help, "help", false, "Show help message")
	fs.BoolVar(&f.Help, "h", false, "Show help message (shorthand)")
	fs.BoolVar(&f.Version, "version", false, "Show version information")
	fs.BoolVar(&f.Version, "v", false, "Show version (shorthand)")

	// Core
	fs.StringVar(&f.Network, "network", "", "Network type (mainnet or testnet)")
	fs.StringVar(&f.Network, "testnet", "", "Use testnet (shorthand for --network=testnet)")
	fs.StringVar(&f.DataDir, "datadir", "", "Data directory path")
	fs.StringVar(&f.Config, "config", "", "Config file path")
	fs.StringVar(&f.Config, "c", "", "Config file path (shorthand)")

	// P2P
	fs.BoolVar(&f.P2P, "p2p", true, "Enable P2P networking")
	fs.IntVar(&f.P2PPort, "p2p-port", 0, "P2P listen port")
	fs.StringVar(&f.Seeds, "seeds", "", "Seed nodes as comma-separated libp2p multiaddrs")
	fs.IntVar(&f.MaxPeers, "maxpeers", 0, "Maximum number of peers")
	fs.BoolVar(&f.NoDiscover, "nodiscover", false, "Disable peer discovery")
	fs.BoolVar(&f.DHTServer, "dht-server", false, "Run DHT in server mode (for seeds/validators)")

	// RPC
	fs.BoolVar(&f.RPC, "rpc", true, "Enable RPC server")
	fs.StringVar(&f.RPCAddr, "rpc-addr", "", "RPC listen address")
	fs.IntVar(&f.RPCPort, "rpc-port", 0, "RPC listen port")
	fs.StringVar(&f.RPCAllowed, "rpc-allowed", "", "Allowed IPs for RPC")
	fs.StringVar(&f.RPCCORS, "rpc-cors", "", "Allowed CORS origins for RPC (comma-separated)")

	// Wallet
	fs.BoolVar(&f.Wallet, "wallet", false, "Enable integrated wallet")
	fs.StringVar(&f.WalletFile, "wallet-file", "", "Wallet file path")

	// Mining (operational - consensus type is in genesis)
	fs.BoolVar(&f.Mine, "mine", false, "Enable block production")
	fs.StringVar(&f.Coinbase, "coinbase", "", "Address to receive block rewards")
	fs.StringVar(&f.ValidatorKey, "validator-key", "", "Path to validator private key (PoA)")

	// Sub-chain sync
	fs.StringVar(&f.SyncSubChains, "sync-subchains", "", "Which sub-chains to sync: all, none (default), or comma-separated chain IDs")
	fs.StringVar(&f.MineSubChains, "mine-subchains", "", "Comma-separated PoW sub-chain IDs to mine (max 8)")

	// Logging
	fs.StringVar(&f.LogLevel, "log-level", "", "Log level (debug, info, warn, error)")
	fs.StringVar(&f.LogFile, "log-file", "", "Log file path")
	fs.BoolVar(&f.LogJSON, "log-json", false, "Output logs as JSON")

	// Custom usage
	fs.Usage = func() {
		printUsage()
	}

	// Parse
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}

	// Handle --testnet shorthand
	if isFlagSet(fs, "testnet") {
		f.Network = "testnet"
	}
	f.SetP2P = isFlagSet(fs, "p2p")
	f.SetRPC = isFlagSet(fs, "rpc")
	f.SetNoDiscover = isFlagSet(fs, "nodiscover")
	f.SetWallet = isFlagSet(fs, "wallet")
	f.SetMine = isFlagSet(fs, "mine")
	f.SetLogJSON = isFlagSet(fs, "log-json")

	f.Args = fs.Args()

	// Detect unparsed flags caused by positional arguments stopping the parser.
	// This catches mistakes like "--wallet validator --mine" where "validator"
	// is not a flag value (--wallet is a bool) and stops all further parsing.
	for _, arg := range f.Args {
		if strings.HasPrefix(arg, "-") {
			fmt.Fprintf(os.Stderr, "Error: flag %q was not parsed (positional argument stopped parsing)\n", arg)
			fmt.Fprintf(os.Stderr, "Hint: --wallet is a boolean flag. Use --wallet (not --wallet <name>)\n")
			os.Exit(1)
		}
	}

	return f
}

// ApplyFlags applies command-line flags to a Config struct.
func ApplyFlags(cfg *Config, f *Flags) {
	// Core
	if f.Network != "" {
		cfg.Network = NetworkType(f.Network)
	}
	if f.DataDir != "" {
		cfg.DataDir = f.DataDir
	}

	// P2P
	if f.SetP2P {
		cfg.P2P.Enabled = f.P2P
	}
	if f.P2PPort != 0 {
		cfg.P2P.Port = f.P2PPort
	}
	if f.Seeds != "" {
		cfg.P2P.Seeds = parseStringList(f.Seeds)
	}
	if f.MaxPeers != 0 {
		cfg.P2P.MaxPeers = f.MaxPeers
	}
	if f.SetNoDiscover {
		cfg.P2P.NoDiscover = f.NoDiscover
	}
	if f.DHTServer {
		cfg.P2P.DHTServer = true
	}

	// RPC
	if f.SetRPC {
		cfg.RPC.Enabled = f.RPC
	}
	if f.RPCAddr != "" {
		cfg.RPC.Addr = f.RPCAddr
	}
	if f.RPCPort != 0 {
		cfg.RPC.Port = f.RPCPort
	}
	if f.RPCAllowed != "" {
		cfg.RPC.AllowedIPs = parseStringList(f.RPCAllowed)
	}
	if f.RPCCORS != "" {
		cfg.RPC.CORSOrigins = parseStringList(f.RPCCORS)
	}

	// Wallet
	if f.SetWallet {
		cfg.Wallet.Enabled = f.Wallet
	}
	if f.WalletFile != "" {
		cfg.Wallet.FilePath = f.WalletFile
	}

	// Mining
	if f.SetMine {
		cfg.Mining.Enabled = f.Mine
	}
	if f.Coinbase != "" {
		cfg.Mining.Coinbase = f.Coinbase
	}
	if f.ValidatorKey != "" {
		cfg.Mining.ValidatorKey = f.ValidatorKey
	}

	// Sub-chain sync
	if f.SyncSubChains != "" {
		switch f.SyncSubChains {
		case "all":
			cfg.SubChainSync.Mode = SubChainSyncAll
			cfg.SubChainSync.ChainIDs = nil
		case "none":
			cfg.SubChainSync.Mode = SubChainSyncNone
			cfg.SubChainSync.ChainIDs = nil
		default:
			cfg.SubChainSync.Mode = SubChainSyncList
			cfg.SubChainSync.ChainIDs = parseStringList(f.SyncSubChains)
		}
	}

	// Sub-chain mining
	if f.MineSubChains != "" {
		cfg.SubChainMineIDs = parseStringList(f.MineSubChains)
	}

	// Logging
	if f.LogLevel != "" {
		cfg.Log.Level = f.LogLevel
	}
	if f.LogFile != "" {
		cfg.Log.File = f.LogFile
	}
	if f.SetLogJSON {
		cfg.Log.JSON = f.LogJSON
	}
}

// isFlagSet checks if a flag was explicitly set.
func isFlagSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func printUsage() {
	usage := `Klingnet Chain - UTXO blockchain with recursive sub-chains

Usage:
  klingnetd [options]
  klingnetd --help

Commands:
  --help, -h      Show this help message
  --version, -v   Show version information

Core Options:
  --network       Network type: mainnet (default) or testnet
  --testnet       Shorthand for --network=testnet
  --datadir       Data directory (default: ~/.klingnet)
  --config, -c    Config file path (default: <datadir>/klingnet.conf)

P2P Options:
  --p2p           Enable P2P networking (default: true)
  --p2p-port      P2P listen port (mainnet: 30303, testnet: 30304)
  --seeds         Seed nodes as comma-separated libp2p multiaddrs
  --maxpeers      Maximum number of peers (default: 50)
  --nodiscover    Disable peer discovery
  --dht-server    Run DHT in server mode (for seed nodes/validators)

RPC Options:
  --rpc           Enable RPC server (default: true)
  --rpc-addr      RPC listen address (default: 127.0.0.1)
  --rpc-port      RPC port (mainnet: 8545, testnet: 8645)
  --rpc-allowed   Allowed IPs for RPC (comma-separated)
  --rpc-cors      Allowed CORS origins for RPC (comma-separated)

Wallet Options:
  --wallet        Enable integrated wallet
  --wallet-file   Wallet file path

Mining Options:
  --mine            Enable block production
  --coinbase        Address to receive block rewards
  --validator-key   Path to validator private key (for PoA chains)

Sub-chain Options:
  --sync-subchains  Which sub-chains to sync: all, none (default), or
                    comma-separated chain ID hex strings
  --mine-subchains  Comma-separated PoW sub-chain IDs to mine (max 8)

Logging Options:
  --log-level     Log level: debug, info, warn, error (default: info)
  --log-file      Log file path (default: stdout)
  --log-json      Output logs as JSON

Examples:
  # Start mainnet node
  klingnetd

  # Start testnet node
  klingnetd --network=testnet

  # Start as a validator (PoA)
  klingnetd --mine --coinbase=<address> --validator-key=~/.klingnet/validator.key

  # Start with custom data directory
  klingnetd --datadir=/path/to/data

Note:
  Protocol rules (consensus type, sub-chain limits, etc.) are hardcoded in
  the genesis configuration and cannot be changed at runtime. Data
  directories are created automatically on first start.
`
	fmt.Print(usage)
}

// Load loads configuration with the following precedence:
// 1. Default values
// 2. Auto-create data dirs + default config (idempotent)
// 3. Config file
// 4. Command-line flags
func Load() (*Config, *Flags, error) {
	flags := ParseFlags()

	// Handle help/version
	if flags.Help {
		printUsage()
		os.Exit(0)
	}
	if flags.Version {
		fmt.Println("klingnetd version 0.1.0")
		os.Exit(0)
	}

	// Determine network first (needed for defaults)
	network := Mainnet
	if flags.Network == "testnet" || strings.ToLower(flags.Network) == "testnet" {
		network = Testnet
	}

	// Start with defaults
	cfg := Default(network)

	// Override datadir if specified
	if flags.DataDir != "" {
		cfg.DataDir = flags.DataDir
	}

	// Auto-create data directories and default config on first start.
	if err := EnsureDataDirs(cfg); err != nil {
		return nil, nil, fmt.Errorf("ensuring data dirs: %w", err)
	}

	// Determine config file path
	configPath := flags.Config
	if configPath == "" {
		configPath = cfg.ConfigFile()
	}

	// Load config file
	fileValues, err := LoadFile(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config file: %w", err)
	}

	// Apply file config
	if err := ApplyFileConfig(cfg, fileValues); err != nil {
		return nil, nil, fmt.Errorf("applying config file: %w", err)
	}

	// Apply flags (highest precedence)
	ApplyFlags(cfg, flags)
	if err := Validate(cfg); err != nil {
		return nil, nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, flags, nil
}

// LoadFromFile loads config from defaults + conf file only (no CLI flags).
// Used by the Qt wallet which has no CLI flags.
func LoadFromFile(dataDir string, network NetworkType) (*Config, error) {
	cfg := Default(network)
	if dataDir != "" {
		cfg.DataDir = dataDir
	}
	if err := EnsureDataDirs(cfg); err != nil {
		return nil, fmt.Errorf("ensuring data dirs: %w", err)
	}
	fileValues, err := LoadFile(cfg.ConfigFile())
	if err != nil {
		return nil, fmt.Errorf("loading config file: %w", err)
	}
	if err := ApplyFileConfig(cfg, fileValues); err != nil {
		return nil, fmt.Errorf("applying config: %w", err)
	}
	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

// EnsureDataDirs creates the data directory structure and a default config
// file if they don't already exist. This is idempotent — safe to call on
// every startup.
func EnsureDataDirs(cfg *Config) error {
	dirs := []string{
		cfg.DataDir,
		cfg.ChainDataDir(),
		cfg.BlocksDir(),
		cfg.UTXODir(),
		cfg.WalletDir(),
		cfg.KeystoreDir(),
		cfg.SubChainsDir(),
		cfg.LogsDir(),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// Create default config if it doesn't exist.
	configPath := cfg.ConfigFile()
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := WriteDefaultConfig(configPath, cfg.Network); err != nil {
			return fmt.Errorf("writing config file: %w", err)
		}
	}

	return nil
}

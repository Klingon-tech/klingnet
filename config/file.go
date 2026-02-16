package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// LoadFile loads node configuration from a .conf file.
// Format: key = value (one per line, # for comments)
func LoadFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse key = value
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("line %d: invalid format (expected key = value)", lineNum)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes if present
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		values[key] = value
	}

	return values, scanner.Err()
}

// ApplyFileConfig applies file configuration to a Config struct.
func ApplyFileConfig(cfg *Config, values map[string]string) error {
	for key, value := range values {
		if err := setConfigValue(cfg, key, value); err != nil {
			return fmt.Errorf("config key %q: %w", key, err)
		}
	}
	return nil
}

// setConfigValue sets a node config value by key.
// Only node-operational settings, NOT protocol rules.
func setConfigValue(cfg *Config, key, value string) error {
	switch key {
	// Core
	case "network":
		cfg.Network = NetworkType(value)
	case "datadir":
		cfg.DataDir = value

	// P2P
	case "p2p.enabled", "p2p":
		cfg.P2P.Enabled = parseBool(value)
	case "p2p.listen":
		cfg.P2P.ListenAddr = value
	case "p2p.port":
		port, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.P2P.Port = port
	case "p2p.seeds":
		cfg.P2P.Seeds = parseStringList(value)
	case "p2p.maxpeers":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.P2P.MaxPeers = n
	case "p2p.nodiscover":
		cfg.P2P.NoDiscover = parseBool(value)
	case "p2p.dhtserver":
		cfg.P2P.DHTServer = parseBool(value)

	// RPC
	case "rpc.enabled", "rpc":
		cfg.RPC.Enabled = parseBool(value)
	case "rpc.addr":
		cfg.RPC.Addr = value
	case "rpc.port":
		port, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.RPC.Port = port
	case "rpc.allowed":
		cfg.RPC.AllowedIPs = parseStringList(value)
	case "rpc.cors":
		cfg.RPC.CORSOrigins = parseStringList(value)

	// Wallet
	case "wallet.enabled", "wallet":
		cfg.Wallet.Enabled = parseBool(value)
	case "wallet.file":
		cfg.Wallet.FilePath = value

	// Mining (operational, not consensus rules)
	case "mining.enabled", "mine":
		cfg.Mining.Enabled = parseBool(value)
	case "mining.coinbase", "coinbase":
		cfg.Mining.Coinbase = value
	case "mining.validatorkey":
		cfg.Mining.ValidatorKey = value
	case "mining.threads":
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.Mining.Threads = n

	// Sub-chains (operational)
	case "subchain.sync":
		switch strings.ToLower(value) {
		case "all":
			cfg.SubChainSync.Mode = SubChainSyncAll
			cfg.SubChainSync.ChainIDs = nil
		case "none", "":
			cfg.SubChainSync.Mode = SubChainSyncNone
			cfg.SubChainSync.ChainIDs = nil
		default:
			cfg.SubChainSync.Mode = SubChainSyncList
			cfg.SubChainSync.ChainIDs = parseStringList(value)
		}
	case "subchain.mine":
		cfg.SubChainMineIDs = parseStringList(value)

	// Logging
	case "log.level":
		cfg.Log.Level = value
	case "log.file":
		cfg.Log.File = value
	case "log.json":
		cfg.Log.JSON = parseBool(value)

	default:
		// Unknown keys are ignored
	}
	return nil
}

// parseBool parses a boolean value.
func parseBool(s string) bool {
	s = strings.ToLower(s)
	return s == "true" || s == "1" || s == "yes" || s == "on"
}

// parseStringList parses a comma-separated list.
func parseStringList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// WriteDefaultConfig writes a default node configuration file.
func WriteDefaultConfig(path string, network NetworkType) error {
	content := `# Klingnet Chain Node Configuration
#
# This file contains NODE settings only.
# Protocol rules (consensus, sub-chain limits) are hardcoded in the
# genesis configuration and cannot be changed without a hard fork.

# Network: mainnet or testnet
network = ` + string(network) + `

# Data directory (default: ~/.klingnet)
# datadir = ~/.klingnet

# ============================================================================
# P2P Network
# ============================================================================

p2p.enabled = true
p2p.listen = 0.0.0.0
p2p.port = ` + defaultPort(network) + `
p2p.maxpeers = 50

# Seed nodes (comma-separated)
# p2p.seeds = node1.example.com:30303,node2.example.com:30303

# Disable peer discovery (for private networks)
# p2p.nodiscover = false

# Run DHT in server mode (for seed nodes/validators)
# p2p.dhtserver = false

# ============================================================================
# RPC Server
# ============================================================================

rpc.enabled = true
rpc.addr = 127.0.0.1
rpc.port = ` + defaultRPCPort(network) + `
rpc.allowed = 127.0.0.1
# CORS allowed origins ("*" for all)
# rpc.cors = http://localhost:3000

# ============================================================================
# Wallet
# ============================================================================

wallet.enabled = false
# wallet.file = wallet.dat

# ============================================================================
# Mining / Block Production
# ============================================================================

# Enable block production (requires validator key for PoA)
mining.enabled = false

# Address to receive block rewards
# mining.coinbase = <your-address>

# Path to validator private key (for PoA)
# mining.validatorkey = ~/.klingnet/validator.key

# Mining threads (for PoW, if enabled on this chain)
# mining.threads = 1

# ============================================================================
# Sub-Chains
# ============================================================================

# Sub-chain sync: all, none, or comma-separated chain IDs
# subchain.sync = none

# PoW sub-chains to mine (comma-separated chain IDs)
# subchain.mine =

# ============================================================================
# Logging
# ============================================================================

log.level = info
# log.file =
log.json = false
`
	return os.WriteFile(path, []byte(content), 0644)
}

func defaultPort(network NetworkType) string {
	if network == Testnet {
		return "30304"
	}
	return "30303"
}

func defaultRPCPort(network NetworkType) string {
	if network == Testnet {
		return "8645"
	}
	return "8545"
}

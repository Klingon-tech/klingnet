package node

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// loadValidatorKey reads a hex-encoded 32-byte private key from a file.
func loadValidatorKey(path string) (*crypto.PrivateKey, error) {
	path = expandHome(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("validator key file not found: %s (use 'klingnet-cli wallet exportKey' to generate one)", path)
		}
		if os.IsPermission(err) {
			return nil, fmt.Errorf("permission denied reading validator key file: %s", path)
		}
		return nil, fmt.Errorf("read validator key file %s: %w", path, err)
	}

	hexStr := strings.TrimSpace(string(data))
	if len(hexStr) == 0 {
		return nil, fmt.Errorf("validator key file %s is empty", path)
	}

	keyBytes, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, fmt.Errorf("validator key file %s contains invalid hex (expected 64-char hex-encoded private key): %w", path, err)
	}

	pk, err := crypto.PrivateKeyFromBytes(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid validator key in %s (expected 32-byte secp256k1 private key): %w", path, err)
	}
	return pk, nil
}

// resolveCoinbase determines the coinbase address from a string or validator key.
func resolveCoinbase(coinbaseStr string, validatorKey *crypto.PrivateKey) (types.Address, error) {
	if coinbaseStr != "" {
		addr, err := types.ParseAddress(coinbaseStr)
		if err != nil {
			return types.Address{}, fmt.Errorf("invalid coinbase address: %w", err)
		}
		return addr, nil
	}

	if validatorKey != nil {
		return crypto.AddressFromPubKey(validatorKey.PublicKey()), nil
	}

	return types.Address{}, fmt.Errorf("--mine requires --coinbase address or --validator-key (to derive coinbase from public key)")
}

// createEngine builds a consensus engine from the genesis configuration.
func createEngine(genesis *config.Genesis) (consensus.Engine, error) {
	switch genesis.Protocol.Consensus.Type {
	case config.ConsensusPoA:
		validators := make([][]byte, len(genesis.Protocol.Consensus.Validators))
		for i, v := range genesis.Protocol.Consensus.Validators {
			b, err := hex.DecodeString(v)
			if err != nil {
				return nil, fmt.Errorf("decode validator %d: %w", i, err)
			}
			validators[i] = b
		}

		poa, err := consensus.NewPoA(validators, genesis.Protocol.Consensus.BlockTime)
		if err != nil {
			return nil, fmt.Errorf("create poa: %w", err)
		}

		return poa, nil

	default:
		return nil, fmt.Errorf("unsupported consensus type: %s", genesis.Protocol.Consensus.Type)
	}
}

// isPoW checks if an engine is PoW.
func isPoW(engine consensus.Engine) bool {
	_, ok := engine.(*consensus.PoW)
	return ok
}

// formatDifficulty returns a human-readable difficulty string (e.g. "1.05M").
func formatDifficulty(d uint64) string {
	switch {
	case d >= 1_000_000_000_000:
		return fmt.Sprintf("%.2fT", float64(d)/1_000_000_000_000)
	case d >= 1_000_000_000:
		return fmt.Sprintf("%.2fG", float64(d)/1_000_000_000)
	case d >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(d)/1_000_000)
	case d >= 1_000:
		return fmt.Sprintf("%.2fK", float64(d)/1_000)
	default:
		return fmt.Sprintf("%d", d)
	}
}

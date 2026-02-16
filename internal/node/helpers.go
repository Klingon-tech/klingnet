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
		return nil, fmt.Errorf("read key file: %w", err)
	}

	hexStr := strings.TrimSpace(string(data))
	keyBytes, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, fmt.Errorf("decode hex: %w", err)
	}

	return crypto.PrivateKeyFromBytes(keyBytes)
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

	return types.Address{}, fmt.Errorf("mining requires coinbase or validator-key")
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

		poa, err := consensus.NewPoA(validators)
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

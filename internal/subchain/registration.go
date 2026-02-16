package subchain

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// RegistrationData is the JSON payload in a ScriptTypeRegister output's Script.Data.
// It defines the configuration for a new sub-chain.
type RegistrationData struct {
	Name              string   `json:"name"`                         // 1-64 chars
	Symbol            string   `json:"symbol"`                       // 2-10 uppercase alphanumeric
	ConsensusType     string   `json:"consensus_type"`               // "poa" or "pow"
	BlockTime         int      `json:"block_time"`                   // Seconds between blocks (>= 1)
	BlockReward       uint64   `json:"block_reward"`                 // Base units per block
	MaxSupply         uint64   `json:"max_supply"`                   // Total coin cap in base units
	MinFeeRate        uint64   `json:"min_fee_rate"`                  // Minimum fee rate (base units per byte)
	Validators        []string `json:"validators,omitempty"`         // PoA: hex-encoded 33-byte compressed pubkeys
	InitialDifficulty uint64   `json:"initial_difficulty,omitempty"` // PoW: starting difficulty
	DifficultyAdjust  int      `json:"difficulty_adjust,omitempty"`  // PoW: blocks between adjustments (0=disabled)
	ValidatorStake    uint64   `json:"validator_stake,omitempty"`    // PoA: min stake for dynamic validators (0=disabled, fixed set)
}

var (
	namePattern   = regexp.MustCompile(`^[a-zA-Z0-9 \-]{1,64}$`)
	symbolPattern = regexp.MustCompile(`^[A-Z0-9]{2,10}$`)
)

// ParseRegistrationData deserializes Script.Data into RegistrationData.
func ParseRegistrationData(scriptData []byte) (*RegistrationData, error) {
	var rd RegistrationData
	if err := json.Unmarshal(scriptData, &rd); err != nil {
		return nil, fmt.Errorf("parse registration data: %w", err)
	}
	return &rd, nil
}

// ValidateRegistrationData checks that a RegistrationData is well-formed.
func ValidateRegistrationData(data *RegistrationData, rules *config.SubChainRules) error {
	if !namePattern.MatchString(data.Name) {
		return fmt.Errorf("name must be 1-64 alphanumeric/space/hyphen characters")
	}
	if !symbolPattern.MatchString(data.Symbol) {
		return fmt.Errorf("symbol must be 2-10 uppercase alphanumeric characters")
	}

	switch data.ConsensusType {
	case config.ConsensusPoA:
		if len(data.Validators) == 0 {
			return fmt.Errorf("PoA requires at least one validator")
		}
		for i, v := range data.Validators {
			b, err := hex.DecodeString(v)
			if err != nil || len(b) != 33 {
				return fmt.Errorf("validator %d: must be 33-byte compressed pubkey hex", i)
			}
		}
	case config.ConsensusPoW:
		if !rules.AllowPoW {
			return fmt.Errorf("PoW sub-chains are not allowed by protocol rules")
		}
		if data.InitialDifficulty == 0 {
			return fmt.Errorf("PoW requires initial_difficulty > 0")
		}
		if data.DifficultyAdjust < 0 {
			return fmt.Errorf("difficulty_adjust must be >= 0")
		}
		if data.DifficultyAdjust > 0 && data.DifficultyAdjust < 10 {
			return fmt.Errorf("difficulty_adjust must be >= 10 (or 0 to disable)")
		}
	default:
		return fmt.Errorf("unknown consensus type: %q (must be %q or %q)",
			data.ConsensusType, config.ConsensusPoA, config.ConsensusPoW)
	}

	if data.BlockTime < 1 {
		return fmt.Errorf("block_time must be >= 1 second")
	}
	if data.BlockReward == 0 {
		return fmt.Errorf("block_reward must be > 0")
	}
	if data.MaxSupply == 0 {
		return fmt.Errorf("max_supply must be > 0")
	}
	if data.MaxSupply < data.BlockReward {
		return fmt.Errorf("max_supply must be >= block_reward")
	}
	if data.MinFeeRate == 0 {
		return fmt.Errorf("min_fee_rate must be > 0")
	}

	return nil
}

// DeriveChainID computes ChainID = BLAKE3(txHash || outputIndex).
func DeriveChainID(txHash types.Hash, outputIndex uint32) types.ChainID {
	buf := make([]byte, types.HashSize+4)
	copy(buf, txHash[:])
	binary.BigEndian.PutUint32(buf[types.HashSize:], outputIndex)
	hash := crypto.Hash(buf)
	return types.ChainID(hash)
}

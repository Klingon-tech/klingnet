package config

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// Validate checks runtime node config for obvious operator mistakes.
func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.Network != Mainnet && cfg.Network != Testnet {
		return fmt.Errorf("network must be %q or %q", Mainnet, Testnet)
	}
	if cfg.P2P.Port < 0 || cfg.P2P.Port > 65535 {
		return fmt.Errorf("p2p.port must be in range [0, 65535]")
	}
	if cfg.RPC.Port < 0 || cfg.RPC.Port > 65535 {
		return fmt.Errorf("rpc.port must be in range [0, 65535]")
	}

	if cfg.SubChainSync.Mode == "" {
		cfg.SubChainSync.Mode = SubChainSyncNone
	}
	switch cfg.SubChainSync.Mode {
	case SubChainSyncAll, SubChainSyncNone:
		cfg.SubChainSync.ChainIDs = nil
	case SubChainSyncList:
		if len(cfg.SubChainSync.ChainIDs) == 0 {
			return fmt.Errorf("subchain.sync=list requires at least one chain ID")
		}
	default:
		return fmt.Errorf("subchain.sync must be all, none, or list")
	}

	if err := validateChainIDs(cfg.SubChainSync.ChainIDs, "subchain.sync"); err != nil {
		return err
	}
	if len(cfg.SubChainMineIDs) > MaxSubChainMiners {
		return fmt.Errorf("subchain.mine has %d IDs, max is %d", len(cfg.SubChainMineIDs), MaxSubChainMiners)
	}
	if err := validateChainIDs(cfg.SubChainMineIDs, "subchain.mine"); err != nil {
		return err
	}

	return nil
}

func validateChainIDs(ids []string, field string) error {
	seen := make(map[string]struct{}, len(ids))
	for i, id := range ids {
		s := strings.ToLower(strings.TrimSpace(id))
		if s == "" {
			return fmt.Errorf("%s[%d] is empty", field, i)
		}
		b, err := hex.DecodeString(s)
		if err != nil || len(b) != types.HashSize {
			return fmt.Errorf("%s[%d] must be 32-byte hex chain ID", field, i)
		}
		if _, ok := seen[s]; ok {
			return fmt.Errorf("%s has duplicate chain ID %q", field, s)
		}
		seen[s] = struct{}{}
		ids[i] = s
	}
	return nil
}

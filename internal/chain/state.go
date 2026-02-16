package chain

import "github.com/Klingon-tech/klingnet-chain/pkg/types"

// State holds the current chain tip state.
type State struct {
	Height               uint64
	TipHash              types.Hash
	Supply               uint64 // Total coins in circulation (genesis alloc + cumulative rewards).
	CumulativeDifficulty uint64 // Sum of all block difficulties (for PoW fork choice).
	TipTimestamp         uint64 // Timestamp of the current tip block.
}

// IsGenesis returns true if no blocks have been processed yet.
func (s *State) IsGenesis() bool {
	return s.Height == 0 && s.TipHash.IsZero()
}

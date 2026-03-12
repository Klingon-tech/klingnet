package chain

import "github.com/Klingon-tech/klingnet-chain/config"

// blockSubsidy returns the configured block subsidy at the given height,
// applying halvings when enabled. Height 0 is genesis and mints no subsidy.
func (c *Chain) blockSubsidy(height uint64) uint64 {
	return config.ConsensusRules{
		BlockReward:     c.blockReward,
		HalvingInterval: c.halvingInterval,
	}.BlockSubsidy(height)
}

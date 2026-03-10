package chain

// blockSubsidy returns the configured block subsidy at the given height,
// applying halvings when enabled. Height 0 is genesis and mints no subsidy.
func (c *Chain) blockSubsidy(height uint64) uint64 {
	if height == 0 || c.blockReward == 0 {
		return 0
	}
	if c.halvingInterval == 0 {
		return c.blockReward
	}
	halvings := (height - 1) / c.halvingInterval
	if halvings >= 64 {
		return 0
	}
	return c.blockReward >> halvings
}

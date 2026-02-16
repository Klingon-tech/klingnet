package config

import "testing"

func TestForkSchedule_IsActive_ZeroNotScheduled(t *testing.T) {
	fs := ForkSchedule{}
	if fs.IsActive(0, 100) {
		t.Error("fork at height 0 (not scheduled) should not be active")
	}
}

func TestForkSchedule_IsActive_HeightReached(t *testing.T) {
	fs := ForkSchedule{}
	if !fs.IsActive(50, 50) {
		t.Error("fork at height 50 should be active at height 50")
	}
	if !fs.IsActive(50, 100) {
		t.Error("fork at height 50 should be active at height 100")
	}
}

func TestForkSchedule_IsActive_HeightNotReached(t *testing.T) {
	fs := ForkSchedule{}
	if fs.IsActive(50, 49) {
		t.Error("fork at height 50 should not be active at height 49")
	}
}

func TestMainnetGenesis_HasForks(t *testing.T) {
	g := MainnetGenesis()
	// Forks field should exist (zero-value ForkSchedule).
	_ = g.Protocol.Forks
}

func TestTestnetGenesis_HasForks(t *testing.T) {
	g := TestnetGenesis()
	_ = g.Protocol.Forks
}

func TestGenesis_Validate_MainnetValid(t *testing.T) {
	g := MainnetGenesis()
	if err := g.Validate(); err != nil {
		t.Errorf("mainnet genesis should be valid: %v", err)
	}
}

func TestGenesis_Validate_TestnetValid(t *testing.T) {
	g := TestnetGenesis()
	if err := g.Validate(); err != nil {
		t.Errorf("testnet genesis should be valid: %v", err)
	}
}

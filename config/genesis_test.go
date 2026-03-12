package config

import (
	"math"
	"strings"
	"testing"
)

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

func TestConsensusRules_BlockSubsidy(t *testing.T) {
	tests := []struct {
		name     string
		reward   uint64
		halving  uint64
		height   uint64
		want     uint64
	}{
		{"genesis returns 0", 1000, 0, 0, 0},
		{"no halving", 1000, 0, 100, 1000},
		{"zero reward", 0, 10, 5, 0},
		{"before first halving", 1000, 10, 1, 1000},
		{"at first halving", 1000, 10, 11, 500},
		{"at second halving", 1000, 10, 21, 250},
		{"64+ halvings returns 0", 1000, 1, 65, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := ConsensusRules{BlockReward: tt.reward, HalvingInterval: tt.halving}
			got := r.BlockSubsidy(tt.height)
			if got != tt.want {
				t.Errorf("BlockSubsidy(%d) = %d, want %d", tt.height, got, tt.want)
			}
		})
	}
}

func TestGenesis_Validate_AllocOverflow(t *testing.T) {
	g := MainnetGenesis()
	g.Alloc = map[string]uint64{
		"0000000000000000000000000000000000000001": math.MaxUint64,
		"0000000000000000000000000000000000000002": 1,
	}

	err := g.Validate()
	if err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("expected overflow validation error, got: %v", err)
	}
}

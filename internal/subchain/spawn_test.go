package subchain

import (
	"testing"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func TestSpawn_PoA(t *testing.T) {
	db := storage.NewMemory()
	rd := validPoARegistration()
	chainID := DeriveChainID(types.Hash{1, 2, 3}, 0)

	result, err := Spawn(SpawnConfig{
		ChainID:         chainID,
		Registration:    rd,
		ParentDB:        db,
		CreatedAtHeight: 10,
	})
	if err != nil {
		t.Fatalf("Spawn PoA: %v", err)
	}

	// Chain should be initialized at height 0.
	if result.Chain.Height() != 0 {
		t.Fatalf("Height = %d, want 0", result.Chain.Height())
	}

	// Genesis should have sub-chains disabled (flat model).
	if result.Genesis.Protocol.SubChain.Enabled {
		t.Fatal("sub-chain genesis should have SubChain.Enabled = false")
	}

	// Symbol should match registration.
	if result.Genesis.Symbol != "TST" {
		t.Fatalf("Symbol = %q, want %q", result.Genesis.Symbol, "TST")
	}

	// Engine should be PoA.
	if _, ok := result.Engine.(*consensus.PoA); !ok {
		t.Fatal("expected PoA engine")
	}

	// Pool should exist.
	if result.Pool == nil {
		t.Fatal("Pool is nil")
	}
}

func TestSpawn_PoW(t *testing.T) {
	db := storage.NewMemory()
	rd := validPoWRegistration()
	chainID := DeriveChainID(types.Hash{4, 5, 6}, 0)

	result, err := Spawn(SpawnConfig{
		ChainID:      chainID,
		Registration: rd,
		ParentDB:     db,
	})
	if err != nil {
		t.Fatalf("Spawn PoW: %v", err)
	}

	if result.Chain.Height() != 0 {
		t.Fatalf("Height = %d, want 0", result.Chain.Height())
	}

	// Engine should be PoW.
	if _, ok := result.Engine.(*consensus.PoW); !ok {
		t.Fatal("expected PoW engine")
	}
}

func TestSpawn_Isolation(t *testing.T) {
	db := storage.NewMemory()
	rd := validPoARegistration()

	id1 := DeriveChainID(types.Hash{1}, 0)
	id2 := DeriveChainID(types.Hash{2}, 0)

	r1, err := Spawn(SpawnConfig{ChainID: id1, Registration: rd, ParentDB: db})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Spawn(SpawnConfig{ChainID: id2, Registration: rd, ParentDB: db})
	if err != nil {
		t.Fatal(err)
	}

	// Both chains should be at height 0 independently.
	if r1.Chain.Height() != 0 || r2.Chain.Height() != 0 {
		t.Fatal("both chains should be at height 0")
	}

	// Chain IDs should be different.
	if id1 == id2 {
		t.Fatal("chain IDs should differ")
	}

	// Each chain's genesis config should reference its own chain ID.
	if r1.Genesis.ChainID == r2.Genesis.ChainID {
		t.Fatal("genesis chain IDs should differ")
	}
}

func TestSpawn_NilRegistration(t *testing.T) {
	_, err := Spawn(SpawnConfig{
		ChainID:  types.ChainID{1},
		ParentDB: storage.NewMemory(),
	})
	if err == nil {
		t.Fatal("expected error for nil registration")
	}
}

func TestSpawn_RestoreExisting(t *testing.T) {
	db := storage.NewMemory()
	rd := validPoARegistration()
	chainID := DeriveChainID(types.Hash{9}, 0)

	// First spawn — initializes from genesis.
	r1, err := Spawn(SpawnConfig{ChainID: chainID, Registration: rd, ParentDB: db})
	if err != nil {
		t.Fatal(err)
	}
	tip1 := r1.Chain.TipHash()

	// Second spawn with same DB and chainID — should recover from existing data.
	r2, err := Spawn(SpawnConfig{ChainID: chainID, Registration: rd, ParentDB: db})
	if err != nil {
		t.Fatal(err)
	}
	tip2 := r2.Chain.TipHash()

	if tip1 != tip2 {
		t.Fatalf("restored tip %s != original tip %s", tip2, tip1)
	}
}

func TestSpawn_BadConsensus(t *testing.T) {
	db := storage.NewMemory()
	rd := &RegistrationData{
		Name:          "Bad",
		Symbol:        "BAD",
		ConsensusType: "pos", // unsupported
		BlockTime:     3,
		BlockReward:   config.Coin,
		MaxSupply:     1000 * config.Coin,
		MinFeeRate:    10,
	}

	_, err := Spawn(SpawnConfig{
		ChainID:      types.ChainID{1},
		Registration: rd,
		ParentDB:     db,
	})
	if err == nil {
		t.Fatal("expected error for unsupported consensus type")
	}
}

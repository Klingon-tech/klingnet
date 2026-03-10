package chain

import (
	"errors"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// testMultiValidatorChain creates a chain with N validators and returns the
// chain, all validator keys, and the PoA engine.
func testMultiValidatorChain(t *testing.T, n int) (*Chain, []*crypto.PrivateKey, *consensus.PoA) {
	t.Helper()

	keys := make([]*crypto.PrivateKey, n)
	pubs := make([][]byte, n)
	for i := range keys {
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey %d: %v", i, err)
		}
		keys[i] = key
		pubs[i] = key.PublicKey()
	}

	poa, err := consensus.NewPoA(pubs, 3)
	if err != nil {
		t.Fatalf("NewPoA: %v", err)
	}

	db := storage.NewMemory()
	utxoStore := utxo.NewStore(db)
	ch, err := New(types.ChainID{}, db, utxoStore, poa)
	if err != nil {
		t.Fatalf("New chain: %v", err)
	}

	addr := crypto.AddressFromPubKey(keys[0].PublicKey())
	gen := &config.Genesis{
		ChainID:   "test-chain-1",
		ChainName: "Test Chain",
		Timestamp: 1700000000,
		Alloc: map[string]uint64{
			addr.String(): 100_000,
		},
		Protocol: config.ProtocolConfig{
			Consensus: config.ConsensusRules{
				Type:        config.ConsensusPoA,
				BlockTime:   3,
				BlockReward: 1000,
			},
		},
	}
	if err := ch.InitFromGenesis(gen); err != nil {
		t.Fatalf("InitFromGenesis: %v", err)
	}

	return ch, keys, poa
}

// buildCoinbaseOnlyBlock creates a minimal block with only a coinbase tx,
// signed by the given validator key.
func buildCoinbaseOnlyBlock(t *testing.T, ch *Chain, poa *consensus.PoA, signerKey *crypto.PrivateKey, timestamp uint64) *block.Block {
	t.Helper()

	state := ch.State()
	return buildCoinbaseOnlyBlockAt(t, poa, signerKey, state.TipHash, state.Height+1, timestamp)
}

func buildCoinbaseOnlyBlockAt(t *testing.T, poa *consensus.PoA, signerKey *crypto.PrivateKey, prevHash types.Hash, height, timestamp uint64) *block.Block {
	t.Helper()

	coinbase := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
		}},
	}
	txs := []*tx.Transaction{coinbase}
	merkle := block.ComputeMerkleRoot([]types.Hash{coinbase.Hash()})
	header := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   prevHash,
		MerkleRoot: merkle,
		Timestamp:  timestamp,
		Height:     height,
	}
	blk := block.NewBlock(header, txs)

	// Set signer temporarily for Prepare + Seal.
	origSigner := poa.GetSigner()
	poa.SetSigner(signerKey)
	defer func() {
		if origSigner != nil {
			poa.SetSigner(origSigner)
		}
	}()

	if err := poa.Prepare(blk.Header); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err := poa.Seal(blk); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return blk
}

func TestPoA_SigningLimit_Formula(t *testing.T) {
	tests := []struct {
		n    int
		want int
	}{
		{1, 0},  // No limit for single validator.
		{2, 2},  // N/2+1 = 1+1 = 2
		{3, 2},  // N/2+1 = 1+1 = 2
		{4, 3},  // N/2+1 = 2+1 = 3
		{5, 3},  // N/2+1 = 2+1 = 3
		{6, 4},  // N/2+1 = 3+1 = 4
		{7, 4},  // N/2+1 = 3+1 = 4
		{10, 6}, // N/2+1 = 5+1 = 6
	}

	for _, tt := range tests {
		pubs := make([][]byte, tt.n)
		for i := range pubs {
			key, _ := crypto.GenerateKey()
			pubs[i] = key.PublicKey()
		}
		poa, _ := consensus.NewPoA(pubs, 3)
		got := poa.SigningLimit()
		if got != tt.want {
			t.Errorf("SigningLimit(N=%d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}

func TestSigningLimit_SingleValidator_NoLimit(t *testing.T) {
	ch, keys, poa := testMultiValidatorChain(t, 1)

	// Single validator should be able to sign consecutive blocks.
	ts := uint64(1700000003)
	for i := 0; i < 5; i++ {
		blk := buildCoinbaseOnlyBlock(t, ch, poa, keys[0], ts)
		if err := ch.ProcessBlock(blk); err != nil {
			t.Fatalf("block %d: unexpected error: %v", i+1, err)
		}
		ts += 3
	}
}

func TestSigningLimit_TwoValidators_Alternating(t *testing.T) {
	ch, keys, poa := testMultiValidatorChain(t, 2)
	// N=2, limit=2: can't sign 2 in a row.

	ts := uint64(1700000003)

	// Validator 0 signs block 1.
	blk1 := buildCoinbaseOnlyBlock(t, ch, poa, keys[0], ts)
	if err := ch.ProcessBlock(blk1); err != nil {
		t.Fatalf("block 1: %v", err)
	}
	ts += 3

	// Validator 1 signs block 2.
	blk2 := buildCoinbaseOnlyBlock(t, ch, poa, keys[1], ts)
	if err := ch.ProcessBlock(blk2); err != nil {
		t.Fatalf("block 2: %v", err)
	}
	ts += 3

	// Validator 0 signs block 3 — OK, gap of 1.
	blk3 := buildCoinbaseOnlyBlock(t, ch, poa, keys[0], ts)
	if err := ch.ProcessBlock(blk3); err != nil {
		t.Fatalf("block 3: %v", err)
	}
}

func TestSigningLimit_TwoValidators_ConsecutiveDetectedByMinerPrecheck(t *testing.T) {
	ch, keys, poa := testMultiValidatorChain(t, 2)
	// N=2, limit=2: can't sign 2 in a row.

	ts := uint64(1700000003)

	// Validator 0 signs block 1.
	blk1 := buildCoinbaseOnlyBlock(t, ch, poa, keys[0], ts)
	if err := ch.ProcessBlock(blk1); err != nil {
		t.Fatalf("block 1: %v", err)
	}

	// Miner pre-check should detect the limit.
	if !ch.IsSigningLimitReached(keys[0].PublicKey()) {
		t.Fatal("expected IsSigningLimitReached=true for V0 after signing block 1")
	}
	// V1 should be fine.
	if ch.IsSigningLimitReached(keys[1].PublicKey()) {
		t.Fatal("V1 should not be limited")
	}
}

func TestSigningLimit_TwoValidators_ConsecutiveRejectedByConsensus(t *testing.T) {
	ch, keys, poa := testMultiValidatorChain(t, 2)

	ts := uint64(1700000003)

	blk1 := buildCoinbaseOnlyBlock(t, ch, poa, keys[0], ts)
	if err := ch.ProcessBlock(blk1); err != nil {
		t.Fatalf("block 1: %v", err)
	}

	blk2 := buildCoinbaseOnlyBlock(t, ch, poa, keys[0], ts+3)
	err := ch.ProcessBlock(blk2)
	if err == nil {
		t.Fatal("expected signing limit violation")
	}
	if !errors.Is(err, ErrSigningLimitExceeded) {
		t.Fatalf("expected ErrSigningLimitExceeded, got: %v", err)
	}
}

func TestSigningLimit_FiveValidators_Window3(t *testing.T) {
	ch, keys, poa := testMultiValidatorChain(t, 5)
	// N=5, limit=3: must wait 2 blocks between signings.

	ts := uint64(1700000003)

	// V0 signs block 1.
	blk := buildCoinbaseOnlyBlock(t, ch, poa, keys[0], ts)
	if err := ch.ProcessBlock(blk); err != nil {
		t.Fatalf("block 1: %v", err)
	}
	ts += 3

	// V1 signs block 2.
	blk = buildCoinbaseOnlyBlock(t, ch, poa, keys[1], ts)
	if err := ch.ProcessBlock(blk); err != nil {
		t.Fatalf("block 2: %v", err)
	}
	ts += 3

	// V0 tries block 3 — miner pre-check detects the limit.
	if !ch.IsSigningLimitReached(keys[0].PublicKey()) {
		t.Fatal("V0 should be limited within window of 3")
	}

	// V2 signs block 3 instead.
	blk = buildCoinbaseOnlyBlock(t, ch, poa, keys[2], ts)
	if err := ch.ProcessBlock(blk); err != nil {
		t.Fatalf("block 3 by V2: %v", err)
	}
	ts += 3

	// V0 signs block 4 — OK, block 1 is now outside the window.
	if ch.IsSigningLimitReached(keys[0].PublicKey()) {
		t.Fatal("V0 should no longer be limited after block 1 exits the window")
	}
	blk = buildCoinbaseOnlyBlock(t, ch, poa, keys[0], ts)
	if err := ch.ProcessBlock(blk); err != nil {
		t.Fatalf("block 4 by V0: %v", err)
	}
}

func TestSigningLimit_IsSigningLimitReached(t *testing.T) {
	ch, keys, poa := testMultiValidatorChain(t, 2)
	// N=2, limit=2.

	ts := uint64(1700000003)

	// Before any blocks, limit not reached.
	if ch.IsSigningLimitReached(keys[0].PublicKey()) {
		t.Error("limit should not be reached before any blocks")
	}

	// V0 signs block 1.
	blk := buildCoinbaseOnlyBlock(t, ch, poa, keys[0], ts)
	if err := ch.ProcessBlock(blk); err != nil {
		t.Fatalf("block 1: %v", err)
	}

	// V0's limit is now reached.
	if !ch.IsSigningLimitReached(keys[0].PublicKey()) {
		t.Error("limit should be reached for V0 after signing block 1")
	}

	// V1's limit is NOT reached.
	if ch.IsSigningLimitReached(keys[1].PublicKey()) {
		t.Error("limit should not be reached for V1")
	}

	ts += 3

	// V1 signs block 2.
	blk = buildCoinbaseOnlyBlock(t, ch, poa, keys[1], ts)
	if err := ch.ProcessBlock(blk); err != nil {
		t.Fatalf("block 2: %v", err)
	}

	// V0's limit is no longer reached (block 1 is now outside window).
	if ch.IsSigningLimitReached(keys[0].PublicKey()) {
		t.Error("limit should not be reached for V0 after V1 signed block 2")
	}
}

func TestSigningLimit_ThreeValidators_Rotation(t *testing.T) {
	ch, keys, poa := testMultiValidatorChain(t, 3)
	// N=3, limit=2: can't sign 2 in a row, but can sign every other.

	ts := uint64(1700000003)

	// V0, V1, V0, V2, V0 — all should succeed (alternating).
	signers := []int{0, 1, 0, 2, 0}
	for i, s := range signers {
		blk := buildCoinbaseOnlyBlock(t, ch, poa, keys[s], ts)
		if err := ch.ProcessBlock(blk); err != nil {
			t.Fatalf("block %d by V%d: %v", i+1, s, err)
		}
		ts += 3
	}
}

// Signing-limit consensus enforcement is covered on the fast path here.
// Reorg replay uses the same checker before replaying a candidate branch.

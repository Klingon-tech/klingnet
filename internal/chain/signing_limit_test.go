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

	coinbase := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
		}},
	}
	txs := []*tx.Transaction{coinbase}

	state := ch.State()
	merkle := block.ComputeMerkleRoot([]types.Hash{coinbase.Hash()})
	header := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   state.TipHash,
		MerkleRoot: merkle,
		Timestamp:  timestamp,
		Height:     state.Height + 1,
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

func TestSigningLimit_ReorgRejectsViolation(t *testing.T) {
	// Create a chain with 2 validators (limit=2).
	keys := make([]*crypto.PrivateKey, 2)
	pubs := make([][]byte, 2)
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

	ts := uint64(1700000003)

	// Build the main chain: V0 block 1, V1 block 2.
	blk1 := buildCoinbaseOnlyBlock(t, ch, poa, keys[0], ts)
	if err := ch.ProcessBlock(blk1); err != nil {
		t.Fatalf("main block 1: %v", err)
	}
	ts += 3
	blk2 := buildCoinbaseOnlyBlock(t, ch, poa, keys[1], ts)
	if err := ch.ProcessBlock(blk2); err != nil {
		t.Fatalf("main block 2: %v", err)
	}

	// Now build a fork branch from genesis where V0 signs blocks 1, 2, and 3 (all V0).
	// The fork branch must be longer to trigger reorg, so it needs 3 blocks.
	// But with signing limit, V0 can't sign 2 in a row.
	// Build the fork blocks manually, bypassing ProcessBlock's fast-path check.
	genesisState := types.Hash{}
	genBlk, _ := ch.blocks.GetBlockByHeight(0)
	genesisState = genBlk.Hash()

	// Fork block 1 (height 1) by V0 — from genesis.
	forkCoinbase1 := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
		}},
	}
	forkHeader1 := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   genesisState,
		MerkleRoot: block.ComputeMerkleRoot([]types.Hash{forkCoinbase1.Hash()}),
		Timestamp:  1700000010, // Different timestamp for different hash.
		Height:     1,
	}
	fork1 := block.NewBlock(forkHeader1, []*tx.Transaction{forkCoinbase1})
	poa.SetSigner(keys[0])
	poa.Prepare(fork1.Header)
	poa.Seal(fork1)

	// Fork block 2 (height 2) by V0 — consecutive V0 (violates limit).
	forkCoinbase2 := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
		}},
	}
	forkHeader2 := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   fork1.Hash(),
		MerkleRoot: block.ComputeMerkleRoot([]types.Hash{forkCoinbase2.Hash()}),
		Timestamp:  1700000013,
		Height:     2,
	}
	fork2 := block.NewBlock(forkHeader2, []*tx.Transaction{forkCoinbase2})
	poa.Prepare(fork2.Header)
	poa.Seal(fork2)

	// Fork block 3 (height 3) by V0 — consecutive V0 again.
	forkCoinbase3 := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
		}},
	}
	forkHeader3 := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   fork2.Hash(),
		MerkleRoot: block.ComputeMerkleRoot([]types.Hash{forkCoinbase3.Hash()}),
		Timestamp:  1700000016,
		Height:     3,
		Difficulty: consensus.DiffInTurn, // Higher diff to trigger reorg.
	}
	fork3 := block.NewBlock(forkHeader3, []*tx.Transaction{forkCoinbase3})
	poa.Prepare(fork3.Header)
	poa.Seal(fork3)

	// Submit fork blocks — they should be stored as fork blocks.
	// Fork block 1 triggers fork detection (different hash at height 1).
	if err := ch.ProcessBlock(fork1); err != nil {
		// May trigger reorg attempt which checks signing limit.
		// Block 1 alone shouldn't fail — it's fine on its own.
		if !errors.Is(err, ErrBlockKnown) {
			t.Logf("fork1 result: %v (expected)", err)
		}
	}

	// Fork block 2 triggers reorg attempt with violation.
	if err := ch.ProcessBlock(fork2); err != nil {
		t.Logf("fork2 result: %v (may be expected)", err)
	}

	// Fork block 3 — longer fork, should trigger reorg. But reorg replay
	// should fail because V0 signed blocks 1 and 2 consecutively.
	err = ch.ProcessBlock(fork3)
	// The reorg should fail due to signing limit violation in the replay.
	// The main chain should remain at height 2.
	if ch.Height() == 3 {
		t.Errorf("chain should NOT have reorged to fork with signing limit violation, height=%d", ch.Height())
	}
	if ch.Height() != 2 {
		t.Errorf("chain should remain at height 2, got %d", ch.Height())
	}
}

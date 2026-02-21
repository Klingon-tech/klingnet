package chain

import (
	"encoding/json"
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

// testChainWithKey creates a single-validator PoA chain + key for testing.
func testChainWithKey(t *testing.T) (*Chain, *crypto.PrivateKey, *consensus.PoA) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pubs := [][]byte{key.PublicKey()}
	poa, err := consensus.NewPoA(pubs, 3)
	if err != nil {
		t.Fatalf("NewPoA: %v", err)
	}
	poa.SetSigner(key)

	db := storage.NewMemory()
	utxoStore := utxo.NewStore(db)
	ch, err := New(types.ChainID{}, db, utxoStore, poa)
	if err != nil {
		t.Fatalf("New chain: %v", err)
	}

	addr := crypto.AddressFromPubKey(key.PublicKey())
	gen := &config.Genesis{
		ChainID:   "rebuild-test",
		ChainName: "Rebuild Test",
		Timestamp: 1700000000,
		Alloc: map[string]uint64{
			addr.String(): 100_000_000,
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
	return ch, key, poa
}

// mineBlock mines a single block on the chain at the given timestamp.
func mineBlock(t *testing.T, ch *Chain, poa *consensus.PoA, key *crypto.PrivateKey, ts uint64) *block.Block {
	t.Helper()
	blk := buildCoinbaseOnlyBlock(t, ch, poa, key, ts)
	if err := ch.ProcessBlock(blk); err != nil {
		t.Fatalf("ProcessBlock height %d: %v", blk.Header.Height, err)
	}
	return blk
}

// TestRebuildReorg_MissingUndo verifies that a reorg succeeds via UTXO rebuild
// when old-branch blocks are missing undo data.
func TestRebuildReorg_MissingUndo(t *testing.T) {
	ch, key, poa := testChainWithKey(t)

	// Mine 3 blocks on the main chain.
	ts := uint64(1700000003)
	for i := 0; i < 3; i++ {
		mineBlock(t, ch, poa, key, ts)
		ts += 3
	}
	if ch.Height() != 3 {
		t.Fatalf("expected height 3, got %d", ch.Height())
	}

	// Delete undo data for all 3 blocks to simulate the "missing undo" scenario.
	for h := uint64(1); h <= 3; h++ {
		blk, err := ch.blocks.GetBlockByHeight(h)
		if err != nil {
			t.Fatalf("GetBlockByHeight(%d): %v", h, err)
		}
		if err := ch.blocks.DeleteUndo(blk.Hash()); err != nil {
			t.Fatalf("DeleteUndo(height %d): %v", h, err)
		}
	}

	// Build a longer fork from genesis (4 blocks) to trigger a reorg.
	genBlk, err := ch.blocks.GetBlockByHeight(0)
	if err != nil {
		t.Fatalf("get genesis: %v", err)
	}
	var forkBlocks []*block.Block
	prevHash := genBlk.Hash()
	forkTS := uint64(1700000004) // Slightly different timestamp for different hashes.
	for i := 0; i < 4; i++ {
		height := uint64(i + 1)
		coinbase := &tx.Transaction{
			Version: 1,
			Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
			Outputs: []tx.Output{{
				Value:  1000,
				Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
			}},
		}
		merkle := block.ComputeMerkleRoot([]types.Hash{coinbase.Hash()})
		header := &block.Header{
			Version:    block.CurrentVersion,
			PrevHash:   prevHash,
			MerkleRoot: merkle,
			Timestamp:  forkTS,
			Height:     height,
		}
		blk := block.NewBlock(header, []*tx.Transaction{coinbase})
		if err := poa.Prepare(blk.Header); err != nil {
			t.Fatalf("Prepare fork block %d: %v", height, err)
		}
		if err := poa.Seal(blk); err != nil {
			t.Fatalf("Seal fork block %d: %v", height, err)
		}
		forkBlocks = append(forkBlocks, blk)
		prevHash = blk.Hash()
		forkTS += 3
	}

	// Process fork blocks — the last one should trigger the reorg.
	for _, blk := range forkBlocks {
		err := ch.ProcessBlock(blk)
		if err != nil {
			t.Fatalf("ProcessBlock fork block height %d: %v", blk.Header.Height, err)
		}
	}

	// Verify the chain switched to the fork.
	if ch.Height() != 4 {
		t.Fatalf("expected height 4 after reorg, got %d", ch.Height())
	}
	lastFork := forkBlocks[len(forkBlocks)-1]
	if ch.TipHash() != lastFork.Hash() {
		t.Fatalf("tip hash mismatch: got %s, want %s", ch.TipHash(), lastFork.Hash())
	}

	// Verify undo data now exists for the new branch blocks.
	for _, blk := range forkBlocks {
		undoBytes, err := ch.blocks.GetUndo(blk.Hash())
		if err != nil {
			t.Fatalf("GetUndo for new block at height %d: %v", blk.Header.Height, err)
		}
		var undo UndoData
		if err := json.Unmarshal(undoBytes, &undo); err != nil {
			t.Fatalf("unmarshal undo at height %d: %v", blk.Header.Height, err)
		}
	}
}

// TestRebuildReorg_SupplyCorrect verifies that supply is correctly computed
// after a rebuild reorg.
func TestRebuildReorg_SupplyCorrect(t *testing.T) {
	ch, key, poa := testChainWithKey(t)

	// Mine 2 blocks.
	ts := uint64(1700000003)
	mineBlock(t, ch, poa, key, ts)
	mineBlock(t, ch, poa, key, ts+3)

	supplyBefore := ch.Supply()

	// Delete undo data.
	for h := uint64(1); h <= 2; h++ {
		blk, _ := ch.blocks.GetBlockByHeight(h)
		ch.blocks.DeleteUndo(blk.Hash())
	}

	// Build a 3-block fork from genesis.
	genBlk, _ := ch.blocks.GetBlockByHeight(0)
	prevHash := genBlk.Hash()
	forkTS := uint64(1700000004)
	for i := 0; i < 3; i++ {
		height := uint64(i + 1)
		coinbase := &tx.Transaction{
			Version: 1,
			Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
			Outputs: []tx.Output{{
				Value:  1000,
				Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
			}},
		}
		merkle := block.ComputeMerkleRoot([]types.Hash{coinbase.Hash()})
		header := &block.Header{
			Version:    block.CurrentVersion,
			PrevHash:   prevHash,
			MerkleRoot: merkle,
			Timestamp:  forkTS,
			Height:     height,
		}
		blk := block.NewBlock(header, []*tx.Transaction{coinbase})
		poa.Prepare(blk.Header)
		poa.Seal(blk)
		ch.ProcessBlock(blk)
		prevHash = blk.Hash()
		forkTS += 3
	}

	// Supply should reflect genesis alloc + 3 block rewards.
	// Genesis supply is 100_000_000, each block reward is 1000.
	expectedSupply := uint64(100_000_000 + 3*1000)
	if ch.Supply() != expectedSupply {
		t.Errorf("supply after rebuild reorg = %d, want %d (was %d before)", ch.Supply(), expectedSupply, supplyBefore)
	}
}

// TestRebuildUTXOs_StoresUndoData verifies that RebuildUTXOs now stores undo
// data so subsequent reorgs don't fail.
func TestRebuildUTXOs_StoresUndoData(t *testing.T) {
	ch, key, poa := testChainWithKey(t)

	// Mine 3 blocks.
	ts := uint64(1700000003)
	for i := 0; i < 3; i++ {
		mineBlock(t, ch, poa, key, ts)
		ts += 3
	}

	// Delete all undo data to simulate crash recovery scenario.
	for h := uint64(1); h <= 3; h++ {
		blk, _ := ch.blocks.GetBlockByHeight(h)
		ch.blocks.DeleteUndo(blk.Hash())
	}

	// Run RebuildUTXOs.
	if err := ch.RebuildUTXOs(); err != nil {
		t.Fatalf("RebuildUTXOs: %v", err)
	}

	// Verify undo data now exists for all blocks.
	for h := uint64(1); h <= 3; h++ {
		blk, err := ch.blocks.GetBlockByHeight(h)
		if err != nil {
			t.Fatalf("GetBlockByHeight(%d): %v", h, err)
		}
		undoBytes, err := ch.blocks.GetUndo(blk.Hash())
		if err != nil {
			t.Fatalf("GetUndo after rebuild at height %d: %v", h, err)
		}
		var undo UndoData
		if err := json.Unmarshal(undoBytes, &undo); err != nil {
			t.Fatalf("unmarshal undo at height %d: %v", h, err)
		}
		// Undo should have at least the coinbase output.
		if len(undo.CreatedOutpoints) == 0 {
			t.Errorf("undo at height %d has no created outpoints", h)
		}
	}
}

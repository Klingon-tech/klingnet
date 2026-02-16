package chain

import (
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

// reorgTestChain creates a chain with a genesis that allocates coins to
// the returned address, allowing blocks with real UTXO spending.
func reorgTestChain(t *testing.T) (*Chain, *crypto.PrivateKey, types.Address, *utxo.Store) {
	t.Helper()

	validatorKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	addr := crypto.AddressFromPubKey(validatorKey.PublicKey())

	poa, err := consensus.NewPoA([][]byte{validatorKey.PublicKey()})
	if err != nil {
		t.Fatalf("NewPoA: %v", err)
	}
	poa.SetSigner(validatorKey)

	db := storage.NewMemory()
	utxoStore := utxo.NewStore(db)

	ch, err := New(types.ChainID{}, db, utxoStore, poa)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	gen := &config.Genesis{
		ChainID:   "reorg-test",
		ChainName: "Reorg Test",
		Timestamp: 1700000000,
		Alloc: map[string]uint64{
			addr.String(): 100_000,
		},
		Protocol: config.ProtocolConfig{
			Consensus: config.ConsensusRules{
				Type:        config.ConsensusPoA,
				BlockTime:   3,
				BlockReward: 2000,
			},
		},
	}
	if err := ch.InitFromGenesis(gen); err != nil {
		t.Fatalf("InitFromGenesis: %v", err)
	}

	return ch, validatorKey, addr, utxoStore
}

// buildCoinbaseBlock creates a minimal valid block containing only a coinbase tx.
// The nonce parameter makes each block unique (different reward value).
func buildCoinbaseBlock(t *testing.T, ch *Chain, prevHash types.Hash, height uint64, addr types.Address, nonce uint64) *block.Block {
	t.Helper()

	reward := uint64(1000) + nonce

	coinbase := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  reward,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: addr[:]},
		}},
	}

	merkle := block.ComputeMerkleRoot([]types.Hash{coinbase.Hash()})
	header := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   prevHash,
		MerkleRoot: merkle,
		Timestamp:  1700000000 + height*3 + nonce,
		Height:     height,
	}
	blk := block.NewBlock(header, []*tx.Transaction{coinbase})

	poa := ch.engine.(*consensus.PoA)
	if err := poa.Seal(blk); err != nil {
		t.Fatalf("Seal block at height %d: %v", height, err)
	}
	return blk
}

func TestReorg_LongerForkWins(t *testing.T) {
	ch, _, addr, _ := reorgTestChain(t)

	// Genesis is at height 0. Build main chain: blocks 1, 2.
	genesisHash := ch.TipHash()

	blkA1 := buildCoinbaseBlock(t, ch, genesisHash, 1, addr, 0)
	if err := ch.ProcessBlock(blkA1); err != nil {
		t.Fatalf("process A1: %v", err)
	}
	blkA2 := buildCoinbaseBlock(t, ch, blkA1.Hash(), 2, addr, 0)
	if err := ch.ProcessBlock(blkA2); err != nil {
		t.Fatalf("process A2: %v", err)
	}

	if ch.Height() != 2 {
		t.Fatalf("expected height 2, got %d", ch.Height())
	}

	// Build fork from genesis: blocks B1, B2, B3 (longer).
	blkB1 := buildCoinbaseBlock(t, ch, genesisHash, 1, addr, 100)
	blkB2 := buildCoinbaseBlock(t, ch, blkB1.Hash(), 2, addr, 100)
	blkB3 := buildCoinbaseBlock(t, ch, blkB2.Hash(), 3, addr, 100)

	// Store B1, B2 first (ProcessBlock will detect fork and store them).
	if err := ch.ProcessBlock(blkB1); err != nil {
		t.Fatalf("process B1: %v", err)
	}
	// B1 at height 1 is not longer than current tip (height 2), so no reorg.
	if ch.Height() != 2 {
		t.Errorf("after B1: expected height 2, got %d", ch.Height())
	}

	if err := ch.ProcessBlock(blkB2); err != nil {
		t.Fatalf("process B2: %v", err)
	}
	// B2 at height 2 is same as current tip, no reorg.
	if ch.Height() != 2 {
		t.Errorf("after B2: expected height 2, got %d", ch.Height())
	}
	if ch.TipHash() != blkA2.Hash() {
		t.Errorf("after B2: tip should still be A2")
	}

	// B3 at height 3 is longer, should trigger reorg.
	if err := ch.ProcessBlock(blkB3); err != nil {
		t.Fatalf("process B3: %v", err)
	}

	if ch.Height() != 3 {
		t.Errorf("after reorg: expected height 3, got %d", ch.Height())
	}
	if ch.TipHash() != blkB3.Hash() {
		t.Errorf("after reorg: tip should be B3, got %s", ch.TipHash())
	}
}

func TestReorg_SameLengthNoSwitch(t *testing.T) {
	ch, _, addr, _ := reorgTestChain(t)

	genesisHash := ch.TipHash()

	// Main chain: A1.
	blkA1 := buildCoinbaseBlock(t, ch, genesisHash, 1, addr, 0)
	if err := ch.ProcessBlock(blkA1); err != nil {
		t.Fatalf("process A1: %v", err)
	}
	tipA := ch.TipHash()

	// Fork chain: B1 (same height).
	blkB1 := buildCoinbaseBlock(t, ch, genesisHash, 1, addr, 100)
	if err := ch.ProcessBlock(blkB1); err != nil {
		t.Fatalf("process B1: %v", err)
	}

	// Should not have reorged — same length.
	if ch.Height() != 1 {
		t.Errorf("expected height 1, got %d", ch.Height())
	}
	if ch.TipHash() != tipA {
		t.Errorf("tip should still be A1")
	}
}

func TestReorg_UTXOConsistency(t *testing.T) {
	ch, _, addr, utxoStore := reorgTestChain(t)
	genesisHash := ch.TipHash()

	// Main chain: A1, A2.
	blkA1 := buildCoinbaseBlock(t, ch, genesisHash, 1, addr, 0)
	if err := ch.ProcessBlock(blkA1); err != nil {
		t.Fatalf("process A1: %v", err)
	}
	blkA2 := buildCoinbaseBlock(t, ch, blkA1.Hash(), 2, addr, 0)
	if err := ch.ProcessBlock(blkA2); err != nil {
		t.Fatalf("process A2: %v", err)
	}

	// Remember a UTXO from A2's coinbase.
	a2CoinbaseTxHash := blkA2.Transactions[0].Hash()
	a2Op := types.Outpoint{TxID: a2CoinbaseTxHash, Index: 0}
	hasA2, _ := utxoStore.Has(a2Op)
	if !hasA2 {
		t.Fatal("A2 coinbase UTXO should exist before reorg")
	}

	// Build longer fork: B1, B2, B3.
	blkB1 := buildCoinbaseBlock(t, ch, genesisHash, 1, addr, 100)
	blkB2 := buildCoinbaseBlock(t, ch, blkB1.Hash(), 2, addr, 100)
	blkB3 := buildCoinbaseBlock(t, ch, blkB2.Hash(), 3, addr, 100)

	ch.ProcessBlock(blkB1)
	ch.ProcessBlock(blkB2)
	if err := ch.ProcessBlock(blkB3); err != nil {
		t.Fatalf("process B3: %v", err)
	}

	// After reorg: A2's coinbase UTXO should be gone.
	hasA2After, _ := utxoStore.Has(a2Op)
	if hasA2After {
		t.Error("A2 coinbase UTXO should not exist after reorg")
	}

	// B3's coinbase UTXO should exist.
	b3CoinbaseTxHash := blkB3.Transactions[0].Hash()
	b3Op := types.Outpoint{TxID: b3CoinbaseTxHash, Index: 0}
	hasB3, _ := utxoStore.Has(b3Op)
	if !hasB3 {
		t.Error("B3 coinbase UTXO should exist after reorg")
	}

	// Genesis UTXO should still exist (common ancestor).
	genesisBlk, _ := ch.GetBlockByHeight(0)
	genCoinbaseHash := genesisBlk.Transactions[0].Hash()
	genOp := types.Outpoint{TxID: genCoinbaseHash, Index: 0}
	hasGen, _ := utxoStore.Has(genOp)
	if !hasGen {
		t.Error("genesis UTXO should still exist after reorg")
	}
}

func TestReorg_SupplyAdjusted(t *testing.T) {
	ch, _, addr, _ := reorgTestChain(t)
	genesisHash := ch.TipHash()

	supplyAfterGenesis := ch.Supply()

	// Main chain: A1 (reward=1000), A2 (reward=1000).
	blkA1 := buildCoinbaseBlock(t, ch, genesisHash, 1, addr, 0)
	if err := ch.ProcessBlock(blkA1); err != nil {
		t.Fatalf("process A1: %v", err)
	}

	supplyAfterA1 := ch.Supply()
	if supplyAfterA1 != supplyAfterGenesis+1000 {
		t.Fatalf("supply after A1: got %d, want %d", supplyAfterA1, supplyAfterGenesis+1000)
	}

	blkA2 := buildCoinbaseBlock(t, ch, blkA1.Hash(), 2, addr, 0)
	if err := ch.ProcessBlock(blkA2); err != nil {
		t.Fatalf("process A2: %v", err)
	}

	// Fork: B1, B2, B3 (each with 1100 reward due to nonce=100).
	blkB1 := buildCoinbaseBlock(t, ch, genesisHash, 1, addr, 100)
	blkB2 := buildCoinbaseBlock(t, ch, blkB1.Hash(), 2, addr, 100)
	blkB3 := buildCoinbaseBlock(t, ch, blkB2.Hash(), 3, addr, 100)

	ch.ProcessBlock(blkB1)
	ch.ProcessBlock(blkB2)
	if err := ch.ProcessBlock(blkB3); err != nil {
		t.Fatalf("process B3: %v", err)
	}

	// After reorg: supply = genesis + 3 blocks × 1100 reward (nonce=100).
	expectedSupply := supplyAfterGenesis + 3*1100
	if ch.Supply() != expectedSupply {
		t.Errorf("supply after reorg: got %d, want %d", ch.Supply(), expectedSupply)
	}
}

func TestReorg_TxIndexUpdated(t *testing.T) {
	ch, _, addr, _ := reorgTestChain(t)
	genesisHash := ch.TipHash()

	// Main chain: A1.
	blkA1 := buildCoinbaseBlock(t, ch, genesisHash, 1, addr, 0)
	if err := ch.ProcessBlock(blkA1); err != nil {
		t.Fatalf("process A1: %v", err)
	}
	a1TxHash := blkA1.Transactions[0].Hash()

	// Verify A1 tx is in the index.
	if _, err := ch.GetTransaction(a1TxHash); err != nil {
		t.Fatalf("A1 tx should be in index: %v", err)
	}

	// Fork: B1, B2 (longer).
	blkB1 := buildCoinbaseBlock(t, ch, genesisHash, 1, addr, 100)
	blkB2 := buildCoinbaseBlock(t, ch, blkB1.Hash(), 2, addr, 100)

	ch.ProcessBlock(blkB1)
	if err := ch.ProcessBlock(blkB2); err != nil {
		t.Fatalf("process B2: %v", err)
	}

	// After reorg: A1 tx should not be findable (reverted).
	_, err := ch.GetTransaction(a1TxHash)
	if err == nil {
		t.Error("A1 tx should not be in index after reorg")
	}

	// B1 and B2 txs should be in index.
	b1TxHash := blkB1.Transactions[0].Hash()
	if _, err := ch.GetTransaction(b1TxHash); err != nil {
		t.Errorf("B1 tx should be in index: %v", err)
	}
	b2TxHash := blkB2.Transactions[0].Hash()
	if _, err := ch.GetTransaction(b2TxHash); err != nil {
		t.Errorf("B2 tx should be in index: %v", err)
	}
}

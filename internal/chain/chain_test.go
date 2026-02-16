package chain

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// testGenesis returns a minimal valid genesis config with an allocation.
func testGenesis(t *testing.T) (*config.Genesis, types.Address) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	addr := types.Address{}
	h := crypto.Hash(key.PublicKey())
	copy(addr[:], h[:types.AddressSize])

	return &config.Genesis{
		ChainID:   "test-chain-1",
		ChainName: "Test Chain",
		Timestamp: 1700000000,
		Alloc: map[string]uint64{
			addr.String(): 5000,
		},
		Protocol: config.ProtocolConfig{
			Consensus: config.ConsensusRules{
				Type:        config.ConsensusPoA,
				BlockTime:   3,
				BlockReward: 1000,
			},
			SubChain: config.SubChainRules{
				MaxDepth:       5,
				MaxPerParent:   10,
				AnchorInterval: 10,
			},
		},
	}, addr
}

// testChain creates a chain initialized from a genesis block with a single validator.
func testChain(t *testing.T) (*Chain, *crypto.PrivateKey, *config.Genesis) {
	t.Helper()

	validatorKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	poa, err := consensus.NewPoA([][]byte{validatorKey.PublicKey()})
	if err != nil {
		t.Fatalf("NewPoA: %v", err)
	}
	poa.SetSigner(validatorKey)

	db := storage.NewMemory()
	utxoStore := utxo.NewStore(db)

	ch, err := New(types.ChainID{}, db, utxoStore, poa)
	if err != nil {
		t.Fatalf("New chain: %v", err)
	}

	addr := crypto.AddressFromPubKey(validatorKey.PublicKey())
	gen := &config.Genesis{
		ChainID:   "test-chain-1",
		ChainName: "Test Chain",
		Timestamp: 1700000000,
		Alloc: map[string]uint64{
			addr.String(): 5000,
		},
		Protocol: config.ProtocolConfig{
			Consensus: config.ConsensusRules{
				Type:        config.ConsensusPoA,
				BlockTime:   3,
				BlockReward: 1000,
			},
			SubChain: config.SubChainRules{
				MaxDepth:       5,
				MaxPerParent:   10,
				AnchorInterval: 10,
			},
		},
	}
	if err := ch.InitFromGenesis(gen); err != nil {
		t.Fatalf("InitFromGenesis: %v", err)
	}

	return ch, validatorKey, gen
}

// buildSignedBlock creates a signed block at the given height that spends
// the given prevOut and creates a new output.
func buildSignedBlock(t *testing.T, ch *Chain, key *crypto.PrivateKey, _ *crypto.PrivateKey, prevOut types.Outpoint, value uint64) *block.Block {
	t.Helper()

	// Coinbase tx (zero outpoint).
	coinbase := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
		}},
	}

	// User tx spending prevOut.
	spendAddr := crypto.AddressFromPubKey(key.PublicKey())
	b := tx.NewBuilder().
		AddInput(prevOut).
		AddOutput(value, types.Script{Type: types.ScriptTypeP2PKH, Data: spendAddr.Bytes()})
	b.Sign(key)
	userTx := b.Build()

	txs := []*tx.Transaction{coinbase, userTx}

	state := ch.State()
	hashes := []types.Hash{txs[0].Hash(), txs[1].Hash()}
	merkle := block.ComputeMerkleRoot(hashes)
	header := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   state.TipHash,
		MerkleRoot: merkle,
		Timestamp:  1700000001 + state.Height,
		Height:     state.Height + 1,
	}
	blk := block.NewBlock(header, txs)

	// Seal with validator.
	poa := ch.engine.(*consensus.PoA)
	if err := poa.Seal(blk); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return blk
}

// testCoinbaseTx returns a minimal coinbase transaction for test blocks.
func testCoinbaseTx() *tx.Transaction {
	return &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
		}},
	}
}

// --- Genesis Tests ---

func TestCreateGenesisBlock(t *testing.T) {
	gen, _ := testGenesis(t)
	blk, err := CreateGenesisBlock(gen)
	if err != nil {
		t.Fatalf("CreateGenesisBlock: %v", err)
	}
	if blk.Header.Height != 0 {
		t.Errorf("genesis height = %d, want 0", blk.Header.Height)
	}
	if !blk.Header.PrevHash.IsZero() {
		t.Error("genesis PrevHash should be zero")
	}
	if blk.Header.Timestamp != gen.Timestamp {
		t.Errorf("timestamp = %d, want %d", blk.Header.Timestamp, gen.Timestamp)
	}
	if len(blk.Transactions) != 1 {
		t.Fatalf("genesis should have 1 tx, got %d", len(blk.Transactions))
	}
	if blk.Hash().IsZero() {
		t.Error("genesis hash should not be zero")
	}
}

func TestCreateGenesisBlock_WithAlloc(t *testing.T) {
	gen, addr := testGenesis(t)
	blk, err := CreateGenesisBlock(gen)
	if err != nil {
		t.Fatalf("CreateGenesisBlock: %v", err)
	}

	coinbase := blk.Transactions[0]
	if len(coinbase.Outputs) != 1 {
		t.Fatalf("coinbase should have 1 output, got %d", len(coinbase.Outputs))
	}
	out := coinbase.Outputs[0]
	if out.Value != 5000 {
		t.Errorf("output value = %d, want 5000", out.Value)
	}
	if out.Script.Type != types.ScriptTypeP2PKH {
		t.Errorf("script type = %d, want P2PKH", out.Script.Type)
	}
	var outAddr types.Address
	copy(outAddr[:], out.Script.Data)
	if outAddr != addr {
		t.Errorf("output address mismatch")
	}
}

func TestCreateGenesisBlock_NoAlloc(t *testing.T) {
	gen := &config.Genesis{
		ChainID:   "test",
		Timestamp: 1000,
		Alloc:     nil,
		Protocol: config.ProtocolConfig{
			Consensus: config.ConsensusRules{Type: "poa", BlockTime: 3},
			SubChain:  config.SubChainRules{MaxDepth: 5, MaxPerParent: 10, AnchorInterval: 10},
		},
	}
	blk, err := CreateGenesisBlock(gen)
	if err != nil {
		t.Fatalf("CreateGenesisBlock: %v", err)
	}
	// Should still produce a block with a zero-value coinbase.
	if len(blk.Transactions) != 1 {
		t.Fatalf("should have 1 tx, got %d", len(blk.Transactions))
	}
	if blk.Transactions[0].Outputs[0].Value != 0 {
		t.Errorf("no-alloc coinbase output should be 0, got %d", blk.Transactions[0].Outputs[0].Value)
	}
}

func TestCreateGenesisBlock_NilConfig(t *testing.T) {
	_, err := CreateGenesisBlock(nil)
	if err == nil {
		t.Error("should fail with nil config")
	}
}

func TestCreateGenesisBlock_InvalidAllocAddress(t *testing.T) {
	gen := &config.Genesis{
		ChainID:   "test",
		Timestamp: 1000,
		Alloc:     map[string]uint64{"not-hex": 100},
		Protocol: config.ProtocolConfig{
			Consensus: config.ConsensusRules{Type: "poa", BlockTime: 3},
			SubChain:  config.SubChainRules{MaxDepth: 5, MaxPerParent: 10, AnchorInterval: 10},
		},
	}
	_, err := CreateGenesisBlock(gen)
	if err == nil {
		t.Error("should fail with invalid hex address")
	}
}

func TestCreateGenesisBlock_WrongLengthAddress(t *testing.T) {
	gen := &config.Genesis{
		ChainID:   "test",
		Timestamp: 1000,
		Alloc:     map[string]uint64{"aabb": 100}, // 2 bytes, not 20
		Protocol: config.ProtocolConfig{
			Consensus: config.ConsensusRules{Type: "poa", BlockTime: 3},
			SubChain:  config.SubChainRules{MaxDepth: 5, MaxPerParent: 10, AnchorInterval: 10},
		},
	}
	_, err := CreateGenesisBlock(gen)
	if err == nil {
		t.Error("should fail with wrong length address")
	}
}

func TestCreateGenesisBlock_Deterministic(t *testing.T) {
	gen, _ := testGenesis(t)
	blk1, _ := CreateGenesisBlock(gen)
	blk2, _ := CreateGenesisBlock(gen)
	if blk1.Hash() != blk2.Hash() {
		t.Error("genesis block should be deterministic")
	}
}

// --- BlockStore Tests ---

func TestBlockStore_PutGetBlock(t *testing.T) {
	db := storage.NewMemory()
	bs := NewBlockStore(db)

	blk := makeTestBlock(t, 1, types.Hash{0x01})
	if err := bs.PutBlock(blk); err != nil {
		t.Fatalf("PutBlock: %v", err)
	}

	got, err := bs.GetBlock(blk.Hash())
	if err != nil {
		t.Fatalf("GetBlock: %v", err)
	}
	if got.Hash() != blk.Hash() {
		t.Errorf("hash mismatch: got %s, want %s", got.Hash(), blk.Hash())
	}
}

func TestBlockStore_GetBlockByHeight(t *testing.T) {
	db := storage.NewMemory()
	bs := NewBlockStore(db)

	blk := makeTestBlock(t, 5, types.Hash{0x05})
	bs.PutBlock(blk)

	got, err := bs.GetBlockByHeight(5)
	if err != nil {
		t.Fatalf("GetBlockByHeight: %v", err)
	}
	if got.Hash() != blk.Hash() {
		t.Error("block by height should match")
	}
}

func TestBlockStore_HasBlock(t *testing.T) {
	db := storage.NewMemory()
	bs := NewBlockStore(db)

	blk := makeTestBlock(t, 1, types.Hash{})
	bs.PutBlock(blk)

	has, _ := bs.HasBlock(blk.Hash())
	if !has {
		t.Error("HasBlock should return true")
	}

	has, _ = bs.HasBlock(types.Hash{0xff})
	if has {
		t.Error("HasBlock should return false for unknown hash")
	}
}

func TestBlockStore_SetGetTip(t *testing.T) {
	db := storage.NewMemory()
	bs := NewBlockStore(db)

	hash := types.Hash{0xaa, 0xbb}
	if err := bs.SetTip(hash, 42, 99000); err != nil {
		t.Fatalf("SetTip: %v", err)
	}

	gotHash, gotHeight, gotSupply, err := bs.GetTip()
	if err != nil {
		t.Fatalf("GetTip: %v", err)
	}
	if gotHash != hash {
		t.Errorf("tip hash = %s, want %s", gotHash, hash)
	}
	if gotHeight != 42 {
		t.Errorf("tip height = %d, want 42", gotHeight)
	}
	if gotSupply != 99000 {
		t.Errorf("tip supply = %d, want 99000", gotSupply)
	}
}

func TestBlockStore_GetTip_Empty(t *testing.T) {
	db := storage.NewMemory()
	bs := NewBlockStore(db)

	hash, height, supply, err := bs.GetTip()
	if err != nil {
		t.Fatalf("GetTip: %v", err)
	}
	if !hash.IsZero() {
		t.Error("empty store tip should be zero hash")
	}
	if height != 0 {
		t.Errorf("empty store height = %d, want 0", height)
	}
	if supply != 0 {
		t.Errorf("empty store supply = %d, want 0", supply)
	}
}

func TestBlockStore_GetBlock_NotFound(t *testing.T) {
	db := storage.NewMemory()
	bs := NewBlockStore(db)

	_, err := bs.GetBlock(types.Hash{0x01})
	if err == nil {
		t.Error("GetBlock should fail for unknown hash")
	}
}

// --- Transaction Index Tests ---

func TestBlockStore_TxIndex(t *testing.T) {
	db := storage.NewMemory()
	bs := NewBlockStore(db)

	blk := makeTestBlock(t, 1, types.Hash{0x01})
	if err := bs.PutBlock(blk); err != nil {
		t.Fatalf("PutBlock: %v", err)
	}

	// Should be able to look up each transaction in the block.
	for _, txn := range blk.Transactions {
		txHash := txn.Hash()
		height, blockHash, err := bs.GetTxLocation(txHash)
		if err != nil {
			t.Fatalf("GetTxLocation(%s): %v", txHash, err)
		}
		if height != 1 {
			t.Errorf("tx location height = %d, want 1", height)
		}
		if blockHash != blk.Hash() {
			t.Errorf("tx location blockHash = %s, want %s", blockHash, blk.Hash())
		}
	}
}

func TestBlockStore_TxIndex_NotFound(t *testing.T) {
	db := storage.NewMemory()
	bs := NewBlockStore(db)

	_, _, err := bs.GetTxLocation(types.Hash{0xff})
	if err == nil {
		t.Error("GetTxLocation should fail for unknown tx")
	}
}

func TestBlockStore_DeleteTxIndex(t *testing.T) {
	db := storage.NewMemory()
	bs := NewBlockStore(db)

	blk := makeTestBlock(t, 1, types.Hash{0x01})
	bs.PutBlock(blk)

	txHash := blk.Transactions[0].Hash()

	// Should exist.
	_, _, err := bs.GetTxLocation(txHash)
	if err != nil {
		t.Fatalf("GetTxLocation: %v", err)
	}

	// Delete.
	if err := bs.DeleteTxIndex(txHash); err != nil {
		t.Fatalf("DeleteTxIndex: %v", err)
	}

	// Should not exist.
	_, _, err = bs.GetTxLocation(txHash)
	if err == nil {
		t.Error("GetTxLocation should fail after delete")
	}
}

func TestChain_GetTransaction(t *testing.T) {
	ch, _, _ := testChain(t)

	// Genesis block txs should be indexed.
	genesisBlock, _ := ch.GetBlockByHeight(0)
	coinbaseTx := genesisBlock.Transactions[0]
	txHash := coinbaseTx.Hash()

	got, err := ch.GetTransaction(txHash)
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if got.Hash() != txHash {
		t.Errorf("GetTransaction hash = %s, want %s", got.Hash(), txHash)
	}
}

func TestChain_GetTransaction_NotFound(t *testing.T) {
	ch, _, _ := testChain(t)

	_, err := ch.GetTransaction(types.Hash{0xde, 0xad})
	if err == nil {
		t.Error("GetTransaction should fail for unknown tx")
	}
}

// --- Chain Init Tests ---

func TestChain_New(t *testing.T) {
	key, _ := crypto.GenerateKey()
	poa, _ := consensus.NewPoA([][]byte{key.PublicKey()})
	db := storage.NewMemory()
	utxoStore := utxo.NewStore(db)

	ch, err := New(types.ChainID{}, db, utxoStore, poa)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !ch.TipHash().IsZero() {
		t.Error("fresh chain tip should be zero")
	}
	if ch.Height() != 0 {
		t.Errorf("fresh chain height = %d, want 0", ch.Height())
	}
}

func TestChain_New_NilDB(t *testing.T) {
	key, _ := crypto.GenerateKey()
	poa, _ := consensus.NewPoA([][]byte{key.PublicKey()})
	utxoStore := utxo.NewStore(storage.NewMemory())

	_, err := New(types.ChainID{}, nil, utxoStore, poa)
	if err == nil {
		t.Error("should fail with nil db")
	}
}

func TestChain_New_NilUTXOSet(t *testing.T) {
	key, _ := crypto.GenerateKey()
	poa, _ := consensus.NewPoA([][]byte{key.PublicKey()})
	db := storage.NewMemory()

	_, err := New(types.ChainID{}, db, nil, poa)
	if err == nil {
		t.Error("should fail with nil utxo set")
	}
}

func TestChain_New_NilEngine(t *testing.T) {
	db := storage.NewMemory()
	utxoStore := utxo.NewStore(db)

	_, err := New(types.ChainID{}, db, utxoStore, nil)
	if err == nil {
		t.Error("should fail with nil engine")
	}
}

func TestChain_InitFromGenesis(t *testing.T) {
	ch, _, gen := testChain(t)

	// Chain should be at height 0 with a non-zero tip.
	if ch.Height() != 0 {
		t.Errorf("height = %d, want 0", ch.Height())
	}
	if ch.TipHash().IsZero() {
		t.Error("tip should not be zero after genesis init")
	}

	// Should be able to retrieve the genesis block.
	blk, err := ch.GetBlockByHeight(0)
	if err != nil {
		t.Fatalf("GetBlockByHeight(0): %v", err)
	}
	if blk.Header.Height != 0 {
		t.Errorf("genesis block height = %d", blk.Header.Height)
	}
	if blk.Header.Timestamp != gen.Timestamp {
		t.Errorf("genesis timestamp = %d, want %d", blk.Header.Timestamp, gen.Timestamp)
	}
}

func TestChain_InitFromGenesis_AllocCreatesUTXOs(t *testing.T) {
	ch, _, _ := testChain(t)

	// The genesis coinbase tx should have created a UTXO.
	genesisBlock, _ := ch.GetBlockByHeight(0)
	coinbaseTx := genesisBlock.Transactions[0]
	txHash := coinbaseTx.Hash()

	outpoint := types.Outpoint{TxID: txHash, Index: 0}
	has, err := ch.utxos.Has(outpoint)
	if err != nil {
		t.Fatalf("UTXO Has: %v", err)
	}
	if !has {
		t.Error("genesis allocation should create a UTXO")
	}

	u, err := ch.utxos.Get(outpoint)
	if err != nil {
		t.Fatalf("UTXO Get: %v", err)
	}
	if u.Value != 5000 {
		t.Errorf("UTXO value = %d, want 5000", u.Value)
	}
}

func TestChain_InitFromGenesis_DoubleInit(t *testing.T) {
	ch, _, gen := testChain(t)

	err := ch.InitFromGenesis(gen)
	if err == nil {
		t.Error("double InitFromGenesis should fail")
	}
}

// --- ProcessBlock Tests ---

func TestChain_ProcessBlock(t *testing.T) {
	ch, validatorKey, _ := testChain(t)

	// Get the genesis UTXO to spend.
	genesisBlock, _ := ch.GetBlockByHeight(0)
	coinbaseTx := genesisBlock.Transactions[0]
	prevOut := types.Outpoint{TxID: coinbaseTx.Hash(), Index: 0}

	// We need a key that can sign for spending.
	spendKey := validatorKey
	blk := buildSignedBlock(t, ch, spendKey, validatorKey, prevOut, 4000)

	if err := ch.ProcessBlock(blk); err != nil {
		t.Fatalf("ProcessBlock: %v", err)
	}

	if ch.Height() != 1 {
		t.Errorf("height = %d, want 1", ch.Height())
	}
	if ch.TipHash() != blk.Hash() {
		t.Error("tip should be the new block")
	}
}

func TestChain_ProcessBlock_DuplicateBlock(t *testing.T) {
	ch, validatorKey, _ := testChain(t)

	genesisBlock, _ := ch.GetBlockByHeight(0)
	prevOut := types.Outpoint{TxID: genesisBlock.Transactions[0].Hash(), Index: 0}

	spendKey := validatorKey
	blk := buildSignedBlock(t, ch, spendKey, validatorKey, prevOut, 4000)

	ch.ProcessBlock(blk)

	err := ch.ProcessBlock(blk)
	if !errors.Is(err, ErrBlockKnown) {
		t.Errorf("expected ErrBlockKnown, got: %v", err)
	}
}

func TestChain_ProcessBlock_BadPrevHash(t *testing.T) {
	ch, validatorKey, _ := testChain(t)

	coinbase := testCoinbaseTx()
	txs := []*tx.Transaction{coinbase}
	hashes := []types.Hash{coinbase.Hash()}
	merkle := block.ComputeMerkleRoot(hashes)
	header := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   types.Hash{0xff, 0xff}, // Wrong prev hash.
		MerkleRoot: merkle,
		Timestamp:  1700000002,
		Height:     1,
	}
	blk := block.NewBlock(header, txs)

	poa := ch.engine.(*consensus.PoA)
	poa.Seal(blk)

	err := ch.ProcessBlock(blk)
	if !errors.Is(err, ErrPrevNotFound) {
		t.Errorf("expected ErrPrevNotFound, got: %v", err)
	}
	_ = validatorKey
}

func TestChain_ProcessBlock_BadHeight(t *testing.T) {
	ch, validatorKey, _ := testChain(t)

	coinbase := testCoinbaseTx()
	txs := []*tx.Transaction{coinbase}
	hashes := []types.Hash{coinbase.Hash()}

	state := ch.State()
	merkle := block.ComputeMerkleRoot(hashes)
	header := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   state.TipHash,
		MerkleRoot: merkle,
		Timestamp:  1700000002,
		Height:     99, // Wrong height, should be 1.
	}
	blk := block.NewBlock(header, txs)

	poa := ch.engine.(*consensus.PoA)
	poa.Seal(blk)

	err := ch.ProcessBlock(blk)
	if !errors.Is(err, ErrBadHeight) {
		t.Errorf("expected ErrBadHeight, got: %v", err)
	}
	_ = validatorKey
}

func TestChain_ProcessBlock_NoValidatorSig(t *testing.T) {
	ch, _, _ := testChain(t)

	coinbase := testCoinbaseTx()
	txs := []*tx.Transaction{coinbase}
	hashes := []types.Hash{coinbase.Hash()}

	state := ch.State()
	merkle := block.ComputeMerkleRoot(hashes)
	header := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   state.TipHash,
		MerkleRoot: merkle,
		Timestamp:  1700000002,
		Height:     1,
	}
	blk := block.NewBlock(header, txs)
	// Don't seal — no validator sig.

	err := ch.ProcessBlock(blk)
	if err == nil {
		t.Error("ProcessBlock should fail without validator sig")
	}
}

func TestChain_ProcessBlock_NilBlock(t *testing.T) {
	ch, _, _ := testChain(t)

	err := ch.ProcessBlock(nil)
	if err == nil {
		t.Error("ProcessBlock(nil) should fail")
	}
}

func TestChain_ProcessBlock_MultipleBlocks(t *testing.T) {
	ch, validatorKey, _ := testChain(t)

	// Build two blocks in sequence.
	genesisBlock, _ := ch.GetBlockByHeight(0)
	prevOut := types.Outpoint{TxID: genesisBlock.Transactions[0].Hash(), Index: 0}

	key := validatorKey
	blk1 := buildSignedBlock(t, ch, key, validatorKey, prevOut, 4000)
	if err := ch.ProcessBlock(blk1); err != nil {
		t.Fatalf("ProcessBlock(1): %v", err)
	}

	// Block 2 spends user tx output from block 1 (index 1; coinbase is index 0).
	blk1Tx := blk1.Transactions[1]
	prevOut2 := types.Outpoint{TxID: blk1Tx.Hash(), Index: 0}
	blk2 := buildSignedBlock(t, ch, key, validatorKey, prevOut2, 3000)
	if err := ch.ProcessBlock(blk2); err != nil {
		t.Fatalf("ProcessBlock(2): %v", err)
	}

	if ch.Height() != 2 {
		t.Errorf("height = %d, want 2", ch.Height())
	}

	// Retrieve both blocks.
	got1, _ := ch.GetBlockByHeight(1)
	got2, _ := ch.GetBlockByHeight(2)
	if got1.Hash() != blk1.Hash() {
		t.Error("block 1 hash mismatch")
	}
	if got2.Hash() != blk2.Hash() {
		t.Error("block 2 hash mismatch")
	}
}

func TestChain_ProcessBlock_UTXOSpent(t *testing.T) {
	ch, validatorKey, _ := testChain(t)

	genesisBlock, _ := ch.GetBlockByHeight(0)
	prevOut := types.Outpoint{TxID: genesisBlock.Transactions[0].Hash(), Index: 0}

	key := validatorKey
	blk := buildSignedBlock(t, ch, key, validatorKey, prevOut, 4000)
	ch.ProcessBlock(blk)

	// The genesis UTXO should be spent.
	has, _ := ch.utxos.Has(prevOut)
	if has {
		t.Error("spent UTXO should be deleted")
	}

	// The new output should exist (user tx is at index 1, coinbase at 0).
	newOut := types.Outpoint{TxID: blk.Transactions[1].Hash(), Index: 0}
	has, _ = ch.utxos.Has(newOut)
	if !has {
		t.Error("new UTXO should exist")
	}

	u, _ := ch.utxos.Get(newOut)
	if u.Value != 4000 {
		t.Errorf("new UTXO value = %d, want 4000", u.Value)
	}
	if u.Height != 1 {
		t.Errorf("new UTXO height = %d, want 1", u.Height)
	}
}

func TestChain_GetBlock(t *testing.T) {
	ch, _, _ := testChain(t)

	tip := ch.TipHash()
	blk, err := ch.GetBlock(tip)
	if err != nil {
		t.Fatalf("GetBlock: %v", err)
	}
	if blk.Hash() != tip {
		t.Error("GetBlock should return the genesis block")
	}
}

func TestChain_State(t *testing.T) {
	ch, _, _ := testChain(t)

	s := ch.State()
	if s.Height != 0 {
		t.Errorf("state height = %d, want 0", s.Height)
	}
	if s.TipHash.IsZero() {
		t.Error("state tip should not be zero after genesis")
	}
}

// --- Config Genesis Hash Tests ---

func TestGenesisConfig_Hash(t *testing.T) {
	gen, _ := testGenesis(t)
	hash, err := gen.Hash()
	if err != nil {
		t.Fatalf("Genesis.Hash: %v", err)
	}
	if hash.IsZero() {
		t.Error("genesis config hash should not be zero")
	}

	// Deterministic.
	hash2, _ := gen.Hash()
	if hash != hash2 {
		t.Error("genesis config hash should be deterministic")
	}
}

func TestGenesisConfig_Hash_DifferentConfigs(t *testing.T) {
	gen1 := &config.Genesis{
		ChainID:   "chain-a",
		Timestamp: 1000,
		Protocol: config.ProtocolConfig{
			Consensus: config.ConsensusRules{Type: "poa", BlockTime: 3},
			SubChain:  config.SubChainRules{MaxDepth: 5, MaxPerParent: 10, AnchorInterval: 10},
		},
	}
	gen2 := &config.Genesis{
		ChainID:   "chain-b",
		Timestamp: 2000,
		Protocol: config.ProtocolConfig{
			Consensus: config.ConsensusRules{Type: "poa", BlockTime: 3},
			SubChain:  config.SubChainRules{MaxDepth: 5, MaxPerParent: 10, AnchorInterval: 10},
		},
	}

	h1, _ := gen1.Hash()
	h2, _ := gen2.Hash()
	if h1 == h2 {
		t.Error("different genesis configs should produce different hashes")
	}
}

// --- State Tests ---

func TestState_IsGenesis(t *testing.T) {
	s := &State{}
	if !s.IsGenesis() {
		t.Error("zero state should be genesis")
	}

	s.Height = 1
	if s.IsGenesis() {
		t.Error("non-zero height is not genesis")
	}

	s.Height = 0
	s.TipHash = types.Hash{0x01}
	if s.IsGenesis() {
		t.Error("non-zero tip is not genesis")
	}
}

// --- Unstaking Tests ---

// buildCustomBlock creates a signed block with the given transactions.
func buildCustomBlock(t *testing.T, ch *Chain, txs []*tx.Transaction) *block.Block {
	t.Helper()
	state := ch.State()
	hashes := make([]types.Hash, len(txs))
	for i, transaction := range txs {
		hashes[i] = transaction.Hash()
	}
	merkle := block.ComputeMerkleRoot(hashes)
	header := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   state.TipHash,
		MerkleRoot: merkle,
		Timestamp:  1700000001 + state.Height,
		Height:     state.Height + 1,
	}
	blk := block.NewBlock(header, txs)
	poa := ch.engine.(*consensus.PoA)
	if err := poa.Seal(blk); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return blk
}

func TestChain_ProcessBlock_UnstakeHandler(t *testing.T) {
	ch, _, _ := testChain(t)

	stakeKey, _ := crypto.GenerateKey()
	stakePubKey := stakeKey.PublicKey()

	// Step 1: produce a block with coinbase UTXO for the staker.
	coinbase1 := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: crypto.AddressFromPubKey(stakeKey.PublicKey()).Bytes()},
		}},
	}
	blk1 := buildCustomBlock(t, ch, []*tx.Transaction{coinbase1})
	if err := ch.ProcessBlock(blk1); err != nil {
		t.Fatalf("ProcessBlock (block 1): %v", err)
	}

	// Produce enough blocks for coinbase maturity (20 blocks).
	for i := 0; i < int(config.CoinbaseMaturity); i++ {
		cb := &tx.Transaction{
			Version: 1,
			Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
			Outputs: []tx.Output{{
				Value:  1000,
				Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
			}},
		}
		blk := buildCustomBlock(t, ch, []*tx.Transaction{cb})
		if err := ch.ProcessBlock(blk); err != nil {
			t.Fatalf("ProcessBlock (maturity block %d): %v", i, err)
		}
	}

	// Step 2: Spend coinbase to create stake UTXO.
	coinbaseOut := types.Outpoint{TxID: coinbase1.Hash(), Index: 0}
	stakeTxBuilder := tx.NewBuilder().
		AddInput(coinbaseOut).
		AddOutput(900, types.Script{Type: types.ScriptTypeStake, Data: stakePubKey})
	stakeTxBuilder.Sign(stakeKey)
	stakeTx := stakeTxBuilder.Build()

	coinbase2 := testCoinbaseTx()
	blkStake := buildCustomBlock(t, ch, []*tx.Transaction{coinbase2, stakeTx})

	// Set stake handler to track.
	var stakeHandlerCalled bool
	ch.SetStakeHandler(func(pubKey []byte) {
		stakeHandlerCalled = true
	})

	if err := ch.ProcessBlock(blkStake); err != nil {
		t.Fatalf("ProcessBlock (stake block): %v", err)
	}
	if !stakeHandlerCalled {
		t.Error("stake handler should have been called")
	}

	// Step 3: Spend the stake UTXO (unstake).
	stakeOut := types.Outpoint{TxID: stakeTx.Hash(), Index: 0}
	unstakeTxBuilder := tx.NewBuilder().
		AddInput(stakeOut).
		AddOutput(800, types.Script{Type: types.ScriptTypeP2PKH, Data: crypto.AddressFromPubKey(stakePubKey).Bytes()})
	unstakeTxBuilder.Sign(stakeKey)
	unstakeTx := unstakeTxBuilder.Build()

	coinbase3 := testCoinbaseTx()
	blkUnstake := buildCustomBlock(t, ch, []*tx.Transaction{coinbase3, unstakeTx})

	// Set unstake handler to track.
	var unstakeHandlerCalled bool
	ch.SetUnstakeHandler(func(pubKey []byte) {
		unstakeHandlerCalled = true
	})

	if err := ch.ProcessBlock(blkUnstake); err != nil {
		t.Fatalf("ProcessBlock (unstake block): %v", err)
	}
	if !unstakeHandlerCalled {
		t.Error("unstake handler should have been called")
	}
}

func TestChain_ProcessBlock_UnstakeCooldown(t *testing.T) {
	ch, _, _ := testChain(t)

	// Step 1: produce a block with coinbase UTXO for the staker.
	stakeKey, _ := crypto.GenerateKey()
	stakePubKey := stakeKey.PublicKey()

	coinbase1 := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: crypto.AddressFromPubKey(stakePubKey).Bytes()},
		}},
	}
	blk1 := buildCustomBlock(t, ch, []*tx.Transaction{coinbase1})
	if err := ch.ProcessBlock(blk1); err != nil {
		t.Fatalf("ProcessBlock (block 1): %v", err)
	}

	// Produce maturity blocks.
	for i := 0; i < int(config.CoinbaseMaturity); i++ {
		cb := testCoinbaseTx()
		blk := buildCustomBlock(t, ch, []*tx.Transaction{cb})
		if err := ch.ProcessBlock(blk); err != nil {
			t.Fatalf("ProcessBlock (maturity block %d): %v", i, err)
		}
	}

	// Step 2: Stake.
	coinbaseOut := types.Outpoint{TxID: coinbase1.Hash(), Index: 0}
	stakeTxBuilder := tx.NewBuilder().
		AddInput(coinbaseOut).
		AddOutput(900, types.Script{Type: types.ScriptTypeStake, Data: stakePubKey})
	stakeTxBuilder.Sign(stakeKey)
	stakeTx := stakeTxBuilder.Build()

	coinbase2 := testCoinbaseTx()
	blkStake := buildCustomBlock(t, ch, []*tx.Transaction{coinbase2, stakeTx})
	if err := ch.ProcessBlock(blkStake); err != nil {
		t.Fatalf("ProcessBlock (stake block): %v", err)
	}

	// Step 3: Unstake.
	stakeOut := types.Outpoint{TxID: stakeTx.Hash(), Index: 0}
	unstakeTxBuilder := tx.NewBuilder().
		AddInput(stakeOut).
		AddOutput(800, types.Script{Type: types.ScriptTypeP2PKH, Data: crypto.AddressFromPubKey(stakePubKey).Bytes()})
	unstakeTxBuilder.Sign(stakeKey)
	unstakeTx := unstakeTxBuilder.Build()

	coinbase3 := testCoinbaseTx()
	blkUnstake := buildCustomBlock(t, ch, []*tx.Transaction{coinbase3, unstakeTx})
	if err := ch.ProcessBlock(blkUnstake); err != nil {
		t.Fatalf("ProcessBlock (unstake block): %v", err)
	}

	// Check that the output from the unstake tx has LockedUntil set.
	returnOut := types.Outpoint{TxID: unstakeTx.Hash(), Index: 0}
	u, err := ch.utxos.Get(returnOut)
	if err != nil {
		t.Fatalf("Get return UTXO: %v", err)
	}

	expectedLock := blkUnstake.Header.Height + config.UnstakeCooldown
	if u.LockedUntil != expectedLock {
		t.Errorf("LockedUntil = %d, want %d", u.LockedUntil, expectedLock)
	}

	// Verify the lock is enforced: try to spend it before the cooldown expires.
	// The block height needs to be < LockedUntil for the lock to trigger.
	spendBuilder := tx.NewBuilder().
		AddInput(returnOut).
		AddOutput(700, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	spendBuilder.Sign(stakeKey)
	spendTx := spendBuilder.Build()

	coinbase4 := testCoinbaseTx()
	blkSpendEarly := buildCustomBlock(t, ch, []*tx.Transaction{coinbase4, spendTx})

	err = ch.ProcessBlock(blkSpendEarly)
	if err == nil {
		t.Error("spending locked output before cooldown should fail")
	}
}

// --- Helpers ---

func makeTestBlock(t *testing.T, height uint64, prevHash types.Hash) *block.Block {
	t.Helper()
	key, _ := crypto.GenerateKey()
	b := tx.NewBuilder().
		AddInput(types.Outpoint{TxID: types.Hash{0x01}, Index: 0}).
		AddOutput(1000, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b.Sign(key)
	transaction := b.Build()

	merkle := block.ComputeMerkleRoot([]types.Hash{transaction.Hash()})
	header := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   prevHash,
		MerkleRoot: merkle,
		Timestamp:  1700000000 + height,
		Height:     height,
	}
	return block.NewBlock(header, []*tx.Transaction{transaction})
}

// --- Supply Cap Tests ---

func TestProcessBlock_SupplyCapEnforced(t *testing.T) {
	// Create a genesis with maxSupply = 7000, alloc = 5000, blockReward = 1000.
	validatorKey, _ := crypto.GenerateKey()
	poa, _ := consensus.NewPoA([][]byte{validatorKey.PublicKey()})
	poa.SetSigner(validatorKey)

	db := storage.NewMemory()
	utxoStore := utxo.NewStore(db)
	ch, _ := New(types.ChainID{}, db, utxoStore, poa)

	addr := crypto.AddressFromPubKey(validatorKey.PublicKey())
	gen := &config.Genesis{
		ChainID:   "test-supply",
		ChainName: "Test",
		Timestamp: 1700000000,
		Alloc:     map[string]uint64{addr.String(): 5000},
		Protocol: config.ProtocolConfig{
			Consensus: config.ConsensusRules{
				Type:        config.ConsensusPoA,
				BlockTime:   3,
				BlockReward: 1000,
				MaxSupply:   7000,
				Validators:  []string{hex.EncodeToString(validatorKey.PublicKey())},
			},
			SubChain: config.SubChainRules{MaxDepth: 1, MaxPerParent: 10, AnchorInterval: 10},
		},
	}
	if err := ch.InitFromGenesis(gen); err != nil {
		t.Fatalf("InitFromGenesis: %v", err)
	}

	// Produce 2 valid blocks.
	// Supply starts at 5000 (alloc). With max supply 7000:
	// Block 1: reward=1000 -> supply=6000
	// Block 2: reward=1000 -> supply=7000 (cap reached).
	for i := 0; i < 2; i++ {
		coinbase := &tx.Transaction{
			Version: 1,
			Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
			Outputs: []tx.Output{{
				Value:  1000,
				Script: types.Script{Type: types.ScriptTypeP2PKH, Data: addr[:]},
			}},
		}
		blk := buildCustomBlock(t, ch, []*tx.Transaction{coinbase})
		if err := ch.ProcessBlock(blk); err != nil {
			t.Fatalf("block %d: %v", i+1, err)
		}
	}

	// A third block that tries to mint beyond cap must be rejected by consensus.
	coinbase3 := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: addr[:]},
		}},
	}
	blk3 := buildCustomBlock(t, ch, []*tx.Transaction{coinbase3})
	if err := ch.ProcessBlock(blk3); !errors.Is(err, ErrCoinbaseRewardExceeded) {
		t.Fatalf("expected ErrCoinbaseRewardExceeded at cap, got: %v", err)
	}

	// Supply should remain capped at 7000 (5000 alloc + 2000 new).
	if ch.Supply() != 7000 {
		t.Errorf("supply = %d, want 7000", ch.Supply())
	}
}

// --- Future Timestamp Tests ---

func TestProcessBlock_FutureTimestamp(t *testing.T) {
	ch, _, _ := testChain(t)

	coinbase := testCoinbaseTx()
	txs := []*tx.Transaction{coinbase}
	hashes := []types.Hash{coinbase.Hash()}
	state := ch.State()
	merkle := block.ComputeMerkleRoot(hashes)

	// 10 minutes in the future — well past the 2-minute threshold.
	futureTime := uint64(time.Now().Add(10 * time.Minute).Unix())
	header := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   state.TipHash,
		MerkleRoot: merkle,
		Timestamp:  futureTime,
		Height:     1,
	}
	blk := block.NewBlock(header, txs)
	poa := ch.engine.(*consensus.PoA)
	poa.Seal(blk)

	err := ch.ProcessBlock(blk)
	if !errors.Is(err, ErrTimestampTooFuture) {
		t.Errorf("expected ErrTimestampTooFuture, got: %v", err)
	}
}

// --- Wrong Stake Amount Tests ---

func TestProcessBlock_WrongStakeAmount(t *testing.T) {
	// Create chain with validatorStake = 900.
	validatorKey, _ := crypto.GenerateKey()
	poa, _ := consensus.NewPoA([][]byte{validatorKey.PublicKey()})
	poa.SetSigner(validatorKey)

	db := storage.NewMemory()
	utxoStore := utxo.NewStore(db)
	ch, _ := New(types.ChainID{}, db, utxoStore, poa)

	addr := crypto.AddressFromPubKey(validatorKey.PublicKey())
	gen := &config.Genesis{
		ChainID:   "test-stake",
		ChainName: "Test",
		Timestamp: 1700000000,
		Alloc:     map[string]uint64{addr.String(): 5000},
		Protocol: config.ProtocolConfig{
			Consensus: config.ConsensusRules{
				Type:           config.ConsensusPoA,
				BlockTime:      3,
				BlockReward:    1000,
				ValidatorStake: 900,
				Validators:     []string{hex.EncodeToString(validatorKey.PublicKey())},
			},
			SubChain: config.SubChainRules{MaxDepth: 1, MaxPerParent: 10, AnchorInterval: 10},
		},
	}
	if err := ch.InitFromGenesis(gen); err != nil {
		t.Fatalf("InitFromGenesis: %v", err)
	}

	// First produce a block with a coinbase for funds, then wait for maturity.
	coinbase1 := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: addr[:]},
		}},
	}
	blk1 := buildCustomBlock(t, ch, []*tx.Transaction{coinbase1})
	if err := ch.ProcessBlock(blk1); err != nil {
		t.Fatalf("ProcessBlock (block 1): %v", err)
	}

	// Produce enough blocks for coinbase maturity.
	for i := 0; i < int(config.CoinbaseMaturity); i++ {
		cb := testCoinbaseTx()
		blk := buildCustomBlock(t, ch, []*tx.Transaction{cb})
		if err := ch.ProcessBlock(blk); err != nil {
			t.Fatalf("maturity block %d: %v", i, err)
		}
	}

	// Build a stake tx with wrong value (500 instead of 900).
	stakeKey, _ := crypto.GenerateKey()
	coinbaseOut := types.Outpoint{TxID: coinbase1.Hash(), Index: 0}
	stakeTxBuilder := tx.NewBuilder().
		AddInput(coinbaseOut).
		AddOutput(500, types.Script{Type: types.ScriptTypeStake, Data: stakeKey.PublicKey()})
	stakeTxBuilder.Sign(validatorKey)
	stakeTx := stakeTxBuilder.Build()

	coinbase := testCoinbaseTx()
	blk := buildCustomBlock(t, ch, []*tx.Transaction{coinbase, stakeTx})

	err := ch.ProcessBlock(blk)
	if !errors.Is(err, ErrInvalidStakeAmount) {
		t.Errorf("expected ErrInvalidStakeAmount, got: %v", err)
	}
}

func TestProcessBlock_CoinbaseWithTokenData_Rejected(t *testing.T) {
	ch, _, _ := testChain(t)

	// Craft a coinbase with token data in the output.
	coinbase := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
			Token:  &types.TokenData{ID: types.TokenID{1, 2, 3}, Amount: 999},
		}},
	}

	blk := buildCustomBlock(t, ch, []*tx.Transaction{coinbase})
	err := ch.ProcessBlock(blk)
	if err == nil {
		t.Fatal("expected error for coinbase with token data")
	}
	if !strings.Contains(err.Error(), "must not contain token data") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProcessBlock_CoinbaseWithMintScript_Rejected(t *testing.T) {
	ch, _, _ := testChain(t)

	// Craft a coinbase with ScriptTypeMint.
	coinbase := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeMint, Data: make([]byte, 20)},
		}},
	}

	blk := buildCustomBlock(t, ch, []*tx.Transaction{coinbase})
	err := ch.ProcessBlock(blk)
	if err == nil {
		t.Fatal("expected error for coinbase with mint script")
	}
	if !strings.Contains(err.Error(), "must not use mint script type") {
		t.Errorf("unexpected error: %v", err)
	}
}

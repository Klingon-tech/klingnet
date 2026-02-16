package block

import (
	"bytes"
	"errors"
	"sort"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// testCoinbase returns a minimal coinbase transaction.
func testCoinbase() *tx.Transaction {
	return &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}}, // Zero outpoint = coinbase.
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
		}},
	}
}

// validBlock creates a minimal valid block with correct merkle root.
func validBlock(t *testing.T) *Block {
	t.Helper()

	coinbase := testCoinbase()
	txHashes := []types.Hash{coinbase.Hash()}
	merkleRoot := ComputeMerkleRoot(txHashes)

	header := &Header{
		Version:    CurrentVersion,
		PrevHash:   types.Hash{0xaa},
		MerkleRoot: merkleRoot,
		Timestamp:  1700000000,
		Height:     1,
	}

	return NewBlock(header, []*tx.Transaction{coinbase})
}

func TestBlock_Validate_Valid(t *testing.T) {
	blk := validBlock(t)
	if err := blk.Validate(); err != nil {
		t.Errorf("valid block should pass: %v", err)
	}
}

func TestBlock_Validate_NilHeader(t *testing.T) {
	blk := &Block{Header: nil}
	err := blk.Validate()
	if !errors.Is(err, ErrNilHeader) {
		t.Errorf("expected ErrNilHeader, got: %v", err)
	}
}

func TestBlock_Validate_BadVersion(t *testing.T) {
	blk := validBlock(t)
	blk.Header.Version = 99
	err := blk.Validate()
	if !errors.Is(err, ErrBadVersion) {
		t.Errorf("expected ErrBadVersion, got: %v", err)
	}
}

func TestBlock_Validate_VersionZero(t *testing.T) {
	blk := validBlock(t)
	blk.Header.Version = 0
	err := blk.Validate()
	if !errors.Is(err, ErrBadVersion) {
		t.Errorf("expected ErrBadVersion for version 0, got: %v", err)
	}
}

func TestBlock_Validate_VersionCurrent(t *testing.T) {
	blk := validBlock(t)
	blk.Header.Version = CurrentVersion
	if err := blk.Validate(); err != nil {
		t.Errorf("version %d should be valid: %v", CurrentVersion, err)
	}
}

func TestBlock_Validate_VersionAboveMax(t *testing.T) {
	blk := validBlock(t)
	blk.Header.Version = MaxVersion + 1
	err := blk.Validate()
	if !errors.Is(err, ErrBadVersion) {
		t.Errorf("expected ErrBadVersion for version %d, got: %v", MaxVersion+1, err)
	}
}

func TestBlock_Validate_ZeroTimestamp(t *testing.T) {
	blk := validBlock(t)
	blk.Header.Timestamp = 0
	err := blk.Validate()
	if !errors.Is(err, ErrZeroTimestamp) {
		t.Errorf("expected ErrZeroTimestamp, got: %v", err)
	}
}

func TestBlock_Validate_NoTransactions(t *testing.T) {
	blk := &Block{
		Header: &Header{
			Version:   CurrentVersion,
			Timestamp: 1700000000,
		},
		Transactions: nil,
	}
	err := blk.Validate()
	if !errors.Is(err, ErrNoTransactions) {
		t.Errorf("expected ErrNoTransactions, got: %v", err)
	}
}

func TestBlock_Validate_BadMerkleRoot(t *testing.T) {
	blk := validBlock(t)
	blk.Header.MerkleRoot = types.Hash{0xde, 0xad} // wrong root
	err := blk.Validate()
	if !errors.Is(err, ErrBadMerkleRoot) {
		t.Errorf("expected ErrBadMerkleRoot, got: %v", err)
	}
}

func TestBlock_Validate_InvalidTransaction(t *testing.T) {
	coinbase := testCoinbase()
	// Build a bad tx (no sig/pubkey on a non-coinbase input).
	badTx := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}}},
		Outputs: []tx.Output{{Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}

	txs := []*tx.Transaction{coinbase, badTx}
	hashes := []types.Hash{txs[0].Hash(), txs[1].Hash()}
	merkle := ComputeMerkleRoot(hashes)

	blk := NewBlock(&Header{
		Version:    CurrentVersion,
		MerkleRoot: merkle,
		Timestamp:  1700000000,
		Height:     1,
	}, txs)

	err := blk.Validate()
	if err == nil {
		t.Error("block with invalid tx should fail validation")
	}
}

func TestBlock_Validate_MultipleTxs(t *testing.T) {
	key, _ := crypto.GenerateKey()

	coinbase := testCoinbase()

	b1 := tx.NewBuilder().
		AddInput(types.Outpoint{TxID: types.Hash{0x01}, Index: 0}).
		AddOutput(1000, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b1.Sign(key)

	b2 := tx.NewBuilder().
		AddInput(types.Outpoint{TxID: types.Hash{0x02}, Index: 0}).
		AddOutput(2000, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b2.Sign(key)

	// Canonical order: coinbase first, then non-coinbase sorted by hash ascending.
	userTxs := []*tx.Transaction{b1.Build(), b2.Build()}
	sortTxsByHash(userTxs)

	txs := make([]*tx.Transaction, 0, 3)
	txs = append(txs, coinbase)
	txs = append(txs, userTxs...)

	hashes := make([]types.Hash, len(txs))
	for i, t := range txs {
		hashes[i] = t.Hash()
	}
	merkle := ComputeMerkleRoot(hashes)

	blk := NewBlock(&Header{
		Version:    CurrentVersion,
		MerkleRoot: merkle,
		Timestamp:  1700000000,
		Height:     5,
	}, txs)

	if err := blk.Validate(); err != nil {
		t.Errorf("multi-tx block should validate: %v", err)
	}
}

func TestBlock_Validate_NoCoinbase(t *testing.T) {
	key, _ := crypto.GenerateKey()
	b := tx.NewBuilder().
		AddInput(types.Outpoint{TxID: types.Hash{0x01}, Index: 0}).
		AddOutput(1000, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b.Sign(key)
	transaction := b.Build()

	merkle := ComputeMerkleRoot([]types.Hash{transaction.Hash()})
	blk := NewBlock(&Header{
		Version:    CurrentVersion,
		MerkleRoot: merkle,
		Timestamp:  1700000000,
		Height:     1,
	}, []*tx.Transaction{transaction})

	err := blk.Validate()
	if !errors.Is(err, ErrNoCoinbase) {
		t.Errorf("expected ErrNoCoinbase, got: %v", err)
	}
}

func TestBlock_Validate_BadTxOrder(t *testing.T) {
	key, _ := crypto.GenerateKey()

	coinbase := testCoinbase()

	b1 := tx.NewBuilder().
		AddInput(types.Outpoint{TxID: types.Hash{0x01}, Index: 0}).
		AddOutput(1000, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b1.Sign(key)

	b2 := tx.NewBuilder().
		AddInput(types.Outpoint{TxID: types.Hash{0x02}, Index: 0}).
		AddOutput(2000, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b2.Sign(key)

	// Ensure WRONG order: sort ascending then reverse.
	userTxs := []*tx.Transaction{b1.Build(), b2.Build()}
	sortTxsByHash(userTxs)
	userTxs[0], userTxs[1] = userTxs[1], userTxs[0] // reverse = wrong order

	txs := make([]*tx.Transaction, 0, 3)
	txs = append(txs, coinbase)
	txs = append(txs, userTxs...)

	hashes := make([]types.Hash, len(txs))
	for i, t := range txs {
		hashes[i] = t.Hash()
	}
	merkle := ComputeMerkleRoot(hashes)

	blk := NewBlock(&Header{
		Version:    CurrentVersion,
		MerkleRoot: merkle,
		Timestamp:  1700000000,
		Height:     5,
	}, txs)

	err := blk.Validate()
	if !errors.Is(err, ErrBadTxOrder) {
		t.Errorf("expected ErrBadTxOrder, got: %v", err)
	}
}

// sortTxsByHash sorts transactions by hash ascending (canonical order).
func sortTxsByHash(txs []*tx.Transaction) {
	sort.Slice(txs, func(i, j int) bool {
		hi, hj := txs[i].Hash(), txs[j].Hash()
		return bytes.Compare(hi[:], hj[:]) < 0
	})
}

func TestHeader_Hash_Deterministic(t *testing.T) {
	h := &Header{
		Version:   1,
		PrevHash:  types.Hash{0x01},
		Timestamp: 1700000000,
		Height:    1,
	}

	h1 := h.Hash()
	h2 := h.Hash()
	if h1 != h2 {
		t.Error("Header.Hash() should be deterministic")
	}
	if h1.IsZero() {
		t.Error("Header.Hash() should not be zero")
	}
}

func TestHeader_Hash_IgnoresValidatorSig(t *testing.T) {
	h := &Header{
		Version:   1,
		PrevHash:  types.Hash{0x01},
		Timestamp: 1700000000,
		Height:    1,
	}
	h1 := h.Hash()

	h.ValidatorSig = []byte("some sig data")
	h2 := h.Hash()

	if h1 != h2 {
		t.Error("Header.Hash() should not change when ValidatorSig is set")
	}
}

func TestBlock_Validate_TooManyTxs(t *testing.T) {
	coinbase := testCoinbase()
	key, _ := crypto.GenerateKey()

	// Build MaxBlockTxs + 1 transactions (1 coinbase + MaxBlockTxs non-coinbase).
	txs := make([]*tx.Transaction, 0, config.MaxBlockTxs+1)
	txs = append(txs, coinbase)

	for i := 0; i < config.MaxBlockTxs; i++ {
		b := tx.NewBuilder().
			AddInput(types.Outpoint{TxID: types.Hash{byte(i >> 16), byte(i >> 8), byte(i)}, Index: uint32(i)}).
			AddOutput(1000, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
		b.Sign(key)
		txs = append(txs, b.Build())
	}

	sortTxsByHash(txs[1:])

	hashes := make([]types.Hash, len(txs))
	for i, t := range txs {
		hashes[i] = t.Hash()
	}
	merkle := ComputeMerkleRoot(hashes)

	blk := NewBlock(&Header{
		Version:    CurrentVersion,
		MerkleRoot: merkle,
		Timestamp:  1700000000,
		Height:     1,
	}, txs)

	err := blk.Validate()
	if !errors.Is(err, ErrTooManyTxs) {
		t.Errorf("expected ErrTooManyTxs, got: %v", err)
	}
}

func TestBlock_Validate_BlockTooLarge(t *testing.T) {
	// Create a block with a single tx that has a huge script data payload
	// to push the block over MaxBlockSize.
	bigData := make([]byte, config.MaxBlockSize)
	coinbase := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: bigData},
		}},
	}

	hashes := []types.Hash{coinbase.Hash()}
	merkle := ComputeMerkleRoot(hashes)

	blk := NewBlock(&Header{
		Version:    CurrentVersion,
		MerkleRoot: merkle,
		Timestamp:  1700000000,
		Height:     1,
	}, []*tx.Transaction{coinbase})

	err := blk.Validate()
	if !errors.Is(err, ErrBlockTooLarge) {
		t.Errorf("expected ErrBlockTooLarge, got: %v", err)
	}
}

func TestBlock_Hash(t *testing.T) {
	blk := validBlock(t)
	h := blk.Hash()
	if h.IsZero() {
		t.Error("Block.Hash() should not be zero")
	}

	// Nil header.
	blk2 := &Block{}
	if !blk2.Hash().IsZero() {
		t.Error("Block.Hash() with nil header should be zero")
	}
}

package miner

import (
	"testing"

	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// --- BuildCoinbase ---

func TestBuildCoinbase(t *testing.T) {
	addr := types.Address{0x01, 0x02, 0x03}
	cb := BuildCoinbase(addr, 50000, 42)

	if cb.Version != 1 {
		t.Errorf("version: got %d, want 1", cb.Version)
	}
	if len(cb.Inputs) != 1 {
		t.Fatalf("inputs: got %d, want 1", len(cb.Inputs))
	}
	if !cb.Inputs[0].PrevOut.IsZero() {
		t.Error("coinbase input should be zero outpoint")
	}
	if len(cb.Inputs[0].Signature) != 8 {
		t.Errorf("coinbase signature should be 8-byte height, got %d", len(cb.Inputs[0].Signature))
	}
	if len(cb.Inputs[0].PubKey) != 0 {
		t.Error("coinbase should have no pubkey")
	}
	if len(cb.Outputs) != 1 {
		t.Fatalf("outputs: got %d, want 1", len(cb.Outputs))
	}
	if cb.Outputs[0].Value != 50000 {
		t.Errorf("output value: got %d, want 50000", cb.Outputs[0].Value)
	}
	if cb.Outputs[0].Script.Type != types.ScriptTypeP2PKH {
		t.Error("output script should be P2PKH")
	}

	// Different heights must produce different tx hashes.
	cb2 := BuildCoinbase(addr, 50000, 43)
	if cb.Hash() == cb2.Hash() {
		t.Error("coinbase txs at different heights must have different hashes")
	}
}

func TestBuildCoinbase_Validate(t *testing.T) {
	addr := types.Address{0xaa}
	cb := BuildCoinbase(addr, 1000, 1)

	// Coinbase should pass structural validation (after our fix).
	if err := cb.Validate(); err != nil {
		t.Errorf("coinbase should pass Validate: %v", err)
	}
}

// --- mockChainState ---

type mockChainState struct {
	height  uint64
	tipHash types.Hash
}

func (m *mockChainState) Height() uint64      { return m.height }
func (m *mockChainState) TipHash() types.Hash { return m.tipHash }

// --- mockMempool ---

type mockMempool struct {
	txs  []*tx.Transaction
	fees map[types.Hash]uint64
}

func newMockMempool(txs []*tx.Transaction, fees map[types.Hash]uint64) *mockMempool {
	return &mockMempool{txs: txs, fees: fees}
}

func (m *mockMempool) SelectForBlock(limit int) []*tx.Transaction {
	if limit >= len(m.txs) {
		return m.txs
	}
	return m.txs[:limit]
}

func (m *mockMempool) GetFee(txHash types.Hash) uint64 {
	if m.fees == nil {
		return 0
	}
	return m.fees[txHash]
}

// --- Miner ---

func testMiner(t *testing.T) (*Miner, *crypto.PrivateKey) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	poa, err := consensus.NewPoA([][]byte{key.PublicKey()})
	if err != nil {
		t.Fatalf("create poa: %v", err)
	}
	if err := poa.SetSigner(key); err != nil {
		t.Fatalf("set signer: %v", err)
	}

	addr := crypto.AddressFromPubKey(key.PublicKey())
	chain := &mockChainState{
		height:  0,
		tipHash: types.Hash{0xaa, 0xbb},
	}

	m := New(chain, poa, nil, addr, 50000, 0, nil)
	return m, key
}

func TestMiner_ProduceBlock(t *testing.T) {
	m, _ := testMiner(t)

	blk, err := m.ProduceBlock()
	if err != nil {
		t.Fatalf("ProduceBlock: %v", err)
	}

	if blk.Header.Height != 1 {
		t.Errorf("height: got %d, want 1", blk.Header.Height)
	}
	if blk.Header.PrevHash != (types.Hash{0xaa, 0xbb}) {
		t.Error("PrevHash should match chain tip")
	}
	if blk.Header.Version != 1 {
		t.Errorf("version: got %d, want 1", blk.Header.Version)
	}
	if blk.Header.Timestamp == 0 {
		t.Error("timestamp should not be zero")
	}
	if len(blk.Header.ValidatorSig) == 0 {
		t.Error("block should be sealed with validator signature")
	}
	if len(blk.Transactions) != 1 {
		t.Fatalf("expected 1 tx (coinbase), got %d", len(blk.Transactions))
	}
	if blk.Transactions[0].Outputs[0].Value != 50000 {
		t.Error("coinbase output value mismatch")
	}
}

func TestMiner_ProduceBlock_ValidStructure(t *testing.T) {
	m, _ := testMiner(t)

	blk, err := m.ProduceBlock()
	if err != nil {
		t.Fatalf("ProduceBlock: %v", err)
	}

	// Block should pass structural validation.
	if err := blk.Validate(); err != nil {
		t.Errorf("block should pass Validate: %v", err)
	}
}

func TestMiner_ProduceBlock_ValidConsensus(t *testing.T) {
	key, _ := crypto.GenerateKey()
	poa, _ := consensus.NewPoA([][]byte{key.PublicKey()})
	poa.SetSigner(key)

	addr := crypto.AddressFromPubKey(key.PublicKey())
	chain := &mockChainState{height: 5, tipHash: types.Hash{0x11}}
	m := New(chain, poa, nil, addr, 1000, 0, nil)

	blk, err := m.ProduceBlock()
	if err != nil {
		t.Fatalf("ProduceBlock: %v", err)
	}

	// Should pass consensus verification.
	if err := poa.VerifyHeader(blk.Header); err != nil {
		t.Errorf("block should pass consensus: %v", err)
	}
	if blk.Header.Height != 6 {
		t.Errorf("height: got %d, want 6", blk.Header.Height)
	}
}

func TestMiner_ProduceBlock_WithMempool(t *testing.T) {
	key, _ := crypto.GenerateKey()
	poa, _ := consensus.NewPoA([][]byte{key.PublicKey()})
	poa.SetSigner(key)

	addr := crypto.AddressFromPubKey(key.PublicKey())
	chain := &mockChainState{height: 0, tipHash: types.Hash{0x01}}

	// Mock mempool with a transaction.
	mempoolTx := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{TxID: types.Hash{0xff}, Index: 0}, Signature: []byte("s"), PubKey: []byte("k")}},
		Outputs: []tx.Output{{Value: 500, Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}}},
	}
	txFee := uint64(100)
	fees := map[types.Hash]uint64{mempoolTx.Hash(): txFee}
	pool := newMockMempool([]*tx.Transaction{mempoolTx}, fees)

	m := New(chain, poa, pool, addr, 50000, 0, nil)

	blk, err := m.ProduceBlock()
	if err != nil {
		t.Fatalf("ProduceBlock: %v", err)
	}

	// Should have coinbase + 1 mempool tx.
	if len(blk.Transactions) != 2 {
		t.Errorf("expected 2 txs, got %d", len(blk.Transactions))
	}

	// Coinbase should include block reward + tx fees.
	expectedValue := uint64(50000) + txFee
	if blk.Transactions[0].Outputs[0].Value != expectedValue {
		t.Errorf("coinbase value: got %d, want %d (reward + fees)", blk.Transactions[0].Outputs[0].Value, expectedValue)
	}
}

// --- Supply Cap ---

func TestMiner_ProduceBlock_SupplyCapReduced(t *testing.T) {
	key, _ := crypto.GenerateKey()
	poa, _ := consensus.NewPoA([][]byte{key.PublicKey()})
	poa.SetSigner(key)

	addr := crypto.AddressFromPubKey(key.PublicKey())
	chain := &mockChainState{height: 0, tipHash: types.Hash{0x01}}

	// Max supply 100, current supply 80, block reward 50.
	// Should cap reward to 20.
	supply := uint64(80)
	m := New(chain, poa, nil, addr, 50, 100, func() uint64 { return supply })

	blk, err := m.ProduceBlock()
	if err != nil {
		t.Fatalf("ProduceBlock: %v", err)
	}

	coinbaseValue := blk.Transactions[0].Outputs[0].Value
	if coinbaseValue != 20 {
		t.Errorf("coinbase value: got %d, want 20 (capped by supply)", coinbaseValue)
	}
}

func TestMiner_ProduceBlock_SupplyCapZeroReward(t *testing.T) {
	key, _ := crypto.GenerateKey()
	poa, _ := consensus.NewPoA([][]byte{key.PublicKey()})
	poa.SetSigner(key)

	addr := crypto.AddressFromPubKey(key.PublicKey())
	chain := &mockChainState{height: 0, tipHash: types.Hash{0x01}}

	// Supply already at max → reward should be 0.
	m := New(chain, poa, nil, addr, 50000, 100000, func() uint64 { return 100000 })

	blk, err := m.ProduceBlock()
	if err != nil {
		t.Fatalf("ProduceBlock: %v", err)
	}

	coinbaseValue := blk.Transactions[0].Outputs[0].Value
	if coinbaseValue != 0 {
		t.Errorf("coinbase value: got %d, want 0 (supply at max)", coinbaseValue)
	}
}

func TestMiner_ProduceBlock_SupplyCapWithFees(t *testing.T) {
	key, _ := crypto.GenerateKey()
	poa, _ := consensus.NewPoA([][]byte{key.PublicKey()})
	poa.SetSigner(key)

	addr := crypto.AddressFromPubKey(key.PublicKey())
	chain := &mockChainState{height: 0, tipHash: types.Hash{0x01}}

	// Supply at max but there are fees → coinbase = 0 reward + fees.
	mempoolTx := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{TxID: types.Hash{0xff}, Index: 0}, Signature: []byte("s"), PubKey: []byte("k")}},
		Outputs: []tx.Output{{Value: 500, Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}}},
	}
	fees := map[types.Hash]uint64{mempoolTx.Hash(): 100}
	pool := newMockMempool([]*tx.Transaction{mempoolTx}, fees)

	m := New(chain, poa, pool, addr, 50000, 1000, func() uint64 { return 1000 })

	blk, err := m.ProduceBlock()
	if err != nil {
		t.Fatalf("ProduceBlock: %v", err)
	}

	// Coinbase = 0 reward + 100 fees.
	coinbaseValue := blk.Transactions[0].Outputs[0].Value
	if coinbaseValue != 100 {
		t.Errorf("coinbase value: got %d, want 100 (fees only)", coinbaseValue)
	}
}

func TestMiner_ProduceBlock_UnlimitedSupply(t *testing.T) {
	key, _ := crypto.GenerateKey()
	poa, _ := consensus.NewPoA([][]byte{key.PublicKey()})
	poa.SetSigner(key)

	addr := crypto.AddressFromPubKey(key.PublicKey())
	chain := &mockChainState{height: 0, tipHash: types.Hash{0x01}}

	// maxSupply=0 means unlimited.
	m := New(chain, poa, nil, addr, 50000, 0, nil)

	blk, err := m.ProduceBlock()
	if err != nil {
		t.Fatalf("ProduceBlock: %v", err)
	}

	if blk.Transactions[0].Outputs[0].Value != 50000 {
		t.Errorf("coinbase: got %d, want 50000 (unlimited)", blk.Transactions[0].Outputs[0].Value)
	}
}

// --- UTXOAdapter ---

func TestUTXOAdapter_GetUTXO(t *testing.T) {
	db := storage.NewMemory()
	store := utxo.NewStore(db)

	op := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	u := &utxo.UTXO{
		Outpoint: op,
		Value:    1000,
		Script:   types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
	}
	store.Put(u)

	adapter := NewUTXOAdapter(store)

	val, script, err := adapter.GetUTXO(op)
	if err != nil {
		t.Fatalf("GetUTXO: %v", err)
	}
	if val != 1000 {
		t.Errorf("value: got %d, want 1000", val)
	}
	if script.Type != types.ScriptTypeP2PKH {
		t.Error("script type mismatch")
	}
}

func TestUTXOAdapter_HasUTXO(t *testing.T) {
	db := storage.NewMemory()
	store := utxo.NewStore(db)

	op := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	store.Put(&utxo.UTXO{Outpoint: op, Value: 1})

	adapter := NewUTXOAdapter(store)

	if !adapter.HasUTXO(op) {
		t.Error("HasUTXO should return true for existing outpoint")
	}

	missing := types.Outpoint{TxID: types.Hash{0xff}, Index: 0}
	if adapter.HasUTXO(missing) {
		t.Error("HasUTXO should return false for missing outpoint")
	}
}

func TestUTXOAdapter_GetUTXO_NotFound(t *testing.T) {
	db := storage.NewMemory()
	store := utxo.NewStore(db)
	adapter := NewUTXOAdapter(store)

	_, _, err := adapter.GetUTXO(types.Outpoint{TxID: types.Hash{0xff}})
	if err == nil {
		t.Error("GetUTXO should fail for missing outpoint")
	}
}

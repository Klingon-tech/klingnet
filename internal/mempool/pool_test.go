package mempool

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// mockUTXOs is a simple in-memory UTXO provider for tests.
type mockUTXOs struct {
	utxos map[types.Outpoint]mockUTXO
}

type mockUTXO struct {
	value  uint64
	script types.Script
}

func newMockUTXOs() *mockUTXOs {
	return &mockUTXOs{utxos: make(map[types.Outpoint]mockUTXO)}
}

func (m *mockUTXOs) add(op types.Outpoint, value uint64, addr types.Address) {
	m.utxos[op] = mockUTXO{
		value: value,
		script: types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: addr[:],
		},
	}
}

func (m *mockUTXOs) GetUTXO(op types.Outpoint) (uint64, types.Script, error) {
	u, ok := m.utxos[op]
	if !ok {
		return 0, types.Script{}, fmt.Errorf("not found")
	}
	return u.value, u.script, nil
}

func (m *mockUTXOs) HasUTXO(op types.Outpoint) bool {
	_, ok := m.utxos[op]
	return ok
}

func addressFromKey(key *crypto.PrivateKey) types.Address {
	h := crypto.Hash(key.PublicKey())
	var addr types.Address
	copy(addr[:], h[:types.AddressSize])
	return addr
}

// buildTx creates a signed transaction spending the given outpoint.
func buildTx(t *testing.T, key *crypto.PrivateKey, prevOut types.Outpoint, outputValue uint64) *tx.Transaction {
	t.Helper()
	b := tx.NewBuilder().
		AddInput(prevOut).
		AddOutput(outputValue, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b.Sign(key)
	return b.Build()
}

func TestPool_Add(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	utxos.add(prevOut, 5000, addr)

	pool := New(utxos, 100)
	transaction := buildTx(t, key, prevOut, 4000)

	fee, err := pool.Add(transaction)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if fee != 1000 {
		t.Errorf("fee = %d, want 1000", fee)
	}
	if pool.Count() != 1 {
		t.Errorf("count = %d, want 1", pool.Count())
	}
}

func TestPool_Add_Duplicate(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	utxos.add(prevOut, 5000, addr)

	pool := New(utxos, 100)
	transaction := buildTx(t, key, prevOut, 4000)

	pool.Add(transaction)
	_, err := pool.Add(transaction)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("expected ErrAlreadyExists, got: %v", err)
	}
}

func TestPool_Add_DoubleSpend(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	utxos.add(prevOut, 5000, addr)

	pool := New(utxos, 100)

	tx1 := buildTx(t, key, prevOut, 4000) // Spends prevOut.
	tx2 := buildTx(t, key, prevOut, 3000) // Also spends prevOut — conflict!

	pool.Add(tx1)
	_, err := pool.Add(tx2)
	if !errors.Is(err, ErrConflict) {
		t.Errorf("expected ErrConflict, got: %v", err)
	}
}

func TestPool_Add_PoolFull(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	// Create 3 UTXOs.
	for i := 0; i < 3; i++ {
		utxos.add(types.Outpoint{TxID: types.Hash{byte(i + 1)}, Index: 0}, 5000, addr)
	}

	pool := New(utxos, 2) // Max 2 transactions.

	pool.Add(buildTx(t, key, types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, 4000))
	pool.Add(buildTx(t, key, types.Outpoint{TxID: types.Hash{0x02}, Index: 0}, 4000))

	_, err := pool.Add(buildTx(t, key, types.Outpoint{TxID: types.Hash{0x03}, Index: 0}, 4000))
	if !errors.Is(err, ErrPoolFull) {
		t.Errorf("expected ErrPoolFull, got: %v", err)
	}
}

func TestPool_Add_ValidationFailure(t *testing.T) {
	utxos := newMockUTXOs() // Empty — no UTXOs.
	pool := New(utxos, 100)

	key, _ := crypto.GenerateKey()
	transaction := buildTx(t, key, types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, 1000)

	_, err := pool.Add(transaction)
	if !errors.Is(err, ErrValidation) {
		t.Errorf("expected ErrValidation, got: %v", err)
	}
}

func TestPool_Remove(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	utxos.add(prevOut, 5000, addr)

	pool := New(utxos, 100)
	transaction := buildTx(t, key, prevOut, 4000)
	pool.Add(transaction)

	pool.Remove(transaction.Hash())
	if pool.Count() != 0 {
		t.Errorf("count = %d, want 0", pool.Count())
	}
	if pool.Has(transaction.Hash()) {
		t.Error("Has should return false after Remove")
	}
}

func TestPool_Remove_ClearsConflictIndex(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	utxos.add(prevOut, 5000, addr)

	pool := New(utxos, 100)

	tx1 := buildTx(t, key, prevOut, 4000)
	pool.Add(tx1)
	pool.Remove(tx1.Hash())

	// Should now be able to add a different tx spending the same outpoint.
	tx2 := buildTx(t, key, prevOut, 3000)
	_, err := pool.Add(tx2)
	if err != nil {
		t.Fatalf("Add after Remove should succeed: %v", err)
	}
}

func TestPool_RemoveConfirmed(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	utxos.add(types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, 5000, addr)
	utxos.add(types.Outpoint{TxID: types.Hash{0x02}, Index: 0}, 3000, addr)

	pool := New(utxos, 100)

	tx1 := buildTx(t, key, types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, 4000)
	tx2 := buildTx(t, key, types.Outpoint{TxID: types.Hash{0x02}, Index: 0}, 2000)
	pool.Add(tx1)
	pool.Add(tx2)

	pool.RemoveConfirmed([]*tx.Transaction{tx1})
	if pool.Count() != 1 {
		t.Errorf("count = %d, want 1", pool.Count())
	}
	if pool.Has(tx1.Hash()) {
		t.Error("tx1 should be removed")
	}
	if !pool.Has(tx2.Hash()) {
		t.Error("tx2 should still be in pool")
	}
}

func TestPool_Has(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	utxos.add(prevOut, 5000, addr)

	pool := New(utxos, 100)
	transaction := buildTx(t, key, prevOut, 4000)

	if pool.Has(transaction.Hash()) {
		t.Error("Has should return false before Add")
	}
	pool.Add(transaction)
	if !pool.Has(transaction.Hash()) {
		t.Error("Has should return true after Add")
	}
}

func TestPool_Get(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	utxos.add(prevOut, 5000, addr)

	pool := New(utxos, 100)
	transaction := buildTx(t, key, prevOut, 4000)
	pool.Add(transaction)

	got := pool.Get(transaction.Hash())
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Hash() != transaction.Hash() {
		t.Error("Get returned wrong transaction")
	}

	missing := pool.Get(types.Hash{0xff})
	if missing != nil {
		t.Error("Get should return nil for unknown hash")
	}
}

func TestPool_SelectForBlock(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	// Create UTXOs with different values (will result in different fees).
	utxos.add(types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, 5000, addr)
	utxos.add(types.Outpoint{TxID: types.Hash{0x02}, Index: 0}, 3000, addr)
	utxos.add(types.Outpoint{TxID: types.Hash{0x03}, Index: 0}, 8000, addr)

	pool := New(utxos, 100)

	// Fee = 5000 - 4000 = 1000
	tx1 := buildTx(t, key, types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, 4000)
	// Fee = 3000 - 2500 = 500
	tx2 := buildTx(t, key, types.Outpoint{TxID: types.Hash{0x02}, Index: 0}, 2500)
	// Fee = 8000 - 5000 = 3000
	tx3 := buildTx(t, key, types.Outpoint{TxID: types.Hash{0x03}, Index: 0}, 5000)

	pool.Add(tx1)
	pool.Add(tx2)
	pool.Add(tx3)

	// Select top 2 by fee rate.
	selected := pool.SelectForBlock(2)
	if len(selected) != 2 {
		t.Fatalf("selected %d, want 2", len(selected))
	}

	// tx3 (3000 fee) should be first, tx1 (1000 fee) second.
	if selected[0].Hash() != tx3.Hash() {
		t.Error("highest fee-rate tx should be first")
	}
	if selected[1].Hash() != tx1.Hash() {
		t.Error("second highest fee-rate tx should be second")
	}
}

func TestPool_SelectForBlock_LimitExceedsPool(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	utxos.add(types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, 5000, addr)

	pool := New(utxos, 100)
	pool.Add(buildTx(t, key, types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, 4000))

	selected := pool.SelectForBlock(100)
	if len(selected) != 1 {
		t.Errorf("selected %d, want 1", len(selected))
	}
}

func TestPool_Evict(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	for i := 0; i < 5; i++ {
		utxos.add(types.Outpoint{TxID: types.Hash{byte(i + 1)}, Index: 0}, uint64(5000+i*1000), addr)
	}

	pool := New(utxos, 5) // Max 5.

	for i := 0; i < 5; i++ {
		pool.Add(buildTx(t, key, types.Outpoint{TxID: types.Hash{byte(i + 1)}, Index: 0}, 4000))
	}

	if pool.Count() != 5 {
		t.Fatalf("count = %d, want 5", pool.Count())
	}

	// Shrink max and evict.
	pool.maxSize = 3
	evicted := pool.Evict()
	if evicted != 2 {
		t.Errorf("evicted = %d, want 2", evicted)
	}
	if pool.Count() != 3 {
		t.Errorf("count after evict = %d, want 3", pool.Count())
	}
}

func TestPool_Evict_NotNeeded(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	utxos.add(types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, 5000, addr)

	pool := New(utxos, 100)
	pool.Add(buildTx(t, key, types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, 4000))

	evicted := pool.Evict()
	if evicted != 0 {
		t.Errorf("evicted = %d, want 0", evicted)
	}
}

func TestPolicy_Check(t *testing.T) {
	key, _ := crypto.GenerateKey()

	b := tx.NewBuilder().
		AddInput(types.Outpoint{TxID: types.Hash{0x01}, Index: 0}).
		AddOutput(1000, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b.Sign(key)
	transaction := b.Build()

	policy := DefaultPolicy()
	if err := policy.Check(transaction); err != nil {
		t.Errorf("valid tx should pass policy: %v", err)
	}

	// Tiny max size to trigger rejection.
	policy.MaxTxSize = 1
	if err := policy.Check(transaction); err == nil {
		t.Error("oversized tx should fail policy")
	}
}

func TestNew_DefaultMaxSize(t *testing.T) {
	utxos := newMockUTXOs()
	pool := New(utxos, 0) // Should default to 5000.
	if pool.maxSize != 5000 {
		t.Errorf("maxSize = %d, want 5000", pool.maxSize)
	}
}

func TestPool_MinFeeRate_Reject(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	utxos.add(prevOut, 5000, addr)

	pool := New(utxos, 100)
	pool.SetMinFeeRate(12) // 12 base units per byte; ~89 bytes → requires ~1068 fee.

	// Tx with fee = 1000 (5000 - 4000) should be rejected (1000 < 12*89).
	transaction := buildTx(t, key, prevOut, 4000)
	_, err := pool.Add(transaction)
	if !errors.Is(err, ErrFeeTooLow) {
		t.Errorf("expected ErrFeeTooLow, got: %v", err)
	}
}

func TestPool_MinFeeRate_Accept(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	utxos.add(prevOut, 5000, addr)

	pool := New(utxos, 100)
	pool.SetMinFeeRate(10) // 10 base units per byte; ~89 bytes → requires ~890 fee.

	// Tx with fee = 1000 (5000 - 4000) should pass (1000 >= 10*89).
	transaction := buildTx(t, key, prevOut, 4000)
	fee, err := pool.Add(transaction)
	if err != nil {
		t.Fatalf("Add should pass: %v", err)
	}
	if fee != 1000 {
		t.Errorf("fee = %d, want 1000", fee)
	}
}

func TestPool_GetFee(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	utxos.add(prevOut, 5000, addr)

	pool := New(utxos, 100)
	transaction := buildTx(t, key, prevOut, 4000)
	pool.Add(transaction)

	txHash := transaction.Hash()
	if got := pool.GetFee(txHash); got != 1000 {
		t.Errorf("GetFee = %d, want 1000", got)
	}

	// Unknown tx returns 0.
	if got := pool.GetFee(types.Hash{0xff}); got != 0 {
		t.Errorf("GetFee for unknown = %d, want 0", got)
	}
}

func TestPolicy_Check_TooManyInputs(t *testing.T) {
	inputs := make([]tx.Input, config.MaxTxInputs+1)
	for i := range inputs {
		inputs[i] = tx.Input{
			PrevOut:   types.Outpoint{TxID: types.Hash{byte(i >> 8), byte(i)}, Index: uint32(i)},
			Signature: []byte("s"),
			PubKey:    []byte("k"),
		}
	}
	transaction := &tx.Transaction{
		Inputs:  inputs,
		Outputs: []tx.Output{{Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}
	policy := DefaultPolicy()
	err := policy.Check(transaction)
	if err == nil || !strings.Contains(err.Error(), "too many inputs") {
		t.Errorf("expected too many inputs error, got: %v", err)
	}
}

func TestPolicy_Check_TooManyOutputs(t *testing.T) {
	outputs := make([]tx.Output, config.MaxTxOutputs+1)
	for i := range outputs {
		outputs[i] = tx.Output{Value: 1, Script: types.Script{Type: types.ScriptTypeP2PKH}}
	}
	transaction := &tx.Transaction{
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}, Signature: []byte("s"), PubKey: []byte("k")}},
		Outputs: outputs,
	}
	policy := DefaultPolicy()
	err := policy.Check(transaction)
	if err == nil || !strings.Contains(err.Error(), "too many outputs") {
		t.Errorf("expected too many outputs error, got: %v", err)
	}
}

func TestPool_EvictLowestFeeRate(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	utxos := newMockUTXOs()
	// Create 3 UTXOs with increasing values (higher value = higher fee for same output).
	utxos.add(types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, 2000, addr) // fee = 2000 - 1000 = 1000 (low)
	utxos.add(types.Outpoint{TxID: types.Hash{0x02}, Index: 0}, 4000, addr) // fee = 4000 - 1000 = 3000 (medium)
	utxos.add(types.Outpoint{TxID: types.Hash{0x03}, Index: 0}, 8000, addr) // fee = 8000 - 1000 = 7000 (high)

	pool := New(utxos, 2) // Max 2 transactions.

	// tx1: low fee rate (fee = 1000).
	tx1 := buildTx(t, key, types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, 1000)
	// tx2: medium fee rate (fee = 3000).
	tx2 := buildTx(t, key, types.Outpoint{TxID: types.Hash{0x02}, Index: 0}, 1000)

	if _, err := pool.Add(tx1); err != nil {
		t.Fatalf("Add tx1: %v", err)
	}
	if _, err := pool.Add(tx2); err != nil {
		t.Fatalf("Add tx2: %v", err)
	}

	if pool.Count() != 2 {
		t.Fatalf("pool count = %d, want 2", pool.Count())
	}

	// tx3: high fee rate (fee = 7000) should evict tx1 (lowest fee rate).
	tx3 := buildTx(t, key, types.Outpoint{TxID: types.Hash{0x03}, Index: 0}, 1000)
	if _, err := pool.Add(tx3); err != nil {
		t.Fatalf("Add tx3: %v", err)
	}

	// tx1 should be evicted, tx2 and tx3 should remain.
	if pool.Has(tx1.Hash()) {
		t.Error("tx1 should have been evicted (lowest fee rate)")
	}
	if !pool.Has(tx2.Hash()) {
		t.Error("tx2 should still be present")
	}
	if !pool.Has(tx3.Hash()) {
		t.Error("tx3 should be present")
	}
	if pool.Count() != 2 {
		t.Errorf("pool count = %d, want 2", pool.Count())
	}
}

func TestPolicy_Check_ScriptDataTooLarge(t *testing.T) {
	transaction := &tx.Transaction{
		Inputs: []tx.Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}, Signature: []byte("s"), PubKey: []byte("k")}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, config.MaxScriptData+1)},
		}},
	}
	policy := DefaultPolicy()
	err := policy.Check(transaction)
	if err == nil || !strings.Contains(err.Error(), "script data too large") {
		t.Errorf("expected script data too large error, got: %v", err)
	}
}

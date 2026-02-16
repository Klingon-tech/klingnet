package tx

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// mockUTXOProvider is a simple in-memory UTXO provider for testing.
type mockUTXOProvider struct {
	utxos map[types.Outpoint]mockUTXO
}

type mockUTXO struct {
	value  uint64
	script types.Script
}

func newMockProvider() *mockUTXOProvider {
	return &mockUTXOProvider{utxos: make(map[types.Outpoint]mockUTXO)}
}

func (m *mockUTXOProvider) add(op types.Outpoint, value uint64, script types.Script) {
	m.utxos[op] = mockUTXO{value: value, script: script}
}

func (m *mockUTXOProvider) GetUTXO(op types.Outpoint) (uint64, types.Script, error) {
	u, ok := m.utxos[op]
	if !ok {
		return 0, types.Script{}, fmt.Errorf("not found")
	}
	return u.value, u.script, nil
}

func (m *mockUTXOProvider) HasUTXO(op types.Outpoint) bool {
	_, ok := m.utxos[op]
	return ok
}

// addressFromKey derives a P2PKH address from a crypto.PrivateKey.
func addressFromKey(key *crypto.PrivateKey) types.Address {
	h := crypto.Hash(key.PublicKey())
	var addr types.Address
	copy(addr[:], h[:types.AddressSize])
	return addr
}

func TestValidateWithUTXOs_Valid(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	provider := newMockProvider()
	provider.add(prevOut, 5000, types.Script{
		Type: types.ScriptTypeP2PKH,
		Data: addr[:],
	})

	b := NewBuilder().
		AddInput(prevOut).
		AddOutput(4000, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b.Sign(key)
	transaction := b.Build()

	fee, err := transaction.ValidateWithUTXOs(provider)
	if err != nil {
		t.Fatalf("ValidateWithUTXOs: %v", err)
	}
	if fee != 1000 {
		t.Errorf("fee = %d, want 1000", fee)
	}
}

func TestValidateWithUTXOs_ZeroFee(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	provider := newMockProvider()
	provider.add(prevOut, 3000, types.Script{
		Type: types.ScriptTypeP2PKH,
		Data: addr[:],
	})

	b := NewBuilder().
		AddInput(prevOut).
		AddOutput(3000, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b.Sign(key)
	transaction := b.Build()

	fee, err := transaction.ValidateWithUTXOs(provider)
	if err != nil {
		t.Fatalf("ValidateWithUTXOs: %v", err)
	}
	if fee != 0 {
		t.Errorf("fee = %d, want 0", fee)
	}
}

func TestValidateWithUTXOs_InputNotFound(t *testing.T) {
	key, _ := crypto.GenerateKey()

	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	provider := newMockProvider() // Empty — no UTXOs.

	b := NewBuilder().
		AddInput(prevOut).
		AddOutput(1000, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b.Sign(key)
	transaction := b.Build()

	_, err := transaction.ValidateWithUTXOs(provider)
	if !errors.Is(err, ErrInputNotFound) {
		t.Errorf("expected ErrInputNotFound, got: %v", err)
	}
}

func TestValidateWithUTXOs_InsufficientFunds(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	provider := newMockProvider()
	provider.add(prevOut, 1000, types.Script{
		Type: types.ScriptTypeP2PKH,
		Data: addr[:],
	})

	b := NewBuilder().
		AddInput(prevOut).
		AddOutput(2000, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b.Sign(key)
	transaction := b.Build()

	_, err := transaction.ValidateWithUTXOs(provider)
	if !errors.Is(err, ErrInsufficientFee) {
		t.Errorf("expected ErrInsufficientFee, got: %v", err)
	}
}

func TestValidateWithUTXOs_ScriptMismatch(t *testing.T) {
	key, _ := crypto.GenerateKey()
	// Use a different address than what the key derives.
	wrongAddr := make([]byte, types.AddressSize)
	wrongAddr[0] = 0xff

	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	provider := newMockProvider()
	provider.add(prevOut, 5000, types.Script{
		Type: types.ScriptTypeP2PKH,
		Data: wrongAddr,
	})

	b := NewBuilder().
		AddInput(prevOut).
		AddOutput(4000, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b.Sign(key)
	transaction := b.Build()

	_, err := transaction.ValidateWithUTXOs(provider)
	if !errors.Is(err, ErrScriptMismatch) {
		t.Errorf("expected ErrScriptMismatch, got: %v", err)
	}
}

func TestValidateWithUTXOs_MultipleInputs(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	prevOut1 := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	prevOut2 := types.Outpoint{TxID: types.Hash{0x02}, Index: 0}
	provider := newMockProvider()
	provider.add(prevOut1, 3000, types.Script{Type: types.ScriptTypeP2PKH, Data: addr[:]})
	provider.add(prevOut2, 2000, types.Script{Type: types.ScriptTypeP2PKH, Data: addr[:]})

	b := NewBuilder().
		AddInput(prevOut1).
		AddInput(prevOut2).
		AddOutput(4500, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b.Sign(key)
	transaction := b.Build()

	fee, err := transaction.ValidateWithUTXOs(provider)
	if err != nil {
		t.Fatalf("ValidateWithUTXOs: %v", err)
	}
	if fee != 500 {
		t.Errorf("fee = %d, want 500", fee)
	}
}

func TestValidateWithUTXOs_InvalidSignature(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()
	addr2 := addressFromKey(key2)

	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	provider := newMockProvider()
	// UTXO is locked to key2's address...
	provider.add(prevOut, 5000, types.Script{Type: types.ScriptTypeP2PKH, Data: addr2[:]})

	// ...but signed with key1. The P2PKH check will catch the mismatch.
	b := NewBuilder().
		AddInput(prevOut).
		AddOutput(4000, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b.Sign(key1)
	transaction := b.Build()

	_, err := transaction.ValidateWithUTXOs(provider)
	if !errors.Is(err, ErrScriptMismatch) {
		t.Errorf("expected ErrScriptMismatch, got: %v", err)
	}
}

func TestValidateWithUTXOs_StructuralFailure(t *testing.T) {
	// Transaction with no inputs should fail structural validation.
	transaction := &Transaction{
		Version: 1,
		Outputs: []Output{{Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}
	provider := newMockProvider()

	_, err := transaction.ValidateWithUTXOs(provider)
	if !errors.Is(err, ErrNoInputs) {
		t.Errorf("expected ErrNoInputs, got: %v", err)
	}
}

func TestVerifyP2PKH(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := addressFromKey(key)

	// Valid: pubkey matches address.
	err := verifyP2PKH(key.PublicKey(), addr[:])
	if err != nil {
		t.Errorf("valid P2PKH should pass: %v", err)
	}

	// Mismatch: wrong pubkey.
	key2, _ := crypto.GenerateKey()
	err = verifyP2PKH(key2.PublicKey(), addr[:])
	if !errors.Is(err, ErrScriptMismatch) {
		t.Errorf("expected ErrScriptMismatch for wrong pubkey, got: %v", err)
	}

	// Empty pubkey.
	err = verifyP2PKH(nil, addr[:])
	if !errors.Is(err, ErrMissingPubKey) {
		t.Errorf("expected ErrMissingPubKey, got: %v", err)
	}

	// Wrong script data length.
	err = verifyP2PKH(key.PublicKey(), []byte{0x01, 0x02})
	if !errors.Is(err, ErrScriptMismatch) {
		t.Errorf("expected ErrScriptMismatch for wrong length, got: %v", err)
	}
}

func TestValidateWithUTXOs_StakeSpend(t *testing.T) {
	key, _ := crypto.GenerateKey()
	pubKey := key.PublicKey()

	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	provider := newMockProvider()
	provider.add(prevOut, 5000, types.Script{
		Type: types.ScriptTypeStake,
		Data: pubKey,
	})

	recvAddr := make([]byte, types.AddressSize)
	recvAddr[0] = 0x42

	b := NewBuilder().
		AddInput(prevOut).
		AddOutput(4000, types.Script{Type: types.ScriptTypeP2PKH, Data: recvAddr})
	b.Sign(key)
	transaction := b.Build()

	fee, err := transaction.ValidateWithUTXOs(provider)
	if err != nil {
		t.Fatalf("ValidateWithUTXOs: %v", err)
	}
	if fee != 1000 {
		t.Errorf("fee = %d, want 1000", fee)
	}
}

func TestValidateWithUTXOs_StakeSpend_WrongKey(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()
	pubKey1 := key1.PublicKey()

	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	provider := newMockProvider()
	provider.add(prevOut, 5000, types.Script{
		Type: types.ScriptTypeStake,
		Data: pubKey1,
	})

	recvAddr := make([]byte, types.AddressSize)
	recvAddr[0] = 0x42

	b := NewBuilder().
		AddInput(prevOut).
		AddOutput(4000, types.Script{Type: types.ScriptTypeP2PKH, Data: recvAddr})
	b.Sign(key2) // Sign with different key
	transaction := b.Build()

	_, err := transaction.ValidateWithUTXOs(provider)
	if !errors.Is(err, ErrScriptMismatch) {
		t.Errorf("expected ErrScriptMismatch, got: %v", err)
	}
}

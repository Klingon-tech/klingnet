package tx

import (
	"math"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func testP2PKHScript(addr types.Address) types.Script {
	return types.Script{Type: types.ScriptTypeP2PKH, Data: addr[:]}
}

func TestTransaction_Hash_Deterministic(t *testing.T) {
	tx := &Transaction{
		Version: 1,
		Inputs:  []Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}, Index: 0}}},
		Outputs: []Output{{Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}

	h1 := tx.Hash()
	h2 := tx.Hash()
	if h1 != h2 {
		t.Error("Hash() should be deterministic")
	}
	if h1.IsZero() {
		t.Error("Hash() should not be zero")
	}
}

func TestTransaction_Hash_ChangesWithContent(t *testing.T) {
	tx1 := &Transaction{
		Version: 1,
		Inputs:  []Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}, Index: 0}}},
		Outputs: []Output{{Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}
	tx2 := &Transaction{
		Version: 1,
		Inputs:  []Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}, Index: 0}}},
		Outputs: []Output{{Value: 2000, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}

	if tx1.Hash() == tx2.Hash() {
		t.Error("different transactions should have different hashes")
	}
}

func TestTransaction_Hash_IgnoresSignature(t *testing.T) {
	tx := &Transaction{
		Version: 1,
		Inputs:  []Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}, Index: 0}}},
		Outputs: []Output{{Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}

	h1 := tx.Hash()

	tx.Inputs[0].Signature = []byte("some signature")
	tx.Inputs[0].PubKey = []byte("some key")

	h2 := tx.Hash()

	if h1 != h2 {
		t.Error("Hash() should not change when signatures are added")
	}
}

func TestTransaction_TotalOutputValue(t *testing.T) {
	tx := &Transaction{
		Outputs: []Output{
			{Value: 1000},
			{Value: 2000},
			{Value: 3000},
		},
	}
	got, err := tx.TotalOutputValue()
	if err != nil {
		t.Fatalf("TotalOutputValue() error: %v", err)
	}
	if got != 6000 {
		t.Errorf("TotalOutputValue() = %d, want 6000", got)
	}
}

func TestTransaction_TotalOutputValue_Empty(t *testing.T) {
	tx := &Transaction{}
	got, err := tx.TotalOutputValue()
	if err != nil {
		t.Fatalf("TotalOutputValue() error: %v", err)
	}
	if got != 0 {
		t.Errorf("TotalOutputValue() empty = %d, want 0", got)
	}
}

func TestTransaction_TotalOutputValue_Overflow(t *testing.T) {
	tx := &Transaction{
		Outputs: []Output{
			{Value: math.MaxUint64},
			{Value: 1},
		},
	}
	_, err := tx.TotalOutputValue()
	if err == nil {
		t.Error("TotalOutputValue() should return error on overflow")
	}
}

func TestBuilder_BuildAndSign(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := types.Address{0x01, 0x02, 0x03}

	prevOut := types.Outpoint{TxID: crypto.Hash([]byte("prev tx")), Index: 0}

	b := NewBuilder().
		AddInput(prevOut).
		AddOutput(5000, testP2PKHScript(addr))

	err := b.Sign(key)
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	transaction := b.Build()

	if len(transaction.Inputs) != 1 {
		t.Fatalf("expected 1 input, got %d", len(transaction.Inputs))
	}
	if len(transaction.Outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(transaction.Outputs))
	}
	if transaction.Version != 1 {
		t.Errorf("version = %d, want 1", transaction.Version)
	}

	// Should validate.
	if err := transaction.Validate(); err != nil {
		t.Errorf("Validate() error: %v", err)
	}

	// Signatures should verify.
	if err := transaction.VerifySignatures(); err != nil {
		t.Errorf("VerifySignatures() error: %v", err)
	}
}

func TestBuilder_MultipleInputsOutputs(t *testing.T) {
	key, _ := crypto.GenerateKey()

	b := NewBuilder().
		AddInput(types.Outpoint{TxID: types.Hash{0x01}, Index: 0}).
		AddInput(types.Outpoint{TxID: types.Hash{0x02}, Index: 1}).
		AddOutput(3000, types.Script{Type: types.ScriptTypeP2PKH}).
		AddOutput(2000, types.Script{Type: types.ScriptTypeP2PKH}).
		SetLockTime(100)

	b.Sign(key)
	transaction := b.Build()

	if len(transaction.Inputs) != 2 {
		t.Errorf("input count = %d, want 2", len(transaction.Inputs))
	}
	if len(transaction.Outputs) != 2 {
		t.Errorf("output count = %d, want 2", len(transaction.Outputs))
	}
	if transaction.LockTime != 100 {
		t.Errorf("locktime = %d, want 100", transaction.LockTime)
	}
	if err := transaction.Validate(); err != nil {
		t.Errorf("Validate() error: %v", err)
	}
	if err := transaction.VerifySignatures(); err != nil {
		t.Errorf("VerifySignatures() error: %v", err)
	}
}

func TestBuilder_SignMulti(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	addr1 := crypto.AddressFromPubKey(key1.PublicKey())
	addr2 := crypto.AddressFromPubKey(key2.PublicKey())

	out1 := types.Outpoint{TxID: crypto.Hash([]byte("tx1")), Index: 0}
	out2 := types.Outpoint{TxID: crypto.Hash([]byte("tx2")), Index: 1}

	b := NewBuilder().
		AddInput(out1).
		AddInput(out2).
		AddOutput(3000, testP2PKHScript(types.Address{0x99}))

	signers := map[types.Address]*crypto.PrivateKey{
		addr1: key1,
		addr2: key2,
	}
	outpointAddr := map[types.Outpoint]types.Address{
		out1: addr1,
		out2: addr2,
	}

	if err := b.SignMulti(signers, outpointAddr); err != nil {
		t.Fatalf("SignMulti() error: %v", err)
	}

	transaction := b.Build()

	// Each input should have a valid signature.
	if err := transaction.Validate(); err != nil {
		t.Errorf("Validate() error: %v", err)
	}
	if err := transaction.VerifySignatures(); err != nil {
		t.Errorf("VerifySignatures() error: %v", err)
	}

	// Inputs should have different pubkeys.
	if string(transaction.Inputs[0].PubKey) == string(transaction.Inputs[1].PubKey) {
		t.Error("inputs should have different pubkeys")
	}
}

func TestBuilder_SignMulti_SameKeyTwoInputs(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := crypto.AddressFromPubKey(key.PublicKey())

	out1 := types.Outpoint{TxID: crypto.Hash([]byte("tx1")), Index: 0}
	out2 := types.Outpoint{TxID: crypto.Hash([]byte("tx2")), Index: 0}

	b := NewBuilder().
		AddInput(out1).
		AddInput(out2).
		AddOutput(5000, testP2PKHScript(types.Address{0x99}))

	signers := map[types.Address]*crypto.PrivateKey{addr: key}
	outpointAddr := map[types.Outpoint]types.Address{
		out1: addr,
		out2: addr,
	}

	if err := b.SignMulti(signers, outpointAddr); err != nil {
		t.Fatalf("SignMulti() error: %v", err)
	}

	transaction := b.Build()
	if err := transaction.VerifySignatures(); err != nil {
		t.Errorf("VerifySignatures() error: %v", err)
	}

	// Same key → same signature (cached).
	if string(transaction.Inputs[0].Signature) != string(transaction.Inputs[1].Signature) {
		t.Error("same key should produce same signature (cache)")
	}
}

func TestBuilder_SignMulti_MissingAddress(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := crypto.AddressFromPubKey(key.PublicKey())

	out1 := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}

	b := NewBuilder().
		AddInput(out1).
		AddOutput(1000, testP2PKHScript(types.Address{}))

	// Missing outpointAddr mapping.
	signers := map[types.Address]*crypto.PrivateKey{addr: key}
	outpointAddr := map[types.Outpoint]types.Address{}

	err := b.SignMulti(signers, outpointAddr)
	if err == nil {
		t.Fatal("expected error for missing address mapping")
	}
}

func TestBuilder_SignMulti_MissingSigner(t *testing.T) {
	out1 := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	addr := types.Address{0xAA}

	b := NewBuilder().
		AddInput(out1).
		AddOutput(1000, testP2PKHScript(types.Address{}))

	// Have address mapping but no signer.
	signers := map[types.Address]*crypto.PrivateKey{}
	outpointAddr := map[types.Outpoint]types.Address{out1: addr}

	err := b.SignMulti(signers, outpointAddr)
	if err == nil {
		t.Fatal("expected error for missing signer")
	}
}

func TestBuilder_TokenOutput(t *testing.T) {
	key, _ := crypto.GenerateKey()
	token := types.TokenData{ID: types.TokenID{0xaa}, Amount: 100}

	b := NewBuilder().
		AddInput(types.Outpoint{TxID: types.Hash{0x01}, Index: 0}).
		AddTokenOutput(0, types.Script{Type: types.ScriptTypeMint}, token)

	b.Sign(key)
	transaction := b.Build()

	if transaction.Outputs[0].Token == nil {
		t.Fatal("token output should have token data")
	}
	if transaction.Outputs[0].Token.Amount != 100 {
		t.Errorf("token amount = %d, want 100", transaction.Outputs[0].Token.Amount)
	}
}

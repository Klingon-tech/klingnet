package tx

import (
	"errors"
	"math"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// validTx creates a minimal valid signed transaction for testing.
func validTx(t *testing.T) *Transaction {
	t.Helper()
	key, _ := crypto.GenerateKey()
	b := NewBuilder().
		AddInput(types.Outpoint{TxID: types.Hash{0x01}, Index: 0}).
		AddOutput(1000, types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)})
	b.Sign(key)
	return b.Build()
}

func TestValidate_Valid(t *testing.T) {
	tx := validTx(t)
	if err := tx.Validate(); err != nil {
		t.Errorf("valid tx should pass: %v", err)
	}
}

func TestValidate_NoInputs(t *testing.T) {
	tx := &Transaction{
		Outputs: []Output{{Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}
	err := tx.Validate()
	if !errors.Is(err, ErrNoInputs) {
		t.Errorf("expected ErrNoInputs, got: %v", err)
	}
}

func TestValidate_NoOutputs(t *testing.T) {
	tx := &Transaction{
		Inputs: []Input{{
			PrevOut:   types.Outpoint{TxID: types.Hash{0x01}},
			Signature: []byte("sig"),
			PubKey:    []byte("key"),
		}},
	}
	err := tx.Validate()
	if !errors.Is(err, ErrNoOutputs) {
		t.Errorf("expected ErrNoOutputs, got: %v", err)
	}
}

func TestValidate_DuplicateInput(t *testing.T) {
	same := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	tx := &Transaction{
		Inputs: []Input{
			{PrevOut: same, Signature: []byte("s"), PubKey: []byte("k")},
			{PrevOut: same, Signature: []byte("s"), PubKey: []byte("k")},
		},
		Outputs: []Output{{Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}
	err := tx.Validate()
	if !errors.Is(err, ErrDuplicateInput) {
		t.Errorf("expected ErrDuplicateInput, got: %v", err)
	}
}

func TestValidate_MissingPubKey(t *testing.T) {
	tx := &Transaction{
		Inputs:  []Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}, Signature: []byte("s")}},
		Outputs: []Output{{Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}
	err := tx.Validate()
	if !errors.Is(err, ErrMissingPubKey) {
		t.Errorf("expected ErrMissingPubKey, got: %v", err)
	}
}

func TestValidate_MissingSig(t *testing.T) {
	tx := &Transaction{
		Inputs:  []Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}, PubKey: []byte("k")}},
		Outputs: []Output{{Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}
	err := tx.Validate()
	if !errors.Is(err, ErrMissingSig) {
		t.Errorf("expected ErrMissingSig, got: %v", err)
	}
}

func TestValidate_ZeroValueNoToken(t *testing.T) {
	tx := &Transaction{
		Inputs:  []Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}, Signature: []byte("s"), PubKey: []byte("k")}},
		Outputs: []Output{{Value: 0, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}
	err := tx.Validate()
	if !errors.Is(err, ErrNegativeOutput) {
		t.Errorf("expected ErrNegativeOutput for zero-value no-token output, got: %v", err)
	}
}

func TestValidate_ZeroValueWithToken(t *testing.T) {
	// Zero native value is OK if carrying a token.
	tx := &Transaction{
		Inputs: []Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}, Signature: []byte("s"), PubKey: []byte("k")}},
		Outputs: []Output{{
			Value:  0,
			Script: types.Script{Type: types.ScriptTypeMint},
			Token:  &types.TokenData{ID: types.TokenID{0xaa}, Amount: 100},
		}},
	}
	if err := tx.Validate(); err != nil {
		t.Errorf("zero value with token should be valid: %v", err)
	}
}

func TestValidate_OutputOverflow(t *testing.T) {
	tx := &Transaction{
		Inputs: []Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}, Signature: []byte("s"), PubKey: []byte("k")}},
		Outputs: []Output{
			{Value: math.MaxUint64, Script: types.Script{Type: types.ScriptTypeP2PKH}},
			{Value: 1, Script: types.Script{Type: types.ScriptTypeP2PKH}},
		},
	}
	err := tx.Validate()
	if !errors.Is(err, ErrOutputOverflow) {
		t.Errorf("expected ErrOutputOverflow, got: %v", err)
	}
}

func TestValidate_Coinbase(t *testing.T) {
	// Coinbase tx: zero outpoint input, no sig/pubkey — should pass.
	coinbase := &Transaction{
		Version: 1,
		Inputs:  []Input{{PrevOut: types.Outpoint{}}}, // Zero outpoint.
		Outputs: []Output{{Value: 50000, Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}}},
	}
	if err := coinbase.Validate(); err != nil {
		t.Errorf("coinbase tx should pass Validate: %v", err)
	}
}

func TestVerifySignatures_Coinbase(t *testing.T) {
	// Coinbase tx should pass VerifySignatures (zero outpoint skipped).
	coinbase := &Transaction{
		Version: 1,
		Inputs:  []Input{{PrevOut: types.Outpoint{}}},
		Outputs: []Output{{Value: 50000, Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}}},
	}
	if err := coinbase.VerifySignatures(); err != nil {
		t.Errorf("coinbase tx should pass VerifySignatures: %v", err)
	}
}

func TestVerifySignatures_Valid(t *testing.T) {
	tx := validTx(t)
	if err := tx.VerifySignatures(); err != nil {
		t.Errorf("valid signatures should verify: %v", err)
	}
}

func TestVerifySignatures_WrongKey(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	b := NewBuilder().
		AddInput(types.Outpoint{TxID: types.Hash{0x01}, Index: 0}).
		AddOutput(1000, types.Script{Type: types.ScriptTypeP2PKH})
	b.Sign(key1)
	transaction := b.Build()

	// Replace pubkey with wrong one.
	transaction.Inputs[0].PubKey = key2.PublicKey()

	err := transaction.VerifySignatures()
	if !errors.Is(err, ErrInvalidSig) {
		t.Errorf("expected ErrInvalidSig, got: %v", err)
	}
}

func TestVerifySignatures_TamperedOutput(t *testing.T) {
	tx := validTx(t)

	// Tamper with output value after signing.
	tx.Outputs[0].Value = 9999

	err := tx.VerifySignatures()
	if !errors.Is(err, ErrInvalidSig) {
		t.Errorf("tampered tx should fail verification: %v", err)
	}
}

func TestVerifySignatures_CorruptedSig(t *testing.T) {
	tx := validTx(t)

	// Corrupt signature.
	tx.Inputs[0].Signature[0] ^= 0xFF

	err := tx.VerifySignatures()
	if !errors.Is(err, ErrInvalidSig) {
		t.Errorf("corrupted sig should fail: %v", err)
	}
}

func TestValidate_TooManyInputs(t *testing.T) {
	inputs := make([]Input, config.MaxTxInputs+1)
	for i := range inputs {
		inputs[i] = Input{
			PrevOut:   types.Outpoint{TxID: types.Hash{byte(i >> 8), byte(i)}, Index: uint32(i)},
			Signature: []byte("s"),
			PubKey:    []byte("k"),
		}
	}
	transaction := &Transaction{
		Inputs:  inputs,
		Outputs: []Output{{Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}
	err := transaction.Validate()
	if !errors.Is(err, ErrTooManyInputs) {
		t.Errorf("expected ErrTooManyInputs, got: %v", err)
	}
}

func TestValidate_TooManyInputs_AtLimit(t *testing.T) {
	inputs := make([]Input, config.MaxTxInputs)
	for i := range inputs {
		inputs[i] = Input{
			PrevOut:   types.Outpoint{TxID: types.Hash{byte(i >> 8), byte(i)}, Index: uint32(i)},
			Signature: []byte("s"),
			PubKey:    []byte("k"),
		}
	}
	transaction := &Transaction{
		Inputs:  inputs,
		Outputs: []Output{{Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}
	err := transaction.Validate()
	if errors.Is(err, ErrTooManyInputs) {
		t.Errorf("exactly MaxTxInputs should not trigger ErrTooManyInputs")
	}
}

func TestValidate_TooManyOutputs(t *testing.T) {
	outputs := make([]Output, config.MaxTxOutputs+1)
	for i := range outputs {
		outputs[i] = Output{Value: 1, Script: types.Script{Type: types.ScriptTypeP2PKH}}
	}
	transaction := &Transaction{
		Inputs:  []Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}, Signature: []byte("s"), PubKey: []byte("k")}},
		Outputs: outputs,
	}
	err := transaction.Validate()
	if !errors.Is(err, ErrTooManyOutputs) {
		t.Errorf("expected ErrTooManyOutputs, got: %v", err)
	}
}

func TestValidate_TooManyOutputs_AtLimit(t *testing.T) {
	outputs := make([]Output, config.MaxTxOutputs)
	for i := range outputs {
		outputs[i] = Output{Value: 1, Script: types.Script{Type: types.ScriptTypeP2PKH}}
	}
	transaction := &Transaction{
		Inputs:  []Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}, Signature: []byte("s"), PubKey: []byte("k")}},
		Outputs: outputs,
	}
	err := transaction.Validate()
	if errors.Is(err, ErrTooManyOutputs) {
		t.Errorf("exactly MaxTxOutputs should not trigger ErrTooManyOutputs")
	}
}

func TestValidate_ScriptDataTooLarge(t *testing.T) {
	transaction := &Transaction{
		Inputs: []Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}, Signature: []byte("s"), PubKey: []byte("k")}},
		Outputs: []Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, config.MaxScriptData+1)},
		}},
	}
	err := transaction.Validate()
	if !errors.Is(err, ErrScriptDataTooLarge) {
		t.Errorf("expected ErrScriptDataTooLarge, got: %v", err)
	}
}

func TestValidate_ScriptDataAtLimit(t *testing.T) {
	transaction := &Transaction{
		Inputs: []Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}, Signature: []byte("s"), PubKey: []byte("k")}},
		Outputs: []Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, config.MaxScriptData)},
		}},
	}
	err := transaction.Validate()
	if errors.Is(err, ErrScriptDataTooLarge) {
		t.Errorf("exactly MaxScriptData should not trigger ErrScriptDataTooLarge")
	}
}

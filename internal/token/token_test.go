package token

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// --- DeriveTokenID Tests ---

func TestDeriveTokenID(t *testing.T) {
	txID := types.Hash{0x01}
	id := DeriveTokenID(txID, 0)
	if id.IsZero() {
		t.Error("token ID should not be zero")
	}
}

func TestDeriveTokenID_Deterministic(t *testing.T) {
	txID := types.Hash{0xaa, 0xbb}
	id1 := DeriveTokenID(txID, 3)
	id2 := DeriveTokenID(txID, 3)
	if id1 != id2 {
		t.Error("same inputs should produce same token ID")
	}
}

func TestDeriveTokenID_DifferentIndex(t *testing.T) {
	txID := types.Hash{0x01}
	id0 := DeriveTokenID(txID, 0)
	id1 := DeriveTokenID(txID, 1)
	if id0 == id1 {
		t.Error("different output indices should produce different token IDs")
	}
}

func TestDeriveTokenID_DifferentTxID(t *testing.T) {
	id1 := DeriveTokenID(types.Hash{0x01}, 0)
	id2 := DeriveTokenID(types.Hash{0x02}, 0)
	if id1 == id2 {
		t.Error("different tx IDs should produce different token IDs")
	}
}

// --- mockInputTokens ---

type mockInputTokens struct {
	data map[types.Outpoint]*types.TokenData
}

func newMockInputTokens() *mockInputTokens {
	return &mockInputTokens{data: make(map[types.Outpoint]*types.TokenData)}
}

func (m *mockInputTokens) add(op types.Outpoint, td *types.TokenData) {
	m.data[op] = td
}

func (m *mockInputTokens) GetTokenData(op types.Outpoint) *types.TokenData {
	return m.data[op]
}

// --- ValidateTokens Tests ---

func TestValidateTokens_NoTokens(t *testing.T) {
	// A plain native-asset transaction with no tokens should pass.
	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}}},
		Outputs: []tx.Output{{Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}
	inputs := newMockInputTokens()

	if err := ValidateTokens(transaction, inputs); err != nil {
		t.Errorf("no-token tx should pass: %v", err)
	}
}

func TestValidateTokens_Mint(t *testing.T) {
	// TokenID is derived from the first input outpoint.
	firstInput := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	tokenID := DeriveTokenID(firstInput.TxID, firstInput.Index)

	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: firstInput}},
		Outputs: []tx.Output{{
			Value:  0,
			Script: types.Script{Type: types.ScriptTypeMint},
			Token:  &types.TokenData{ID: tokenID, Amount: 1000},
		}},
	}

	inputs := newMockInputTokens()
	err := ValidateTokens(transaction, inputs)
	if err != nil {
		t.Fatalf("valid mint should pass: %v", err)
	}
}

func TestValidateTokens_Mint_WrongTokenID(t *testing.T) {
	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}}},
		Outputs: []tx.Output{{
			Value:  0,
			Script: types.Script{Type: types.ScriptTypeMint},
			Token:  &types.TokenData{ID: types.TokenID{0xff}, Amount: 1000},
		}},
	}
	inputs := newMockInputTokens()

	err := ValidateTokens(transaction, inputs)
	if !errors.Is(err, ErrTokenIDMismatch) {
		t.Errorf("expected ErrTokenIDMismatch, got: %v", err)
	}
}

func TestValidateTokens_Mint_ZeroAmount(t *testing.T) {
	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}}},
		Outputs: []tx.Output{{
			Value:  0,
			Script: types.Script{Type: types.ScriptTypeMint},
			Token:  &types.TokenData{ID: types.TokenID{0x01}, Amount: 0},
		}},
	}
	inputs := newMockInputTokens()

	err := ValidateTokens(transaction, inputs)
	if !errors.Is(err, ErrMintZeroAmount) {
		t.Errorf("expected ErrMintZeroAmount, got: %v", err)
	}
}

func TestValidateTokens_Transfer(t *testing.T) {
	tokenID := types.TokenID{0xaa}
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}

	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: prevOut}},
		Outputs: []tx.Output{{
			Value:  0,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
			Token:  &types.TokenData{ID: tokenID, Amount: 500},
		}},
	}

	inputs := newMockInputTokens()
	inputs.add(prevOut, &types.TokenData{ID: tokenID, Amount: 500})

	if err := ValidateTokens(transaction, inputs); err != nil {
		t.Errorf("valid transfer should pass: %v", err)
	}
}

func TestValidateTokens_Transfer_Conservation(t *testing.T) {
	tokenID := types.TokenID{0xaa}
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}

	// Input has 500 tokens, output has 600 — violation.
	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: prevOut}},
		Outputs: []tx.Output{{
			Value:  0,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
			Token:  &types.TokenData{ID: tokenID, Amount: 600},
		}},
	}

	inputs := newMockInputTokens()
	inputs.add(prevOut, &types.TokenData{ID: tokenID, Amount: 500})

	err := ValidateTokens(transaction, inputs)
	if !errors.Is(err, ErrTokenConservation) {
		t.Errorf("expected ErrTokenConservation, got: %v", err)
	}
}

func TestValidateTokens_Transfer_Split(t *testing.T) {
	tokenID := types.TokenID{0xaa}
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}

	// Split 1000 tokens into 600 + 400.
	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: prevOut}},
		Outputs: []tx.Output{
			{Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}, Token: &types.TokenData{ID: tokenID, Amount: 600}},
			{Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}, Token: &types.TokenData{ID: tokenID, Amount: 400}},
		},
	}

	inputs := newMockInputTokens()
	inputs.add(prevOut, &types.TokenData{ID: tokenID, Amount: 1000})

	if err := ValidateTokens(transaction, inputs); err != nil {
		t.Errorf("valid split should pass: %v", err)
	}
}

func TestValidateTokens_Transfer_Merge(t *testing.T) {
	tokenID := types.TokenID{0xaa}
	prevOut1 := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	prevOut2 := types.Outpoint{TxID: types.Hash{0x02}, Index: 0}

	// Merge 300 + 700 into 1000.
	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: prevOut1}, {PrevOut: prevOut2}},
		Outputs: []tx.Output{{
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
			Token:  &types.TokenData{ID: tokenID, Amount: 1000},
		}},
	}

	inputs := newMockInputTokens()
	inputs.add(prevOut1, &types.TokenData{ID: tokenID, Amount: 300})
	inputs.add(prevOut2, &types.TokenData{ID: tokenID, Amount: 700})

	if err := ValidateTokens(transaction, inputs); err != nil {
		t.Errorf("valid merge should pass: %v", err)
	}
}

func TestValidateTokens_Burn(t *testing.T) {
	tokenID := types.TokenID{0xaa}
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}

	// Burn 300 out of 1000, transfer remaining 700.
	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: prevOut}},
		Outputs: []tx.Output{
			{Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}, Token: &types.TokenData{ID: tokenID, Amount: 700}},
			{Script: types.Script{Type: types.ScriptTypeBurn}, Token: &types.TokenData{ID: tokenID, Amount: 300}},
		},
	}

	inputs := newMockInputTokens()
	inputs.add(prevOut, &types.TokenData{ID: tokenID, Amount: 1000})

	if err := ValidateTokens(transaction, inputs); err != nil {
		t.Errorf("valid burn should pass: %v", err)
	}
}

func TestValidateTokens_Burn_All(t *testing.T) {
	tokenID := types.TokenID{0xaa}
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}

	// Burn all 500 tokens.
	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: prevOut}},
		Outputs: []tx.Output{
			{Script: types.Script{Type: types.ScriptTypeBurn}, Token: &types.TokenData{ID: tokenID, Amount: 500}},
		},
	}

	inputs := newMockInputTokens()
	inputs.add(prevOut, &types.TokenData{ID: tokenID, Amount: 500})

	if err := ValidateTokens(transaction, inputs); err != nil {
		t.Errorf("burn-all should pass: %v", err)
	}
}

func TestValidateTokens_Burn_ZeroAmount(t *testing.T) {
	tokenID := types.TokenID{0xaa}
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}

	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: prevOut}},
		Outputs: []tx.Output{
			{Script: types.Script{Type: types.ScriptTypeBurn}, Token: &types.TokenData{ID: tokenID, Amount: 0}},
		},
	}

	inputs := newMockInputTokens()
	inputs.add(prevOut, &types.TokenData{ID: tokenID, Amount: 500})

	err := ValidateTokens(transaction, inputs)
	if !errors.Is(err, ErrBurnZeroAmount) {
		t.Errorf("expected ErrBurnZeroAmount, got: %v", err)
	}
}

func TestValidateTokens_MultipleTokenTypes(t *testing.T) {
	tokenA := types.TokenID{0xaa}
	tokenB := types.TokenID{0xbb}
	prevOut1 := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	prevOut2 := types.Outpoint{TxID: types.Hash{0x02}, Index: 0}

	// Transfer both token types.
	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: prevOut1}, {PrevOut: prevOut2}},
		Outputs: []tx.Output{
			{Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}, Token: &types.TokenData{ID: tokenA, Amount: 100}},
			{Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}, Token: &types.TokenData{ID: tokenB, Amount: 200}},
		},
	}

	inputs := newMockInputTokens()
	inputs.add(prevOut1, &types.TokenData{ID: tokenA, Amount: 100})
	inputs.add(prevOut2, &types.TokenData{ID: tokenB, Amount: 200})

	if err := ValidateTokens(transaction, inputs); err != nil {
		t.Errorf("multi-token transfer should pass: %v", err)
	}
}

// --- HasMintOutput Tests ---

func TestHasMintOutput_True(t *testing.T) {
	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}}},
		Outputs: []tx.Output{{
			Script: types.Script{Type: types.ScriptTypeMint},
			Token:  &types.TokenData{ID: types.TokenID{0x01}, Amount: 100},
		}},
	}
	if !HasMintOutput(transaction) {
		t.Error("expected HasMintOutput to return true")
	}
}

func TestHasMintOutput_False(t *testing.T) {
	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
		}},
	}
	if HasMintOutput(transaction) {
		t.Error("expected HasMintOutput to return false for P2PKH-only tx")
	}
}

// --- ValidateMintFee Tests ---

func TestValidateMintFee_NoMint(t *testing.T) {
	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{TxID: types.Hash{0x01}}}},
		Outputs: []tx.Output{{Value: 100, Script: types.Script{Type: types.ScriptTypeP2PKH}}},
	}
	// Should pass even with zero fee, since there's no mint output.
	if err := ValidateMintFee(transaction, 0, 10_000_000_000_000); err != nil {
		t.Errorf("non-mint tx should not require mint fee: %v", err)
	}
}

func TestValidateMintFee_SufficientFee(t *testing.T) {
	firstInput := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	tokenID := DeriveTokenID(firstInput.TxID, firstInput.Index)

	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: firstInput}},
		Outputs: []tx.Output{{
			Script: types.Script{Type: types.ScriptTypeMint},
			Token:  &types.TokenData{ID: tokenID, Amount: 1000},
		}},
	}
	// Fee matches the required mint fee exactly.
	if err := ValidateMintFee(transaction, 10_000_000_000_000, 10_000_000_000_000); err != nil {
		t.Errorf("mint with exact creation fee should pass: %v", err)
	}
}

func TestValidateMintFee_InsufficientFee(t *testing.T) {
	firstInput := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	tokenID := DeriveTokenID(firstInput.TxID, firstInput.Index)

	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: firstInput}},
		Outputs: []tx.Output{{
			Script: types.Script{Type: types.ScriptTypeMint},
			Token:  &types.TokenData{ID: tokenID, Amount: 1000},
		}},
	}
	// Fee of 1 KGX is below the required creation fee.
	err := ValidateMintFee(transaction, 1_000_000_000_000, 10_000_000_000_000)
	if !errors.Is(err, ErrMintFeeTooLow) {
		t.Errorf("expected ErrMintFeeTooLow, got: %v", err)
	}
}

// --- Store Tests ---

func TestStore_PutAndGet(t *testing.T) {
	db := storage.NewMemory()
	store := NewStore(db)

	id := types.TokenID{0x01}
	meta := &Metadata{Name: "TestToken", Symbol: "TT", Decimals: 8}

	if err := store.Put(id, meta); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "TestToken" || got.Symbol != "TT" || got.Decimals != 8 {
		t.Errorf("metadata mismatch: got %+v", got)
	}
}

func TestStore_GetNotFound(t *testing.T) {
	db := storage.NewMemory()
	store := NewStore(db)

	_, err := store.Get(types.TokenID{0xff})
	if err == nil {
		t.Error("Get should fail for unknown token")
	}
}

func TestValidateTokens_OverflowProtection(t *testing.T) {
	tokenID := types.TokenID{0xaa}
	prevOut1 := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	prevOut2 := types.Outpoint{TxID: types.Hash{0x02}, Index: 0}

	// Two inputs each with MaxUint64/2 + 1, which would overflow if summed.
	halfMax := uint64(math.MaxUint64/2) + 1

	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: prevOut1}, {PrevOut: prevOut2}},
		Outputs: []tx.Output{{
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
			Token:  &types.TokenData{ID: tokenID, Amount: 100},
		}},
	}

	inputs := newMockInputTokens()
	inputs.add(prevOut1, &types.TokenData{ID: tokenID, Amount: halfMax})
	inputs.add(prevOut2, &types.TokenData{ID: tokenID, Amount: halfMax})

	err := ValidateTokens(transaction, inputs)
	if err == nil {
		t.Fatal("expected overflow error, got nil")
	}
	if !strings.Contains(err.Error(), "overflow") {
		t.Errorf("expected error containing 'overflow', got: %v", err)
	}
}

func TestStore_Has(t *testing.T) {
	db := storage.NewMemory()
	store := NewStore(db)

	id := types.TokenID{0x01}
	store.Put(id, &Metadata{Name: "X", Symbol: "X"})

	has, _ := store.Has(id)
	if !has {
		t.Error("Has should return true")
	}

	has, _ = store.Has(types.TokenID{0xff})
	if has {
		t.Error("Has should return false for unknown")
	}
}

// --- MaxTokenAmount Tests ---

func TestValidateTokens_Mint_AmountTooLarge(t *testing.T) {
	firstInput := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	tokenID := DeriveTokenID(firstInput.TxID, firstInput.Index)

	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: firstInput}},
		Outputs: []tx.Output{{
			Script: types.Script{Type: types.ScriptTypeMint},
			Token:  &types.TokenData{ID: tokenID, Amount: config.MaxTokenAmount + 1},
		}},
	}
	inputs := newMockInputTokens()

	err := ValidateTokens(transaction, inputs)
	if !errors.Is(err, ErrTokenAmountTooLarge) {
		t.Errorf("expected ErrTokenAmountTooLarge, got: %v", err)
	}
}

func TestValidateTokens_Mint_ExactMaxAmount(t *testing.T) {
	firstInput := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	tokenID := DeriveTokenID(firstInput.TxID, firstInput.Index)

	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: firstInput}},
		Outputs: []tx.Output{{
			Script: types.Script{Type: types.ScriptTypeMint},
			Token:  &types.TokenData{ID: tokenID, Amount: config.MaxTokenAmount},
		}},
	}
	inputs := newMockInputTokens()

	if err := ValidateTokens(transaction, inputs); err != nil {
		t.Errorf("exact MaxTokenAmount should pass: %v", err)
	}
}

func TestValidateTokens_Transfer_AmountTooLarge(t *testing.T) {
	tokenID := types.TokenID{0xaa}
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}

	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: prevOut}},
		Outputs: []tx.Output{{
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
			Token:  &types.TokenData{ID: tokenID, Amount: config.MaxTokenAmount + 1},
		}},
	}
	inputs := newMockInputTokens()
	inputs.add(prevOut, &types.TokenData{ID: tokenID, Amount: config.MaxTokenAmount + 1})

	err := ValidateTokens(transaction, inputs)
	if !errors.Is(err, ErrTokenAmountTooLarge) {
		t.Errorf("expected ErrTokenAmountTooLarge, got: %v", err)
	}
}

func TestValidateTokens_Burn_AmountTooLarge(t *testing.T) {
	tokenID := types.TokenID{0xaa}
	prevOut := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}

	transaction := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: prevOut}},
		Outputs: []tx.Output{{
			Script: types.Script{Type: types.ScriptTypeBurn},
			Token:  &types.TokenData{ID: tokenID, Amount: config.MaxTokenAmount + 1},
		}},
	}
	inputs := newMockInputTokens()
	inputs.add(prevOut, &types.TokenData{ID: tokenID, Amount: config.MaxTokenAmount + 1})

	err := ValidateTokens(transaction, inputs)
	if !errors.Is(err, ErrTokenAmountTooLarge) {
		t.Errorf("expected ErrTokenAmountTooLarge, got: %v", err)
	}
}

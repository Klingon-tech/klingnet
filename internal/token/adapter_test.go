package token

import (
	"fmt"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

type mockUTXOSet struct {
	utxos map[types.Outpoint]*utxo.UTXO
}

func (m *mockUTXOSet) Get(op types.Outpoint) (*utxo.UTXO, error) {
	u, ok := m.utxos[op]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return u, nil
}

func (m *mockUTXOSet) Put(u *utxo.UTXO) error         { m.utxos[u.Outpoint] = u; return nil }
func (m *mockUTXOSet) Delete(op types.Outpoint) error { delete(m.utxos, op); return nil }
func (m *mockUTXOSet) Has(op types.Outpoint) (bool, error) {
	_, ok := m.utxos[op]
	return ok, nil
}

func TestUTXOTokenAdapter_GetTokenData(t *testing.T) {
	tokenID := types.TokenID{0x01, 0x02}
	op := types.Outpoint{TxID: types.Hash{0xAA}, Index: 0}

	set := &mockUTXOSet{utxos: map[types.Outpoint]*utxo.UTXO{
		op: {
			Outpoint: op,
			Value:    100,
			Token:    &types.TokenData{ID: tokenID, Amount: 500},
		},
	}}

	adapter := &UTXOTokenAdapter{Set: set}

	// Should return token data for known outpoint.
	td := adapter.GetTokenData(op)
	if td == nil {
		t.Fatal("expected token data, got nil")
	}
	if td.ID != tokenID {
		t.Errorf("token ID = %x, want %x", td.ID, tokenID)
	}
	if td.Amount != 500 {
		t.Errorf("amount = %d, want 500", td.Amount)
	}
}

func TestUTXOTokenAdapter_GetTokenData_NoToken(t *testing.T) {
	op := types.Outpoint{TxID: types.Hash{0xBB}, Index: 0}

	set := &mockUTXOSet{utxos: map[types.Outpoint]*utxo.UTXO{
		op: {Outpoint: op, Value: 100, Token: nil},
	}}

	adapter := &UTXOTokenAdapter{Set: set}
	td := adapter.GetTokenData(op)
	if td != nil {
		t.Errorf("expected nil for UTXO without token, got %+v", td)
	}
}

func TestUTXOTokenAdapter_GetTokenData_NotFound(t *testing.T) {
	set := &mockUTXOSet{utxos: map[types.Outpoint]*utxo.UTXO{}}

	adapter := &UTXOTokenAdapter{Set: set}
	td := adapter.GetTokenData(types.Outpoint{TxID: types.Hash{0xCC}, Index: 0})
	if td != nil {
		t.Errorf("expected nil for missing outpoint, got %+v", td)
	}
}

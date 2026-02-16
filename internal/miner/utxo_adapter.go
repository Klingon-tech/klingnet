package miner

import (
	"log"

	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// UTXOAdapter bridges utxo.Set to tx.UTXOProvider.
type UTXOAdapter struct {
	set utxo.Set
}

// NewUTXOAdapter creates a UTXOProvider from a utxo.Set.
func NewUTXOAdapter(set utxo.Set) *UTXOAdapter {
	return &UTXOAdapter{set: set}
}

// GetUTXO returns the value and script for a given outpoint.
func (a *UTXOAdapter) GetUTXO(outpoint types.Outpoint) (uint64, types.Script, error) {
	u, err := a.set.Get(outpoint)
	if err != nil {
		return 0, types.Script{}, err
	}
	return u.Value, u.Script, nil
}

// HasUTXO returns whether the outpoint exists in the UTXO set.
func (a *UTXOAdapter) HasUTXO(outpoint types.Outpoint) bool {
	has, err := a.set.Has(outpoint)
	if err != nil {
		log.Printf("utxo adapter: Has(%s) error: %v", outpoint, err)
		return false
	}
	return has
}

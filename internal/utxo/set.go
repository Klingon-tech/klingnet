// Package utxo manages the UTXO set.
package utxo

import "github.com/Klingon-tech/klingnet-chain/pkg/types"

// UTXO represents an unspent transaction output.
type UTXO struct {
	Outpoint    types.Outpoint   `json:"outpoint"`
	Value       uint64           `json:"value"`
	Script      types.Script     `json:"script"`
	Token       *types.TokenData `json:"token,omitempty"`
	Height      uint64           `json:"height"`
	Coinbase    bool             `json:"coinbase"`
	LockedUntil uint64           `json:"locked_until,omitempty"`
}

// Set is the interface for UTXO storage.
type Set interface {
	Get(outpoint types.Outpoint) (*UTXO, error)
	Put(utxo *UTXO) error
	Delete(outpoint types.Outpoint) error
	Has(outpoint types.Outpoint) (bool, error)
}

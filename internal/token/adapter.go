package token

import (
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// UTXOTokenAdapter adapts a utxo.Set to the InputTokens interface
// needed by ValidateTokens.
type UTXOTokenAdapter struct {
	Set utxo.Set
}

// GetTokenData returns the token data for a given outpoint, or nil if none.
func (a *UTXOTokenAdapter) GetTokenData(op types.Outpoint) *types.TokenData {
	u, err := a.Set.Get(op)
	if err != nil {
		return nil
	}
	return u.Token
}

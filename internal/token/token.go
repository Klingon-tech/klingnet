// Package token implements the colored-coin style token system.
//
// Tokens are identified by a TokenID derived from their issuance outpoint.
// They ride on UTXOs via the TokenData field and follow conservation rules:
// for each TokenID in a transaction, total token inputs must equal total token
// outputs (transfers), or the transaction must be a mint (creating tokens)
// or burn (destroying tokens).
package token

import (
	"encoding/binary"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// DeriveTokenID computes a deterministic TokenID from the first input outpoint
// of the minting transaction. This avoids a circular dependency (the tx hash
// depends on outputs which contain the TokenID).
// TokenID = BLAKE3(first_input_txid || first_input_index).
func DeriveTokenID(firstInputTxID types.Hash, firstInputIndex uint32) types.TokenID {
	var buf [types.HashSize + 4]byte
	copy(buf[:types.HashSize], firstInputTxID[:])
	binary.LittleEndian.PutUint32(buf[types.HashSize:], firstInputIndex)
	hash := crypto.Hash(buf[:])
	return types.TokenID(hash)
}

// Metadata holds descriptive information about a token.
type Metadata struct {
	Name     string        `json:"name"`
	Symbol   string        `json:"symbol"`
	Decimals uint8         `json:"decimals"`
	Creator  types.Address `json:"creator"`
}

package types

// TokenData holds token information attached to a UTXO.
type TokenData struct {
	ID     TokenID `json:"id"`
	Amount uint64  `json:"amount"`
}

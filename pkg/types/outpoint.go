package types

import "fmt"

// Outpoint references a specific output in a transaction.
type Outpoint struct {
	TxID  Hash   `json:"txid"`
	Index uint32 `json:"index"`
}

// IsZero returns true if the outpoint has a zero TxID and zero index.
func (o Outpoint) IsZero() bool {
	return o.TxID.IsZero() && o.Index == 0
}

// String returns "txid:index" in hex.
func (o Outpoint) String() string {
	return fmt.Sprintf("%s:%d", o.TxID.String(), o.Index)
}

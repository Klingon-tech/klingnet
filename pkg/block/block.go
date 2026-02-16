// Package block defines block types and validation.
package block

import "github.com/Klingon-tech/klingnet-chain/pkg/tx"

// Block represents a block in the chain.
type Block struct {
	Header       *Header           `json:"header"`
	Transactions []*tx.Transaction `json:"transactions"`
}

// NewBlock creates a new block with the given header and transactions.
func NewBlock(header *Header, txs []*tx.Transaction) *Block {
	return &Block{
		Header:       header,
		Transactions: txs,
	}
}

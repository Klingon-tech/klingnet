package block

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// Validation errors.
var (
	ErrNilHeader           = errors.New("block has nil header")
	ErrNoTransactions      = errors.New("block has no transactions")
	ErrBadMerkleRoot       = errors.New("merkle root mismatch")
	ErrBadVersion          = errors.New("unsupported block version")
	ErrZeroTimestamp       = errors.New("block timestamp is zero")
	ErrBadTxOrder          = errors.New("transactions not in canonical order")
	ErrNoCoinbase          = errors.New("first transaction must be coinbase")
	ErrTooManyTxs          = errors.New("too many transactions in block")
	ErrBlockTooLarge       = errors.New("block too large")
	ErrDuplicateBlockInput = errors.New("duplicate input across transactions in block")
	ErrMultipleCoinbase    = errors.New("multiple coinbase transactions in block")
)

// Block version constants.
const (
	CurrentVersion = 1 // The current block version produced by this software.
	MaxVersion     = 1 // Bump when a fork introduces a new block version.
)

// Validate checks block structure and internal consistency.
// This does NOT verify consensus rules (use consensus.Engine for that).
func (b *Block) Validate() error {
	if b.Header == nil {
		return ErrNilHeader
	}

	if b.Header.Version < 1 || b.Header.Version > MaxVersion {
		return fmt.Errorf("%w: got %d, want 1..%d", ErrBadVersion, b.Header.Version, MaxVersion)
	}

	if b.Header.Timestamp == 0 {
		return ErrZeroTimestamp
	}

	if len(b.Transactions) == 0 {
		return ErrNoTransactions
	}

	if len(b.Transactions) > config.MaxBlockTxs {
		return fmt.Errorf("%w: %d txs, max %d", ErrTooManyTxs, len(b.Transactions), config.MaxBlockTxs)
	}

	// Check total block size (header signing bytes + all tx signing bytes).
	blockSize := len(b.Header.SigningBytes())
	for _, t := range b.Transactions {
		blockSize += len(t.SigningBytes())
	}
	if blockSize > config.MaxBlockSize {
		return fmt.Errorf("%w: %d bytes, max %d", ErrBlockTooLarge, blockSize, config.MaxBlockSize)
	}

	// Verify coinbase: first tx must have a zero-outpoint input.
	if !isCoinbase(b.Transactions[0]) {
		return ErrNoCoinbase
	}
	// Exactly one coinbase transaction per block.
	for i, tx := range b.Transactions[1:] {
		for _, in := range tx.Inputs {
			if in.PrevOut.IsZero() {
				return fmt.Errorf("tx %d: %w", i+1, ErrMultipleCoinbase)
			}
		}
	}

	// Verify merkle root.
	txHashes := make([]types.Hash, len(b.Transactions))
	for i, tx := range b.Transactions {
		txHashes[i] = tx.Hash()
	}
	expectedRoot := ComputeMerkleRoot(txHashes)
	if b.Header.MerkleRoot != expectedRoot {
		return fmt.Errorf("%w: header=%s computed=%s", ErrBadMerkleRoot, b.Header.MerkleRoot, expectedRoot)
	}

	// Canonical tx ordering: coinbase first, remaining sorted by hash ascending.
	for i := 2; i < len(txHashes); i++ {
		if bytes.Compare(txHashes[i-1][:], txHashes[i][:]) >= 0 {
			return fmt.Errorf("%w: tx %d hash >= tx %d hash", ErrBadTxOrder, i-1, i)
		}
	}

	// Validate each transaction structurally.
	for i, tx := range b.Transactions {
		if err := tx.Validate(); err != nil {
			return fmt.Errorf("tx %d: %w", i, err)
		}
	}

	// Check for duplicate inputs across different transactions in the block.
	// (Per-tx duplicates are caught by tx.Validate above.)
	allInputs := make(map[types.Outpoint]int) // outpoint -> tx index
	for i, tx := range b.Transactions {
		for _, in := range tx.Inputs {
			if in.PrevOut.IsZero() {
				continue // Coinbase inputs.
			}
			if prevTx, exists := allInputs[in.PrevOut]; exists {
				return fmt.Errorf("tx %d: %w: outpoint %s also spent in tx %d",
					i, ErrDuplicateBlockInput, in.PrevOut, prevTx)
			}
			allInputs[in.PrevOut] = i
		}
	}

	return nil
}

// isCoinbase returns true if the transaction has a zero-outpoint input (coinbase marker).
func isCoinbase(t *tx.Transaction) bool {
	return len(t.Inputs) == 1 && t.Inputs[0].PrevOut.IsZero()
}

// Hash returns the block header hash.
func (b *Block) Hash() types.Hash {
	if b.Header == nil {
		return types.Hash{}
	}
	return b.Header.Hash()
}

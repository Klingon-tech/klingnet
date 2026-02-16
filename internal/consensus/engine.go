// Package consensus defines consensus engine interfaces.
package consensus

import "github.com/Klingon-tech/klingnet-chain/pkg/block"

// Engine is the interface for consensus implementations.
type Engine interface {
	VerifyHeader(header *block.Header) error
	Prepare(header *block.Header) error
	Seal(blk *block.Block) error
}

// StakeChecker verifies that a validator has sufficient stake locked on-chain.
type StakeChecker interface {
	HasStake(pubKey []byte) (bool, error)
}

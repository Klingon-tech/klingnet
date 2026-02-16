package consensus

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
)

// PoW errors.
var (
	ErrInsufficientWork = errors.New("hash does not meet difficulty target")
	ErrZeroDifficulty   = errors.New("difficulty must be > 0")
	ErrBadDifficulty    = errors.New("block difficulty does not match expected")
)

// maxUint256 is 2^256 - 1.
var maxUint256 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

// PoW implements proof-of-work consensus.
// Difficulty is stored in the block header (consensus-enforced).
// The engine itself holds no mutable state — all difficulty is derived
// from the chain and encoded in each block.
type PoW struct {
	InitialDifficulty uint64 // Starting difficulty (from genesis/registration)
	AdjustInterval    int    // Blocks between difficulty adjustments (0 = no adjustment)
	TargetBlockTime   int    // Target seconds between blocks

	// DifficultyFn is called by Prepare to compute the expected difficulty
	// for a new block. Set by the node operator (klingnetd). If nil, Prepare
	// uses InitialDifficulty.
	DifficultyFn func(height uint64) uint64
}

// NewPoW creates a new PoW engine.
func NewPoW(difficulty uint64, adjustInterval, targetBlockTime int) (*PoW, error) {
	if difficulty == 0 {
		return nil, ErrZeroDifficulty
	}
	return &PoW{
		InitialDifficulty: difficulty,
		AdjustInterval:    adjustInterval,
		TargetBlockTime:   targetBlockTime,
	}, nil
}

// ShouldAdjust returns true if difficulty should be recalculated at this height.
func (p *PoW) ShouldAdjust(height uint64) bool {
	return height > 0 && p.AdjustInterval > 0 && height%uint64(p.AdjustInterval) == 0
}

// target returns MaxUint256 / difficulty as a 256-bit big.Int.
func target(difficulty uint64) *big.Int {
	d := new(big.Int).SetUint64(difficulty)
	return new(big.Int).Div(maxUint256, d)
}

// VerifyHeader checks that the block header hash meets the stated difficulty.
// The difficulty value comes from the header itself (consensus-enforced).
func (p *PoW) VerifyHeader(header *block.Header) error {
	if header.Difficulty == 0 {
		return ErrZeroDifficulty
	}
	t := target(header.Difficulty)
	hash := crypto.Hash(header.SigningBytes())
	hashInt := new(big.Int).SetBytes(hash[:])
	if hashInt.Cmp(t) > 0 {
		return ErrInsufficientWork
	}
	return nil
}

// Prepare sets the block header's difficulty for mining.
// If DifficultyFn is set, it computes the expected difficulty from chain state.
// Otherwise, uses InitialDifficulty.
func (p *PoW) Prepare(header *block.Header) error {
	if p.DifficultyFn != nil {
		header.Difficulty = p.DifficultyFn(header.Height)
	} else {
		header.Difficulty = p.InitialDifficulty
	}
	return nil
}

// Seal mines the block by iterating the nonce until the header hash meets the target.
// Uses the difficulty already set in the block header.
func (p *PoW) Seal(blk *block.Block) error {
	if blk == nil || blk.Header == nil {
		return fmt.Errorf("nil block or header")
	}
	if blk.Header.Difficulty == 0 {
		return ErrZeroDifficulty
	}

	t := target(blk.Header.Difficulty)

	for nonce := uint64(0); ; nonce++ {
		blk.Header.Nonce = nonce
		hash := crypto.Hash(blk.Header.SigningBytes())
		hashInt := new(big.Int).SetBytes(hash[:])
		if hashInt.Cmp(t) <= 0 {
			return nil
		}
		// Overflow protection (practically unreachable with reasonable difficulty).
		if nonce == ^uint64(0) {
			return fmt.Errorf("nonce space exhausted")
		}
	}
}

// ExpectedDifficulty computes the correct difficulty for a block at the given height.
// prevDifficulty is the difficulty from the block at height-1 (0 for height <= 1).
// getTimestamp retrieves a block's timestamp by height (for adjustment calculation).
func (p *PoW) ExpectedDifficulty(height uint64, prevDifficulty uint64, getTimestamp func(uint64) (uint64, error)) uint64 {
	// First PoW block or no previous difficulty: use initial.
	if height <= 1 || prevDifficulty == 0 {
		return p.InitialDifficulty
	}

	// Not at an adjustment boundary: carry forward previous difficulty.
	if !p.ShouldAdjust(height) {
		return prevDifficulty
	}

	// At adjustment boundary: compute from timestamps.
	interval := uint64(p.AdjustInterval)
	startTS, err := getTimestamp(height - interval)
	if err != nil {
		return prevDifficulty
	}
	endTS, err := getTimestamp(height - 1)
	if err != nil {
		return prevDifficulty
	}

	actual := int64(endTS - startTS)
	expected := int64(p.AdjustInterval) * int64(p.TargetBlockTime)
	return CalcNextDifficulty(prevDifficulty, actual, expected)
}

// VerifyDifficulty checks that a block header's stated difficulty matches
// the expected difficulty computed from chain history.
func (p *PoW) VerifyDifficulty(header *block.Header, prevDifficulty uint64, getTimestamp func(uint64) (uint64, error)) error {
	expected := p.ExpectedDifficulty(header.Height, prevDifficulty, getTimestamp)
	if header.Difficulty != expected {
		return fmt.Errorf("%w: height %d has difficulty %d, want %d",
			ErrBadDifficulty, header.Height, header.Difficulty, expected)
	}
	return nil
}

// CalcNextDifficulty computes the new difficulty after a retarget period.
// actualTimeSpan is the elapsed seconds for the last interval.
// expectedTimeSpan is interval * targetBlockTime.
// The result is clamped to [oldDiff/4, oldDiff*4] and never below 1.
func CalcNextDifficulty(currentDiff uint64, actualTimeSpan, expectedTimeSpan int64) uint64 {
	if actualTimeSpan <= 0 {
		actualTimeSpan = 1
	}
	if expectedTimeSpan <= 0 {
		expectedTimeSpan = 1
	}

	// Clamp actual to [expected/4, expected*4] to limit adjustment per period.
	minSpan := expectedTimeSpan / 4
	maxSpan := expectedTimeSpan * 4
	if minSpan == 0 {
		minSpan = 1
	}
	if actualTimeSpan < minSpan {
		actualTimeSpan = minSpan
	}
	if actualTimeSpan > maxSpan {
		actualTimeSpan = maxSpan
	}

	// newDiff = currentDiff * expected / actual (use big.Int to avoid overflow).
	cur := new(big.Int).SetUint64(currentDiff)
	exp := new(big.Int).SetInt64(expectedTimeSpan)
	act := new(big.Int).SetInt64(actualTimeSpan)

	result := new(big.Int).Mul(cur, exp)
	result.Div(result, act)

	// Ensure minimum difficulty of 1.
	if result.Sign() <= 0 || !result.IsUint64() {
		return 1
	}
	d := result.Uint64()
	if d < 1 {
		d = 1
	}
	return d
}

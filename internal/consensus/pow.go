package consensus

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sync"

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

	// Threads controls the number of parallel mining goroutines.
	// 0 or 1 = single-threaded (default). Each goroutine searches a
	// strided partition of the nonce space.
	Threads int
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
// If Threads > 1, mining runs in parallel goroutines.
func (p *PoW) Seal(blk *block.Block) error {
	return p.SealWithCancel(context.Background(), blk)
}

// SealWithCancel mines the block with cancellation support.
// When the context is cancelled, mining stops and ctx.Err() is returned.
// If Threads > 1, mining runs in parallel goroutines with strided nonce partitioning.
func (p *PoW) SealWithCancel(ctx context.Context, blk *block.Block) error {
	if blk == nil || blk.Header == nil {
		return fmt.Errorf("nil block or header")
	}
	if blk.Header.Difficulty == 0 {
		return ErrZeroDifficulty
	}

	threads := p.Threads
	if threads <= 1 {
		return p.sealSingle(ctx, blk)
	}
	return p.sealParallel(ctx, blk, threads)
}

// signingPrefix returns the header's signing bytes WITHOUT the trailing nonce.
// This lets each mining goroutine pre-compute the 92-byte prefix once and only
// append+hash the 8-byte nonce per iteration.
func signingPrefix(h *block.Header) []byte {
	buf := make([]byte, 0, 92)
	buf = binary.LittleEndian.AppendUint32(buf, h.Version)
	buf = append(buf, h.PrevHash[:]...)
	buf = append(buf, h.MerkleRoot[:]...)
	buf = binary.LittleEndian.AppendUint64(buf, h.Timestamp)
	buf = binary.LittleEndian.AppendUint64(buf, h.Height)
	buf = binary.LittleEndian.AppendUint64(buf, h.Difficulty)
	return buf
}

// sealSingle mines with a single goroutine.
func (p *PoW) sealSingle(ctx context.Context, blk *block.Block) error {
	t := target(blk.Header.Difficulty)
	prefix := signingPrefix(blk.Header)
	buf := make([]byte, len(prefix)+8)
	copy(buf, prefix)
	hashInt := new(big.Int)

	for nonce := uint64(0); ; nonce++ {
		// Check cancellation every 65536 iterations.
		if nonce&0xFFFF == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		binary.LittleEndian.PutUint64(buf[len(prefix):], nonce)
		hash := crypto.Hash(buf)
		hashInt.SetBytes(hash[:])
		if hashInt.Cmp(t) <= 0 {
			blk.Header.Nonce = nonce
			return nil
		}
		if nonce == ^uint64(0) {
			return fmt.Errorf("nonce space exhausted")
		}
	}
}

// sealParallel mines with multiple goroutines, each searching a strided
// partition of the nonce space (goroutine i starts at nonce=i, step=threads).
func (p *PoW) sealParallel(ctx context.Context, blk *block.Block, threads int) error {
	t := target(blk.Header.Difficulty)
	prefix := signingPrefix(blk.Header)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		nonce uint64
		err   error
	}
	found := make(chan result, 1)

	var wg sync.WaitGroup
	for i := 0; i < threads; i++ {
		wg.Add(1)
		startNonce := uint64(i)
		stride := uint64(threads)
		go func() {
			defer wg.Done()
			buf := make([]byte, len(prefix)+8)
			copy(buf, prefix)
			hashInt := new(big.Int)

			for nonce := startNonce; ; nonce += stride {
				// Check cancellation every ~65536 iterations per goroutine.
				if (nonce/stride)&0xFFFF == 0 && nonce > 0 {
					select {
					case <-ctx.Done():
						return
					default:
					}
				}

				binary.LittleEndian.PutUint64(buf[len(prefix):], nonce)
				hash := crypto.Hash(buf)
				hashInt.SetBytes(hash[:])
				if hashInt.Cmp(t) <= 0 {
					select {
					case found <- result{nonce: nonce}:
					default:
					}
					cancel()
					return
				}

				// Overflow: would wrap around past max uint64.
				if nonce > ^uint64(0)-stride {
					select {
					case found <- result{err: fmt.Errorf("nonce space exhausted")}:
					default:
					}
					return
				}
			}
		}()
	}

	// Wait in background so goroutines are cleaned up.
	go func() {
		wg.Wait()
		close(found)
	}()

	select {
	case r, ok := <-found:
		if !ok {
			return fmt.Errorf("nonce space exhausted")
		}
		if r.err != nil {
			return r.err
		}
		blk.Header.Nonce = r.nonce
		return nil
	case <-ctx.Done():
		return ctx.Err()
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

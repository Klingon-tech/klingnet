package consensus

import (
	"math/big"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func TestNewPoW_ZeroDifficulty(t *testing.T) {
	_, err := NewPoW(0, 0, 3)
	if err != ErrZeroDifficulty {
		t.Fatalf("NewPoW(0) err = %v, want ErrZeroDifficulty", err)
	}
}

func TestPoW_Target(t *testing.T) {
	// Difficulty 1: target = MaxUint256 / 1 = MaxUint256.
	t1 := target(1)
	if t1.Cmp(maxUint256) != 0 {
		t.Fatalf("target(1) = %s, want maxUint256", t1)
	}

	// Difficulty 2: target = MaxUint256 / 2.
	t2 := target(2)
	halfMax := new(big.Int).Div(maxUint256, big.NewInt(2))
	if t2.Cmp(halfMax) != 0 {
		t.Fatalf("target(2) = %s, want %s", t2, halfMax)
	}
}

func TestPoW_SealAndVerify(t *testing.T) {
	// Very low difficulty so seal completes instantly.
	pow, err := NewPoW(1, 0, 3)
	if err != nil {
		t.Fatal(err)
	}

	header := &block.Header{
		Version:    1,
		PrevHash:   types.Hash{},
		MerkleRoot: types.Hash{1, 2, 3},
		Timestamp:  1000,
		Height:     1,
		Difficulty: 1,
	}

	blk := block.NewBlock(header, nil)
	if err := pow.Seal(blk); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// Verify should pass.
	if err := pow.VerifyHeader(blk.Header); err != nil {
		t.Fatalf("VerifyHeader after Seal: %v", err)
	}
}

func TestPoW_VerifyHeader_Rejects(t *testing.T) {
	pow, err := NewPoW(1, 0, 3)
	if err != nil {
		t.Fatal(err)
	}

	// Very high difficulty in header — nearly impossible for a random nonce.
	header := &block.Header{
		Version:    1,
		PrevHash:   types.Hash{},
		MerkleRoot: types.Hash{1, 2, 3},
		Timestamp:  1000,
		Height:     1,
		Difficulty: ^uint64(0),
		Nonce:      42,
	}

	err = pow.VerifyHeader(header)
	if err != ErrInsufficientWork {
		t.Fatalf("VerifyHeader with max difficulty = %v, want ErrInsufficientWork", err)
	}
}

func TestPoW_VerifyHeader_ZeroDifficulty(t *testing.T) {
	pow, err := NewPoW(1, 0, 3)
	if err != nil {
		t.Fatal(err)
	}

	header := &block.Header{
		Version:    1,
		Height:     1,
		Difficulty: 0, // Missing difficulty in header.
	}

	err = pow.VerifyHeader(header)
	if err != ErrZeroDifficulty {
		t.Fatalf("VerifyHeader(difficulty=0) = %v, want ErrZeroDifficulty", err)
	}
}

func TestPoW_SealModerateDifficulty(t *testing.T) {
	// Moderate difficulty: target has ~248 leading 1-bits (difficulty = 256).
	// Should find a nonce within a few hundred iterations.
	pow, err := NewPoW(256, 0, 3)
	if err != nil {
		t.Fatal(err)
	}

	header := &block.Header{
		Version:    1,
		PrevHash:   types.Hash{},
		MerkleRoot: types.Hash{0xDE, 0xAD},
		Timestamp:  12345,
		Height:     5,
		Difficulty: 256,
	}
	blk := block.NewBlock(header, nil)

	if err := pow.Seal(blk); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// Verify passes.
	if err := pow.VerifyHeader(blk.Header); err != nil {
		t.Fatalf("VerifyHeader: %v", err)
	}

	// Verify the hash is actually below target.
	hash := crypto.Hash(blk.Header.SigningBytes())
	hashInt := new(big.Int).SetBytes(hash[:])
	tgt := target(256)
	if hashInt.Cmp(tgt) > 0 {
		t.Fatalf("hash %s > target %s", hashInt, tgt)
	}
}

func TestPoW_Prepare_SetsDifficulty(t *testing.T) {
	pow, _ := NewPoW(42, 0, 3)
	header := &block.Header{Height: 1, Version: 1, Timestamp: 1}
	if err := pow.Prepare(header); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	// Without DifficultyFn, Prepare uses InitialDifficulty.
	if header.Difficulty != 42 {
		t.Fatalf("Prepare set difficulty = %d, want 42", header.Difficulty)
	}
}

func TestPoW_Prepare_UsesDifficultyFn(t *testing.T) {
	pow, _ := NewPoW(10, 0, 3)
	pow.DifficultyFn = func(height uint64) uint64 {
		return height * 100
	}

	header := &block.Header{Height: 5, Version: 1, Timestamp: 1}
	if err := pow.Prepare(header); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if header.Difficulty != 500 {
		t.Fatalf("Prepare with DifficultyFn set difficulty = %d, want 500", header.Difficulty)
	}
}

// ── Difficulty adjustment tests ──────────────────────────────────────

func TestCalcNextDifficulty_ExactTarget(t *testing.T) {
	// Blocks arrived exactly on time → difficulty unchanged.
	got := CalcNextDifficulty(1000, 600, 600)
	if got != 1000 {
		t.Fatalf("CalcNextDifficulty(exact) = %d, want 1000", got)
	}
}

func TestCalcNextDifficulty_TooFast(t *testing.T) {
	// Blocks 2x faster → difficulty should ~double.
	// actual=300, expected=600 → newDiff = 1000 * 600/300 = 2000
	got := CalcNextDifficulty(1000, 300, 600)
	if got != 2000 {
		t.Fatalf("CalcNextDifficulty(2x fast) = %d, want 2000", got)
	}
}

func TestCalcNextDifficulty_TooSlow(t *testing.T) {
	// Blocks 2x slower → difficulty should ~halve.
	// actual=1200, expected=600 → newDiff = 1000 * 600/1200 = 500
	got := CalcNextDifficulty(1000, 1200, 600)
	if got != 500 {
		t.Fatalf("CalcNextDifficulty(2x slow) = %d, want 500", got)
	}
}

func TestCalcNextDifficulty_ClampUp(t *testing.T) {
	// Blocks 10x faster → clamped to 4x increase.
	// actual=60, expected=600 → clamped actual to 600/4=150
	// newDiff = 1000 * 600/150 = 4000
	got := CalcNextDifficulty(1000, 60, 600)
	if got != 4000 {
		t.Fatalf("CalcNextDifficulty(clamp up) = %d, want 4000", got)
	}
}

func TestCalcNextDifficulty_ClampDown(t *testing.T) {
	// Blocks 10x slower → clamped to 0.25x decrease.
	// actual=6000, expected=600 → clamped actual to 600*4=2400
	// newDiff = 1000 * 600/2400 = 250
	got := CalcNextDifficulty(1000, 6000, 600)
	if got != 250 {
		t.Fatalf("CalcNextDifficulty(clamp down) = %d, want 250", got)
	}
}

func TestCalcNextDifficulty_MinOne(t *testing.T) {
	// Very low difficulty + very slow blocks → must never go below 1.
	got := CalcNextDifficulty(1, 10000, 10)
	if got < 1 {
		t.Fatalf("CalcNextDifficulty(min) = %d, want >= 1", got)
	}
}

func TestPoW_ShouldAdjust(t *testing.T) {
	pow, _ := NewPoW(1, 10, 3)

	tests := []struct {
		height uint64
		want   bool
	}{
		{0, false},  // Genesis: never adjust
		{1, false},  // Not at boundary
		{9, false},  // One before boundary
		{10, true},  // First boundary
		{11, false}, // One after boundary
		{20, true},  // Second boundary
		{30, true},  // Third boundary
		{100, true}, // 10th boundary
	}

	for _, tt := range tests {
		got := pow.ShouldAdjust(tt.height)
		if got != tt.want {
			t.Errorf("ShouldAdjust(%d) = %v, want %v", tt.height, got, tt.want)
		}
	}

	// AdjustInterval=0 → never adjust.
	pow0, _ := NewPoW(1, 0, 3)
	if pow0.ShouldAdjust(10) {
		t.Error("ShouldAdjust with interval=0 should be false")
	}
}

func TestPoW_ExpectedDifficulty(t *testing.T) {
	pow, _ := NewPoW(100, 10, 3) // Adjust every 10 blocks, target 3s/block

	// At height <= 1: always returns InitialDifficulty.
	if got := pow.ExpectedDifficulty(0, 0, nil); got != 100 {
		t.Fatalf("ExpectedDifficulty(0) = %d, want 100", got)
	}
	if got := pow.ExpectedDifficulty(1, 0, nil); got != 100 {
		t.Fatalf("ExpectedDifficulty(1) = %d, want 100", got)
	}

	// At non-boundary: carry forward previous difficulty.
	if got := pow.ExpectedDifficulty(5, 200, nil); got != 200 {
		t.Fatalf("ExpectedDifficulty(5, prev=200) = %d, want 200", got)
	}

	// At boundary (height=10): compute from timestamps.
	// expected = AdjustInterval * TargetBlockTime = 10 * 3 = 30s.
	// getTimestamp(0) → start, getTimestamp(9) → end.
	// actual = end - start. For exact match, end - start must equal 30.
	getTS := func(h uint64) (uint64, error) {
		// Map: 0→0, 9→30 so actual=30=expected.
		if h == 0 {
			return 0, nil
		}
		return 30, nil // Only heights 0 and 9 are queried.
	}
	if got := pow.ExpectedDifficulty(10, 200, getTS); got != 200 {
		t.Fatalf("ExpectedDifficulty(10, exact) = %d, want 200", got)
	}

	// Blocks 2x faster: actual = 15s vs expected = 30s → difficulty doubles.
	getFastTS := func(h uint64) (uint64, error) {
		if h == 0 {
			return 0, nil
		}
		return 15, nil
	}
	if got := pow.ExpectedDifficulty(10, 200, getFastTS); got != 400 {
		t.Fatalf("ExpectedDifficulty(10, 2x fast) = %d, want 400", got)
	}
}

func TestPoW_VerifyDifficulty(t *testing.T) {
	pow, _ := NewPoW(100, 10, 3)

	// Height 1 with prevDifficulty=0: expects InitialDifficulty.
	header := &block.Header{Height: 1, Difficulty: 100}
	if err := pow.VerifyDifficulty(header, 0, nil); err != nil {
		t.Fatalf("VerifyDifficulty(height=1, diff=100) = %v, want nil", err)
	}

	// Wrong difficulty at height 1.
	header2 := &block.Header{Height: 1, Difficulty: 50}
	if err := pow.VerifyDifficulty(header2, 0, nil); err == nil {
		t.Fatal("VerifyDifficulty(height=1, diff=50) = nil, want error")
	}

	// Non-boundary height: must match prevDifficulty.
	header3 := &block.Header{Height: 5, Difficulty: 200}
	if err := pow.VerifyDifficulty(header3, 200, nil); err != nil {
		t.Fatalf("VerifyDifficulty(height=5, diff=200) = %v, want nil", err)
	}
}

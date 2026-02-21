package consensus

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func testValidator(t *testing.T) (*crypto.PrivateKey, *PoA) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	poa, err := NewPoA([][]byte{key.PublicKey()}, 3)
	if err != nil {
		t.Fatalf("NewPoA() error: %v", err)
	}
	return key, poa
}

func testBlock(t *testing.T) *block.Block {
	t.Helper()

	// Coinbase (zero outpoint) must be first transaction.
	coinbase := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{}}},
		Outputs: []tx.Output{{
			Value:  1000,
			Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
		}},
	}

	merkle := block.ComputeMerkleRoot([]types.Hash{coinbase.Hash()})
	header := &block.Header{
		Version:    block.CurrentVersion,
		MerkleRoot: merkle,
		Timestamp:  1700000000,
		Height:     1,
	}
	return block.NewBlock(header, []*tx.Transaction{coinbase})
}

func TestNewPoA(t *testing.T) {
	key, _ := crypto.GenerateKey()
	poa, err := NewPoA([][]byte{key.PublicKey()}, 3)
	if err != nil {
		t.Fatalf("NewPoA() error: %v", err)
	}
	if len(poa.Validators) != 1 {
		t.Errorf("validator count = %d, want 1", len(poa.Validators))
	}
}

func TestNewPoA_NoValidators(t *testing.T) {
	_, err := NewPoA(nil, 3)
	if !errors.Is(err, ErrNoValidators) {
		t.Errorf("expected ErrNoValidators, got: %v", err)
	}
}

func TestPoA_SetSigner(t *testing.T) {
	key, poa := testValidator(t)
	err := poa.SetSigner(key)
	if err != nil {
		t.Errorf("SetSigner() error: %v", err)
	}
}

func TestPoA_SetSigner_NotValidator(t *testing.T) {
	_, poa := testValidator(t)
	otherKey, _ := crypto.GenerateKey()

	err := poa.SetSigner(otherKey)
	if !errors.Is(err, ErrNotValidator) {
		t.Errorf("expected ErrNotValidator, got: %v", err)
	}
}

func TestPoA_SealAndVerify(t *testing.T) {
	key, poa := testValidator(t)
	poa.SetSigner(key)

	blk := testBlock(t)

	poa.Prepare(blk.Header)
	err := poa.Seal(blk)
	if err != nil {
		t.Fatalf("Seal() error: %v", err)
	}

	if len(blk.Header.ValidatorSig) == 0 {
		t.Fatal("ValidatorSig should be set after Seal()")
	}

	err = poa.VerifyHeader(blk.Header)
	if err != nil {
		t.Errorf("VerifyHeader() should pass after Seal(): %v", err)
	}
}

func TestPoA_VerifyHeader_MissingSig(t *testing.T) {
	_, poa := testValidator(t)

	header := &block.Header{
		Version:   1,
		Timestamp: 1700000000,
		Height:    1,
	}

	err := poa.VerifyHeader(header)
	if !errors.Is(err, ErrMissingSig) {
		t.Errorf("expected ErrMissingSig, got: %v", err)
	}
}

func TestPoA_VerifyHeader_InvalidSig(t *testing.T) {
	_, poa := testValidator(t)

	header := &block.Header{
		Version:      1,
		Timestamp:    1700000000,
		Height:       1,
		ValidatorSig: []byte("garbage signature data that is long enough to parse"),
	}

	err := poa.VerifyHeader(header)
	if !errors.Is(err, ErrInvalidSig) {
		t.Errorf("expected ErrInvalidSig, got: %v", err)
	}
}

func TestPoA_VerifyHeader_WrongValidator(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	// PoA only trusts key1.
	poa, _ := NewPoA([][]byte{key1.PublicKey()}, 3)

	blk := testBlock(t)
	// Sign with key2 (not a validator).
	hash := blk.Header.Hash()
	sig, _ := key2.Sign(hash[:])
	blk.Header.ValidatorSig = sig

	err := poa.VerifyHeader(blk.Header)
	if !errors.Is(err, ErrInvalidSig) {
		t.Errorf("expected ErrInvalidSig for wrong validator, got: %v", err)
	}
}

func TestPoA_MultipleValidators(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	poa, _ := NewPoA([][]byte{key1.PublicKey(), key2.PublicKey()}, 3)

	// Both should be accepted as signers.
	if err := poa.SetSigner(key1); err != nil {
		t.Errorf("key1 should be valid signer: %v", err)
	}
	if err := poa.SetSigner(key2); err != nil {
		t.Errorf("key2 should be valid signer: %v", err)
	}

	// Seal with key2 and verify.
	poa.SetSigner(key2)
	blk := testBlock(t)
	poa.Prepare(blk.Header)
	poa.Seal(blk)

	if err := poa.VerifyHeader(blk.Header); err != nil {
		t.Errorf("block sealed by key2 should verify: %v", err)
	}
}

func TestPoA_Seal_NoSigner(t *testing.T) {
	key, _ := crypto.GenerateKey()
	poa, _ := NewPoA([][]byte{key.PublicKey()}, 3)
	// Don't set signer.

	blk := testBlock(t)
	poa.Prepare(blk.Header)
	err := poa.Seal(blk)
	if err == nil {
		t.Error("Seal() without signer should fail")
	}
}

func TestPoA_ImplementsEngine(t *testing.T) {
	var _ Engine = (*PoA)(nil)
}

func TestValidator_ValidateBlock(t *testing.T) {
	key, poa := testValidator(t)
	poa.SetSigner(key)

	blk := testBlock(t)
	poa.Prepare(blk.Header)
	poa.Seal(blk)

	v := NewValidator(poa)
	if err := v.ValidateBlock(blk); err != nil {
		t.Errorf("ValidateBlock() should pass: %v", err)
	}
}

func TestValidator_ValidateBlock_BadStructure(t *testing.T) {
	key, poa := testValidator(t)
	poa.SetSigner(key)

	// Block with wrong merkle root.
	blk := testBlock(t)
	blk.Header.MerkleRoot = types.Hash{0xde, 0xad}
	poa.Prepare(blk.Header)
	poa.Seal(blk)

	v := NewValidator(poa)
	err := v.ValidateBlock(blk)
	if err == nil {
		t.Error("ValidateBlock() should fail with bad merkle root")
	}
}

func TestValidator_ValidateBlock_BadConsensus(t *testing.T) {
	_, poa := testValidator(t)

	blk := testBlock(t)
	// Don't seal — no validator sig.

	v := NewValidator(poa)
	err := v.ValidateBlock(blk)
	if err == nil {
		t.Error("ValidateBlock() should fail without validator sig")
	}
}

func TestPoA_SelectValidator_Deterministic(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()
	key3, _ := crypto.GenerateKey()

	poa, _ := NewPoA([][]byte{key1.PublicKey(), key2.PublicKey(), key3.PublicKey()}, 3)

	prevHash := types.Hash{0x01, 0x02, 0x03}
	height := uint64(42)

	// Same inputs must produce the same result.
	v1 := poa.SelectValidator(height, prevHash)
	v2 := poa.SelectValidator(height, prevHash)

	if v1 == nil {
		t.Fatal("SelectValidator returned nil")
	}
	if !bytes.Equal(v1, v2) {
		t.Error("SelectValidator is not deterministic: different results for same inputs")
	}
}

func TestPoA_SelectValidator_DifferentInputs(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()
	key3, _ := crypto.GenerateKey()

	poa, _ := NewPoA([][]byte{key1.PublicKey(), key2.PublicKey(), key3.PublicKey()}, 3)

	prevHash := types.Hash{0x01, 0x02, 0x03}

	// Check that different heights can produce different validators.
	// With 3 validators and enough samples, we expect at least 2 distinct selections.
	seen := make(map[string]bool)
	for h := uint64(0); h < 100; h++ {
		v := poa.SelectValidator(h, prevHash)
		seen[string(v)] = true
	}
	if len(seen) < 2 {
		t.Errorf("Expected multiple distinct validators over 100 heights, got %d", len(seen))
	}
}

func TestPoA_SelectValidator_SingleValidator(t *testing.T) {
	key, poa := testValidator(t) // single validator

	v := poa.SelectValidator(10, types.Hash{})
	if !bytes.Equal(v, key.PublicKey()) {
		t.Error("Single validator should always be selected")
	}
}

func TestPoA_SelectValidator_Empty(t *testing.T) {
	poa := &PoA{} // No validators
	v := poa.SelectValidator(10, types.Hash{})
	if v != nil {
		t.Error("Empty validator set should return nil")
	}
}

func TestPoA_IsSelected(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	poa, _ := NewPoA([][]byte{key1.PublicKey(), key2.PublicKey()}, 3)
	poa.SetSigner(key1)

	// Find a height where key1 is selected and one where it isn't.
	var selectedHeight, notSelectedHeight uint64
	foundSelected, foundNot := false, false
	prevHash := types.Hash{0xAA}

	for h := uint64(0); h < 200; h++ {
		if poa.IsSelected(h, prevHash) {
			if !foundSelected {
				selectedHeight = h
				foundSelected = true
			}
		} else {
			if !foundNot {
				notSelectedHeight = h
				foundNot = true
			}
		}
		if foundSelected && foundNot {
			break
		}
	}

	if !foundSelected {
		t.Fatal("key1 was never selected in 200 heights")
	}
	if !foundNot {
		t.Fatal("key1 was always selected in 200 heights (expected some non-selection)")
	}

	// Verify consistency.
	if !poa.IsSelected(selectedHeight, prevHash) {
		t.Error("IsSelected should be deterministic (selected height)")
	}
	if poa.IsSelected(notSelectedHeight, prevHash) {
		t.Error("IsSelected should be deterministic (not-selected height)")
	}
}

func TestPoA_IsSelected_NoSigner(t *testing.T) {
	key, _ := crypto.GenerateKey()
	poa, _ := NewPoA([][]byte{key.PublicKey()}, 3)
	// Don't set signer.

	if poa.IsSelected(1, types.Hash{}) {
		t.Error("IsSelected should return false when no signer is set")
	}
}

func TestPoA_IdentifySigner(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	poa, _ := NewPoA([][]byte{key1.PublicKey(), key2.PublicKey()}, 3)

	// Seal with key2.
	poa.SetSigner(key2)
	blk := testBlock(t)
	poa.Prepare(blk.Header)
	poa.Seal(blk)

	signer := poa.IdentifySigner(blk.Header)
	if signer == nil {
		t.Fatal("IdentifySigner returned nil for valid block")
	}
	if !bytes.Equal(signer, key2.PublicKey()) {
		t.Errorf("IdentifySigner returned wrong key: got %x, want %x",
			signer[:8], key2.PublicKey()[:8])
	}
}

func TestPoA_IdentifySigner_NoSig(t *testing.T) {
	key, _ := crypto.GenerateKey()
	poa, _ := NewPoA([][]byte{key.PublicKey()}, 3)

	blk := testBlock(t)
	// Don't seal — no signature.
	signer := poa.IdentifySigner(blk.Header)
	if signer != nil {
		t.Error("IdentifySigner should return nil for unsigned block")
	}
}

func TestPoA_IdentifySigner_UnknownSigner(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	// PoA only knows key1.
	poa, _ := NewPoA([][]byte{key1.PublicKey()}, 3)

	blk := testBlock(t)
	// Sign with key2 (not in validator set).
	hash := blk.Header.Hash()
	sig, _ := key2.Sign(hash[:])
	blk.Header.ValidatorSig = sig

	signer := poa.IdentifySigner(blk.Header)
	if signer != nil {
		t.Error("IdentifySigner should return nil for unknown signer")
	}
}

func TestPoA_RemoveValidator(t *testing.T) {
	key, poa := testValidator(t)
	key2, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	// Add second (non-genesis) validator
	poa.AddValidator(key2.PublicKey())
	if len(poa.Validators) != 2 {
		t.Errorf("after AddValidator, validator count = %d, want 2", len(poa.Validators))
	}

	// Remove the second validator
	poa.RemoveValidator(key2.PublicKey())
	if len(poa.Validators) != 1 {
		t.Errorf("after RemoveValidator, validator count = %d, want 1", len(poa.Validators))
	}

	// Verify the remaining validator is the genesis key
	if !bytes.Equal(poa.Validators[0], key.PublicKey()) {
		t.Error("remaining validator should be the genesis key")
	}
}

func TestPoA_RemoveValidator_Genesis(t *testing.T) {
	key, poa := testValidator(t)

	// Verify initial state: 1 validator
	if len(poa.Validators) != 1 {
		t.Errorf("initial validator count = %d, want 1", len(poa.Validators))
	}

	// Try to remove genesis validator
	poa.RemoveValidator(key.PublicKey())

	// Verify it's still 1 (genesis cannot be removed)
	if len(poa.Validators) != 1 {
		t.Errorf("after RemoveValidator on genesis key, validator count = %d, want 1", len(poa.Validators))
	}

	// Verify the genesis validator is still present
	if !bytes.Equal(poa.Validators[0], key.PublicKey()) {
		t.Error("genesis validator should still be present")
	}
}

// --- Time-slot election + weighted difficulty tests ---

func TestPoA_NewPoA_InvalidBlockTime(t *testing.T) {
	key, _ := crypto.GenerateKey()
	_, err := NewPoA([][]byte{key.PublicKey()}, 0)
	if !errors.Is(err, ErrInvalidBlockTime) {
		t.Errorf("expected ErrInvalidBlockTime for blockTime=0, got: %v", err)
	}
	_, err = NewPoA([][]byte{key.PublicKey()}, -1)
	if !errors.Is(err, ErrInvalidBlockTime) {
		t.Errorf("expected ErrInvalidBlockTime for blockTime=-1, got: %v", err)
	}
}

func TestPoA_SlotValidator_Deterministic(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()
	key3, _ := crypto.GenerateKey()

	poa, _ := NewPoA([][]byte{key1.PublicKey(), key2.PublicKey(), key3.PublicKey()}, 3)

	ts := uint64(1700000000)
	v1 := poa.SlotValidator(ts)
	v2 := poa.SlotValidator(ts)

	if v1 == nil {
		t.Fatal("SlotValidator returned nil")
	}
	if !bytes.Equal(v1, v2) {
		t.Error("SlotValidator is not deterministic: different results for same timestamp")
	}
}

func TestPoA_SlotValidator_RoundRobin(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()
	key3, _ := crypto.GenerateKey()

	poa, _ := NewPoA([][]byte{key1.PublicKey(), key2.PublicKey(), key3.PublicKey()}, 3)

	// With blockTime=3 and 3 validators, consecutive 3-second slots should cycle
	// through all validators: slot = (ts / 3) % 3.
	seen := make(map[string]bool)
	for i := uint64(0); i < 3; i++ {
		ts := 1700000000 + i*3 // Timestamps 0, 3, 6 seconds apart.
		v := poa.SlotValidator(ts)
		seen[string(v)] = true
	}
	if len(seen) != 3 {
		t.Errorf("expected 3 distinct validators in round-robin, got %d", len(seen))
	}
}

func TestPoA_IsInTurn(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	poa, _ := NewPoA([][]byte{key1.PublicKey(), key2.PublicKey()}, 3)
	poa.SetSigner(key1)

	// blockTime=3, 2 validators: slot = (ts / 3) % 2.
	// Find timestamps where key1 is in-turn and out-of-turn.
	foundInTurn, foundOutOfTurn := false, false
	for i := uint64(0); i < 10; i++ {
		ts := 1700000000 + i*3
		if poa.IsInTurn(ts) {
			foundInTurn = true
		} else {
			foundOutOfTurn = true
		}
		if foundInTurn && foundOutOfTurn {
			break
		}
	}

	if !foundInTurn {
		t.Error("key1 was never in-turn")
	}
	if !foundOutOfTurn {
		t.Error("key1 was always in-turn (expected some out-of-turn)")
	}
}

func TestPoA_IsInTurn_NoSigner(t *testing.T) {
	key, _ := crypto.GenerateKey()
	poa, _ := NewPoA([][]byte{key.PublicKey()}, 3)
	// No signer set.

	if poa.IsInTurn(1700000000) {
		t.Error("IsInTurn should return false when no signer is set")
	}
}

func TestPoA_BackupDelay_InTurn(t *testing.T) {
	key, poa := testValidator(t) // Single validator.
	poa.SetSigner(key)

	// Single validator is always in-turn → delay should be 0.
	delay := poa.BackupDelay(1700000000)
	if delay != 0 {
		t.Errorf("in-turn delay = %v, want 0", delay)
	}
}

func TestPoA_BackupDelay_Distances(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()
	key3, _ := crypto.GenerateKey()

	poa, _ := NewPoA([][]byte{key1.PublicKey(), key2.PublicKey(), key3.PublicKey()}, 3)

	// blockTime=3, 3 validators. slot=(ts/3)%3.
	// ts=1700000000: slot=(1700000000/3)%3. Let's find which is in-turn.
	ts := uint64(1700000000)
	inTurn := poa.SlotValidator(ts)

	// Set signer to the in-turn validator — should get 0 delay.
	for _, k := range []*crypto.PrivateKey{key1, key2, key3} {
		if bytes.Equal(k.PublicKey(), inTurn) {
			poa.SetSigner(k)
			break
		}
	}
	delay := poa.BackupDelay(ts)
	if delay != 0 {
		t.Errorf("in-turn validator delay = %v, want 0", delay)
	}

	// Set signer to a different validator — should get non-zero delay.
	for _, k := range []*crypto.PrivateKey{key1, key2, key3} {
		if !bytes.Equal(k.PublicKey(), inTurn) {
			poa.SetSigner(k)
			delay = poa.BackupDelay(ts)
			if delay <= 0 {
				t.Errorf("out-of-turn validator delay = %v, want > 0", delay)
			}
			// Delay should be at most blockTime.
			if delay > time.Duration(3)*time.Second {
				t.Errorf("delay %v exceeds blockTime", delay)
			}
			break
		}
	}
}

func TestPoA_ValidatorCount(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	poa, _ := NewPoA([][]byte{key1.PublicKey(), key2.PublicKey()}, 3)
	if poa.ValidatorCount() != 2 {
		t.Errorf("ValidatorCount() = %d, want 2", poa.ValidatorCount())
	}
}

func TestPoA_Prepare_SetsInTurnDifficulty(t *testing.T) {
	key, poa := testValidator(t) // Single validator — always in-turn.
	poa.SetSigner(key)

	blk := testBlock(t)
	if err := poa.Prepare(blk.Header); err != nil {
		t.Fatalf("Prepare() error: %v", err)
	}
	if blk.Header.Difficulty != DiffInTurn {
		t.Errorf("Difficulty = %d, want %d (DiffInTurn)", blk.Header.Difficulty, DiffInTurn)
	}
}

func TestPoA_Prepare_SetsOutOfTurnDifficulty(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	poa, _ := NewPoA([][]byte{key1.PublicKey(), key2.PublicKey()}, 3)

	// Find a timestamp where key1 is out-of-turn.
	// slot = (ts / 3) % 2. We try timestamps until key1 is NOT in-turn.
	poa.SetSigner(key1)
	var outOfTurnTS uint64
	for i := uint64(0); i < 10; i++ {
		ts := 1700000000 + i*3
		if !poa.IsInTurn(ts) {
			outOfTurnTS = ts
			break
		}
	}
	if outOfTurnTS == 0 {
		t.Fatal("could not find out-of-turn timestamp for key1")
	}

	blk := testBlock(t)
	blk.Header.Timestamp = outOfTurnTS
	if err := poa.Prepare(blk.Header); err != nil {
		t.Fatalf("Prepare() error: %v", err)
	}
	if blk.Header.Difficulty != DiffNoTurn {
		t.Errorf("Difficulty = %d, want %d (DiffNoTurn)", blk.Header.Difficulty, DiffNoTurn)
	}
}

func TestPoA_VerifyHeader_RejectsBadDifficulty(t *testing.T) {
	key, poa := testValidator(t) // Single validator — always in-turn (expects DiffInTurn=2).
	poa.SetSigner(key)

	blk := testBlock(t)
	poa.Prepare(blk.Header)
	poa.Seal(blk)

	// Tamper: set difficulty to DiffNoTurn (wrong for in-turn signer).
	blk.Header.Difficulty = DiffNoTurn
	// Re-seal because the hash changed.
	poa.Seal(blk)

	err := poa.VerifyHeader(blk.Header)
	if !errors.Is(err, ErrBadPoADifficulty) {
		t.Errorf("expected ErrBadPoADifficulty, got: %v", err)
	}
}

func TestPoA_CanonicalValidatorOrder(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()
	key3, _ := crypto.GenerateKey()

	// Create two PoA engines with the same validators in different input order.
	pubs := [][]byte{key1.PublicKey(), key2.PublicKey(), key3.PublicKey()}
	reversed := [][]byte{key3.PublicKey(), key2.PublicKey(), key1.PublicKey()}

	poa1, _ := NewPoA(pubs, 3)
	poa2, _ := NewPoA(reversed, 3)

	// Both should have the same canonical order → same slot election.
	for i := uint64(0); i < 20; i++ {
		ts := 1700000000 + i*3
		v1 := poa1.SlotValidator(ts)
		v2 := poa2.SlotValidator(ts)
		if !bytes.Equal(v1, v2) {
			t.Fatalf("slot mismatch at ts=%d: %x vs %x", ts, v1[:8], v2[:8])
		}
	}
}

func TestPoA_AddValidator_MaintainsOrder(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()
	key3, _ := crypto.GenerateKey()

	// Create with key1 only, then add key2 and key3 in "reverse" order.
	poa1, _ := NewPoA([][]byte{key1.PublicKey()}, 3)
	poa1.AddValidator(key3.PublicKey())
	poa1.AddValidator(key2.PublicKey())

	// Create with all three at once.
	poa2, _ := NewPoA([][]byte{key1.PublicKey(), key2.PublicKey(), key3.PublicKey()}, 3)

	// Both should produce the same slot elections.
	for i := uint64(0); i < 20; i++ {
		ts := 1700000000 + i*3
		v1 := poa1.SlotValidator(ts)
		v2 := poa2.SlotValidator(ts)
		if !bytes.Equal(v1, v2) {
			t.Fatalf("slot mismatch at ts=%d after AddValidator: %x vs %x",
				ts, v1[:8], v2[:8])
		}
	}
}

// Future timestamp check is handled by chain.ProcessBlock (2-minute rule),
// not by PoA.VerifyHeader. VerifyHeader only checks structural correctness
// (signature, difficulty). See TestProcessBlock_FutureTimestamp in chain_test.go.

func TestPoA_SigningLimit(t *testing.T) {
	tests := []struct {
		n    int
		want int
	}{
		{1, 0},
		{2, 2},
		{3, 2},
		{4, 3},
		{5, 3},
		{6, 4},
		{7, 4},
		{10, 6},
	}
	for _, tt := range tests {
		pubs := make([][]byte, tt.n)
		for i := range pubs {
			key, _ := crypto.GenerateKey()
			pubs[i] = key.PublicKey()
		}
		poa, err := NewPoA(pubs, 3)
		if err != nil {
			t.Fatalf("NewPoA(n=%d): %v", tt.n, err)
		}
		got := poa.SigningLimit()
		if got != tt.want {
			t.Errorf("SigningLimit(N=%d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}

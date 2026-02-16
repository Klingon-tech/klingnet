package consensus

import (
	"errors"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// stakeTestEnv holds components for staking tests.
type stakeTestEnv struct {
	poa          *PoA
	utxoStore    *utxo.Store
	stakeChecker *UTXOStakeChecker
	genesisKey   *crypto.PrivateKey
}

func setupStakeTest(t *testing.T, minStake uint64) *stakeTestEnv {
	t.Helper()

	genesisKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	db := storage.NewMemory()
	utxoStore := utxo.NewStore(db)
	checker := NewUTXOStakeChecker(utxoStore, minStake)

	poa, err := NewPoA([][]byte{genesisKey.PublicKey()})
	if err != nil {
		t.Fatalf("NewPoA: %v", err)
	}
	poa.SetSigner(genesisKey)
	poa.SetStakeChecker(checker)

	return &stakeTestEnv{
		poa:          poa,
		utxoStore:    utxoStore,
		stakeChecker: checker,
		genesisKey:   genesisKey,
	}
}

// createStakeUTXO adds a stake UTXO for the given pubkey to the store.
func createStakeUTXO(t *testing.T, store *utxo.Store, pubKey []byte, value uint64, txData string) {
	t.Helper()
	u := &utxo.UTXO{
		Outpoint: types.Outpoint{
			TxID:  crypto.Hash([]byte(txData)),
			Index: 0,
		},
		Value: value,
		Script: types.Script{
			Type: types.ScriptTypeStake,
			Data: pubKey,
		},
		Height: 1,
	}
	if err := store.Put(u); err != nil {
		t.Fatalf("Put stake UTXO: %v", err)
	}
}

func TestStake_GenesisValidatorExempt(t *testing.T) {
	env := setupStakeTest(t, 1000)

	// Genesis validator should pass VerifyHeader without any stake.
	blk := testBlock(t)
	if err := env.poa.Seal(blk); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if err := env.poa.VerifyHeader(blk.Header); err != nil {
		t.Errorf("genesis validator should be exempt from staking: %v", err)
	}
}

func TestStake_NonGenesisWithStake(t *testing.T) {
	env := setupStakeTest(t, 1000)

	// Add a new validator with sufficient stake.
	newKey, _ := crypto.GenerateKey()
	newPub := newKey.PublicKey()
	env.poa.AddValidator(newPub)

	// Lock enough stake.
	createStakeUTXO(t, env.utxoStore, newPub, 1000, "stake-tx-1")

	// Set signer to new validator and seal a block.
	env.poa.SetSigner(newKey)
	blk := testBlock(t)
	if err := env.poa.Seal(blk); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if err := env.poa.VerifyHeader(blk.Header); err != nil {
		t.Errorf("validator with sufficient stake should pass: %v", err)
	}
}

func TestStake_NonGenesisWithoutStake(t *testing.T) {
	env := setupStakeTest(t, 1000)

	// Add a new validator without any stake.
	newKey, _ := crypto.GenerateKey()
	newPub := newKey.PublicKey()
	env.poa.AddValidator(newPub)

	// Set signer to new validator and seal a block.
	env.poa.SetSigner(newKey)
	blk := testBlock(t)
	if err := env.poa.Seal(blk); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	err := env.poa.VerifyHeader(blk.Header)
	if !errors.Is(err, ErrInsufficientStake) {
		t.Errorf("expected ErrInsufficientStake, got: %v", err)
	}
}

func TestStake_InsufficientStake(t *testing.T) {
	env := setupStakeTest(t, 1000)

	newKey, _ := crypto.GenerateKey()
	newPub := newKey.PublicKey()
	env.poa.AddValidator(newPub)

	// Lock less than required stake.
	createStakeUTXO(t, env.utxoStore, newPub, 999, "stake-tx-short")

	env.poa.SetSigner(newKey)
	blk := testBlock(t)
	env.poa.Seal(blk)

	err := env.poa.VerifyHeader(blk.Header)
	if !errors.Is(err, ErrInsufficientStake) {
		t.Errorf("expected ErrInsufficientStake for 999 < 1000, got: %v", err)
	}
}

func TestStake_MultipleUTXOsSumToEnough(t *testing.T) {
	env := setupStakeTest(t, 1000)

	newKey, _ := crypto.GenerateKey()
	newPub := newKey.PublicKey()
	env.poa.AddValidator(newPub)

	// Two UTXOs that together exceed minimum.
	createStakeUTXO(t, env.utxoStore, newPub, 600, "stake-a")
	createStakeUTXO(t, env.utxoStore, newPub, 500, "stake-b")

	env.poa.SetSigner(newKey)
	blk := testBlock(t)
	env.poa.Seal(blk)

	if err := env.poa.VerifyHeader(blk.Header); err != nil {
		t.Errorf("combined stake 1100 >= 1000 should pass: %v", err)
	}
}

func TestStake_NoCheckerMeansNoStakeRequired(t *testing.T) {
	// Create PoA without a stake checker (backward compat).
	key, _ := crypto.GenerateKey()
	poa, _ := NewPoA([][]byte{key.PublicKey()})

	// Add non-genesis validator.
	newKey, _ := crypto.GenerateKey()
	poa.AddValidator(newKey.PublicKey())
	poa.SetSigner(newKey)

	blk := testBlock(t)
	poa.Seal(blk)

	// Should pass without stake checker.
	if err := poa.VerifyHeader(blk.Header); err != nil {
		t.Errorf("no stake checker should accept any validator: %v", err)
	}
}

func TestUTXOStakeChecker_HasStake(t *testing.T) {
	db := storage.NewMemory()
	store := utxo.NewStore(db)
	checker := NewUTXOStakeChecker(store, 500)

	pubKey := make([]byte, 33)
	pubKey[0] = 0x02
	for i := 1; i < 33; i++ {
		pubKey[i] = byte(i)
	}

	// No stake.
	ok, err := checker.HasStake(pubKey)
	if err != nil {
		t.Fatalf("HasStake error: %v", err)
	}
	if ok {
		t.Error("HasStake should be false with no UTXOs")
	}

	// Add stake.
	createStakeUTXO(t, store, pubKey, 500, "st1")

	ok, err = checker.HasStake(pubKey)
	if err != nil {
		t.Fatalf("HasStake error: %v", err)
	}
	if !ok {
		t.Error("HasStake should be true with 500 >= 500")
	}
}

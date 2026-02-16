package utxo

import (
	"testing"

	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(storage.NewMemory())
}

func makeOutpoint(data string, index uint32) types.Outpoint {
	return types.Outpoint{
		TxID:  crypto.Hash([]byte(data)),
		Index: index,
	}
}

func makeUTXO(data string, index uint32, value uint64) *UTXO {
	addr := types.Address{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14}
	return &UTXO{
		Outpoint: makeOutpoint(data, index),
		Value:    value,
		Script: types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: addr[:],
		},
		Height: 1,
	}
}

func TestStore_PutAndGet(t *testing.T) {
	s := testStore(t)
	u := makeUTXO("tx1", 0, 5000)

	err := s.Put(u)
	if err != nil {
		t.Fatalf("Put() error: %v", err)
	}

	got, err := s.Get(u.Outpoint)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	if got.Value != u.Value {
		t.Errorf("Value = %d, want %d", got.Value, u.Value)
	}
	if got.Outpoint != u.Outpoint {
		t.Error("Outpoint mismatch")
	}
	if got.Height != u.Height {
		t.Errorf("Height = %d, want %d", got.Height, u.Height)
	}
}

func TestStore_GetNonexistent(t *testing.T) {
	s := testStore(t)

	_, err := s.Get(makeOutpoint("missing", 0))
	if err == nil {
		t.Error("Get() for nonexistent UTXO should return error")
	}
}

func TestStore_Has(t *testing.T) {
	s := testStore(t)
	u := makeUTXO("tx1", 0, 1000)

	ok, _ := s.Has(u.Outpoint)
	if ok {
		t.Error("Has() should be false before Put()")
	}

	s.Put(u)

	ok, err := s.Has(u.Outpoint)
	if err != nil {
		t.Fatalf("Has() error: %v", err)
	}
	if !ok {
		t.Error("Has() should be true after Put()")
	}
}

func TestStore_Delete(t *testing.T) {
	s := testStore(t)
	u := makeUTXO("tx1", 0, 1000)

	s.Put(u)

	err := s.Delete(u.Outpoint)
	if err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	ok, _ := s.Has(u.Outpoint)
	if ok {
		t.Error("UTXO should be gone after Delete()")
	}
}

func TestStore_MultipleOutputs(t *testing.T) {
	s := testStore(t)

	// Same tx, different output indices.
	u0 := makeUTXO("tx1", 0, 1000)
	u1 := makeUTXO("tx1", 1, 2000)
	u2 := makeUTXO("tx1", 2, 3000)

	s.Put(u0)
	s.Put(u1)
	s.Put(u2)

	got0, _ := s.Get(u0.Outpoint)
	got1, _ := s.Get(u1.Outpoint)
	got2, _ := s.Get(u2.Outpoint)

	if got0.Value != 1000 || got1.Value != 2000 || got2.Value != 3000 {
		t.Error("values mismatch for multi-output tx")
	}

	// Delete middle one.
	s.Delete(u1.Outpoint)

	ok, _ := s.Has(u1.Outpoint)
	if ok {
		t.Error("deleted output should be gone")
	}

	// Others should remain.
	ok0, _ := s.Has(u0.Outpoint)
	ok2, _ := s.Has(u2.Outpoint)
	if !ok0 || !ok2 {
		t.Error("non-deleted outputs should remain")
	}
}

func TestStore_TokenData(t *testing.T) {
	s := testStore(t)
	u := makeUTXO("token-tx", 0, 0)
	u.Token = &types.TokenData{
		ID:     types.TokenID{0xaa, 0xbb},
		Amount: 50000,
	}

	s.Put(u)

	got, err := s.Get(u.Outpoint)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Token == nil {
		t.Fatal("Token should not be nil")
	}
	if got.Token.Amount != 50000 {
		t.Errorf("Token.Amount = %d, want 50000", got.Token.Amount)
	}
	if got.Token.ID != u.Token.ID {
		t.Error("Token.ID mismatch")
	}
}

func TestStore_ImplementsSet(t *testing.T) {
	// Compile-time check that Store satisfies Set.
	var _ Set = (*Store)(nil)
}

// makeStakeUTXO creates a stake UTXO with the given pubkey.
func makeStakeUTXO(txData string, index uint32, value uint64, pubKey []byte) *UTXO {
	return &UTXO{
		Outpoint: makeOutpoint(txData, index),
		Value:    value,
		Script: types.Script{
			Type: types.ScriptTypeStake,
			Data: pubKey,
		},
		Height: 1,
	}
}

func TestStore_StakeIndex_PutAndGet(t *testing.T) {
	s := testStore(t)

	// Generate a fake 33-byte compressed pubkey.
	pubKey := make([]byte, 33)
	pubKey[0] = 0x02
	for i := 1; i < 33; i++ {
		pubKey[i] = byte(i)
	}

	u := makeStakeUTXO("stake-tx", 0, 1000_000_000_000, pubKey)
	if err := s.Put(u); err != nil {
		t.Fatalf("Put() error: %v", err)
	}

	stakes, err := s.GetStakes(pubKey)
	if err != nil {
		t.Fatalf("GetStakes() error: %v", err)
	}
	if len(stakes) != 1 {
		t.Fatalf("GetStakes() returned %d, want 1", len(stakes))
	}
	if stakes[0].Value != u.Value {
		t.Errorf("Value = %d, want %d", stakes[0].Value, u.Value)
	}
}

func TestStore_StakeIndex_MultipleStakes(t *testing.T) {
	s := testStore(t)

	pubKey := make([]byte, 33)
	pubKey[0] = 0x03
	for i := 1; i < 33; i++ {
		pubKey[i] = byte(i + 10)
	}

	u1 := makeStakeUTXO("stake1", 0, 500_000_000_000, pubKey)
	u2 := makeStakeUTXO("stake2", 0, 600_000_000_000, pubKey)

	s.Put(u1)
	s.Put(u2)

	stakes, err := s.GetStakes(pubKey)
	if err != nil {
		t.Fatalf("GetStakes() error: %v", err)
	}
	if len(stakes) != 2 {
		t.Fatalf("GetStakes() returned %d, want 2", len(stakes))
	}

	var total uint64
	for _, st := range stakes {
		total += st.Value
	}
	if total != 1_100_000_000_000 {
		t.Errorf("total stake = %d, want 1_100_000_000_000", total)
	}
}

func TestStore_StakeIndex_DeleteRemovesIndex(t *testing.T) {
	s := testStore(t)

	pubKey := make([]byte, 33)
	pubKey[0] = 0x02
	for i := 1; i < 33; i++ {
		pubKey[i] = byte(i + 20)
	}

	u := makeStakeUTXO("stake-del", 0, 1000_000_000_000, pubKey)
	s.Put(u)

	// Verify stake exists.
	stakes, _ := s.GetStakes(pubKey)
	if len(stakes) != 1 {
		t.Fatalf("expected 1 stake before delete, got %d", len(stakes))
	}

	// Delete the UTXO.
	if err := s.Delete(u.Outpoint); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	// Stake index should be cleaned up.
	stakes, err := s.GetStakes(pubKey)
	if err != nil {
		t.Fatalf("GetStakes() error: %v", err)
	}
	if len(stakes) != 0 {
		t.Errorf("GetStakes() returned %d after delete, want 0", len(stakes))
	}
}

func TestStore_StakeIndex_DifferentPubkeys(t *testing.T) {
	s := testStore(t)

	pk1 := make([]byte, 33)
	pk1[0] = 0x02
	pk1[1] = 0xAA

	pk2 := make([]byte, 33)
	pk2[0] = 0x03
	pk2[1] = 0xBB

	s.Put(makeStakeUTXO("s1", 0, 1000, pk1))
	s.Put(makeStakeUTXO("s2", 0, 2000, pk2))

	stakes1, _ := s.GetStakes(pk1)
	stakes2, _ := s.GetStakes(pk2)

	if len(stakes1) != 1 {
		t.Errorf("pk1 stakes = %d, want 1", len(stakes1))
	}
	if len(stakes2) != 1 {
		t.Errorf("pk2 stakes = %d, want 1", len(stakes2))
	}
	if stakes1[0].Value != 1000 {
		t.Errorf("pk1 value = %d, want 1000", stakes1[0].Value)
	}
	if stakes2[0].Value != 2000 {
		t.Errorf("pk2 value = %d, want 2000", stakes2[0].Value)
	}
}

func TestStore_StakeIndex_InvalidPubkeyLength(t *testing.T) {
	s := testStore(t)

	_, err := s.GetStakes([]byte{0x02, 0x03}) // Too short.
	if err == nil {
		t.Error("GetStakes() should fail with wrong-length pubkey")
	}
}

func TestStore_GetAllStakedValidators(t *testing.T) {
	s := testStore(t)

	// Empty store: no validators.
	vals, err := s.GetAllStakedValidators()
	if err != nil {
		t.Fatal(err)
	}
	if len(vals) != 0 {
		t.Fatalf("empty store: got %d validators, want 0", len(vals))
	}

	// Add stakes for two different pubkeys.
	pk1 := make([]byte, 33)
	pk1[0] = 0x02
	pk1[1] = 0xAA

	pk2 := make([]byte, 33)
	pk2[0] = 0x03
	pk2[1] = 0xBB

	s.Put(makeStakeUTXO("s1", 0, 1000, pk1))
	s.Put(makeStakeUTXO("s2", 0, 2000, pk2))
	// Add a second stake for pk1 (should still appear only once).
	s.Put(makeStakeUTXO("s3", 0, 500, pk1))

	vals, err = s.GetAllStakedValidators()
	if err != nil {
		t.Fatal(err)
	}
	if len(vals) != 2 {
		t.Fatalf("got %d validators, want 2", len(vals))
	}

	// Verify both pubkeys are present.
	found := make(map[string]bool)
	for _, v := range vals {
		found[string(v)] = true
	}
	if !found[string(pk1)] {
		t.Error("pk1 not found in validators")
	}
	if !found[string(pk2)] {
		t.Error("pk2 not found in validators")
	}

	// Delete all stakes for pk1 — should leave only pk2.
	s.Delete(makeOutpoint("s1", 0))
	s.Delete(makeOutpoint("s3", 0))

	vals, err = s.GetAllStakedValidators()
	if err != nil {
		t.Fatal(err)
	}
	if len(vals) != 1 {
		t.Fatalf("after delete: got %d validators, want 1", len(vals))
	}
	if string(vals[0]) != string(pk2) {
		t.Error("expected pk2 to remain")
	}
}

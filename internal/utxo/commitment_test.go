package utxo

import (
	"testing"

	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func TestCommitment_Empty(t *testing.T) {
	db := storage.NewMemory()
	store := NewStore(db)

	root, err := Commitment(store)
	if err != nil {
		t.Fatalf("Commitment: %v", err)
	}
	if !root.IsZero() {
		t.Error("empty store commitment should be zero hash")
	}
}

func TestCommitment_SingleUTXO(t *testing.T) {
	db := storage.NewMemory()
	store := NewStore(db)

	store.Put(&UTXO{
		Outpoint: types.Outpoint{TxID: types.Hash{0x01}, Index: 0},
		Value:    1000,
		Script:   types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
	})

	root, err := Commitment(store)
	if err != nil {
		t.Fatalf("Commitment: %v", err)
	}
	if root.IsZero() {
		t.Error("single UTXO commitment should not be zero")
	}
}

func TestCommitment_Deterministic(t *testing.T) {
	// Build the same store twice and check the commitment is identical.
	makeStore := func() *Store {
		db := storage.NewMemory()
		s := NewStore(db)
		s.Put(&UTXO{
			Outpoint: types.Outpoint{TxID: types.Hash{0x01}, Index: 0},
			Value:    1000,
			Script:   types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
		})
		s.Put(&UTXO{
			Outpoint: types.Outpoint{TxID: types.Hash{0x02}, Index: 1},
			Value:    2000,
			Script:   types.Script{Type: types.ScriptTypeP2PKH, Data: []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0x00, 0xab, 0xcd, 0xef, 0x01}},
		})
		return s
	}

	root1, _ := Commitment(makeStore())
	root2, _ := Commitment(makeStore())
	if root1 != root2 {
		t.Error("commitment should be deterministic")
	}
}

func TestCommitment_ChangesOnModification(t *testing.T) {
	db := storage.NewMemory()
	store := NewStore(db)

	store.Put(&UTXO{
		Outpoint: types.Outpoint{TxID: types.Hash{0x01}, Index: 0},
		Value:    1000,
		Script:   types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
	})

	root1, _ := Commitment(store)

	// Add another UTXO.
	store.Put(&UTXO{
		Outpoint: types.Outpoint{TxID: types.Hash{0x02}, Index: 0},
		Value:    2000,
		Script:   types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
	})

	root2, _ := Commitment(store)

	if root1 == root2 {
		t.Error("commitment should change after adding UTXO")
	}
}

func TestCommitment_ChangesOnDelete(t *testing.T) {
	db := storage.NewMemory()
	store := NewStore(db)

	op1 := types.Outpoint{TxID: types.Hash{0x01}, Index: 0}
	op2 := types.Outpoint{TxID: types.Hash{0x02}, Index: 0}

	store.Put(&UTXO{Outpoint: op1, Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}})
	store.Put(&UTXO{Outpoint: op2, Value: 2000, Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}})

	root1, _ := Commitment(store)

	store.Delete(op2)

	root2, _ := Commitment(store)

	if root1 == root2 {
		t.Error("commitment should change after deleting UTXO")
	}
}

func TestCommitment_OrderIndependent(t *testing.T) {
	// Insert UTXOs in different order, commitment should be the same.
	u1 := &UTXO{Outpoint: types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}}
	u2 := &UTXO{Outpoint: types.Outpoint{TxID: types.Hash{0x02}, Index: 0}, Value: 2000, Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}}

	// Order 1: u1 then u2.
	db1 := storage.NewMemory()
	s1 := NewStore(db1)
	s1.Put(u1)
	s1.Put(u2)
	root1, _ := Commitment(s1)

	// Order 2: u2 then u1.
	db2 := storage.NewMemory()
	s2 := NewStore(db2)
	s2.Put(u2)
	s2.Put(u1)
	root2, _ := Commitment(s2)

	if root1 != root2 {
		t.Error("commitment should be independent of insertion order")
	}
}

func TestForEach(t *testing.T) {
	db := storage.NewMemory()
	store := NewStore(db)

	store.Put(&UTXO{Outpoint: types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}})
	store.Put(&UTXO{Outpoint: types.Outpoint{TxID: types.Hash{0x02}, Index: 0}, Value: 2000, Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}})

	var count int
	var total uint64
	err := store.ForEach(func(u *UTXO) error {
		count++
		total += u.Value
		return nil
	})
	if err != nil {
		t.Fatalf("ForEach: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if total != 3000 {
		t.Errorf("total = %d, want 3000", total)
	}
}

func TestHashUTXO_Deterministic(t *testing.T) {
	u := &UTXO{
		Outpoint: types.Outpoint{TxID: types.Hash{0x01}, Index: 0},
		Value:    1000,
		Script:   types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)},
	}
	h1 := hashUTXO(u)
	h2 := hashUTXO(u)
	if h1 != h2 {
		t.Error("hashUTXO should be deterministic")
	}
	if h1.IsZero() {
		t.Error("hashUTXO should not be zero")
	}
}

func TestHashUTXO_DifferentValues(t *testing.T) {
	u1 := &UTXO{Outpoint: types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, Value: 1000}
	u2 := &UTXO{Outpoint: types.Outpoint{TxID: types.Hash{0x01}, Index: 0}, Value: 2000}
	if hashUTXO(u1) == hashUTXO(u2) {
		t.Error("different values should produce different hashes")
	}
}

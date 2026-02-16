package token

import (
	"errors"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func TestStore_PutGetHas(t *testing.T) {
	db := storage.NewMemory()
	store := NewStore(db)

	id := types.TokenID{0x01, 0x02, 0x03}
	meta := &Metadata{
		Name:     "Test Token",
		Symbol:   "TST",
		Decimals: 8,
		Creator:  types.Address{0xAA},
	}

	// Has should be false before Put.
	has, err := store.Has(id)
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if has {
		t.Fatal("expected Has=false before Put")
	}

	// Put.
	if err := store.Put(id, meta); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Has should be true.
	has, err = store.Has(id)
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if !has {
		t.Fatal("expected Has=true after Put")
	}

	// Get.
	got, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != meta.Name {
		t.Errorf("Name = %q, want %q", got.Name, meta.Name)
	}
	if got.Symbol != meta.Symbol {
		t.Errorf("Symbol = %q, want %q", got.Symbol, meta.Symbol)
	}
	if got.Decimals != meta.Decimals {
		t.Errorf("Decimals = %d, want %d", got.Decimals, meta.Decimals)
	}
	if got.Creator != meta.Creator {
		t.Errorf("Creator = %x, want %x", got.Creator, meta.Creator)
	}
}

func TestStore_Get_NotFound(t *testing.T) {
	db := storage.NewMemory()
	store := NewStore(db)

	_, err := store.Get(types.TokenID{0xFF})
	if err == nil {
		t.Fatal("expected error for non-existent token")
	}
}

func TestStore_List_Empty(t *testing.T) {
	db := storage.NewMemory()
	store := NewStore(db)

	entries, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestStore_List_Multiple(t *testing.T) {
	db := storage.NewMemory()
	store := NewStore(db)

	tokens := []struct {
		id   types.TokenID
		meta *Metadata
	}{
		{types.TokenID{0x01}, &Metadata{Name: "Alpha", Symbol: "ALP", Decimals: 6}},
		{types.TokenID{0x02}, &Metadata{Name: "Beta", Symbol: "BET", Decimals: 8}},
		{types.TokenID{0x03}, &Metadata{Name: "Gamma", Symbol: "GAM", Decimals: 12}},
	}

	for _, tt := range tokens {
		if err := store.Put(tt.id, tt.meta); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	entries, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Verify all tokens are present (order may vary).
	found := make(map[string]bool)
	for _, e := range entries {
		found[e.Symbol] = true
	}
	for _, tt := range tokens {
		if !found[tt.meta.Symbol] {
			t.Errorf("missing token %s", tt.meta.Symbol)
		}
	}
}

func TestStore_ForEach_StopEarly(t *testing.T) {
	db := storage.NewMemory()
	store := NewStore(db)

	for i := 0; i < 5; i++ {
		id := types.TokenID{byte(i)}
		if err := store.Put(id, &Metadata{Name: "Token", Symbol: "TKN"}); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	var count int
	errStop := errors.New("stop")
	err := store.ForEach(func(_ types.TokenID, _ *Metadata) error {
		count++
		if count >= 2 {
			return errStop
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected early-stop error")
	}
	if !errors.Is(err, errStop) {
		t.Errorf("error = %v, want %v", err, errStop)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

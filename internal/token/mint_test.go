package token

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func TestEncodeDecode_Roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		addr     types.Address
		tokName  string
		symbol   string
		decimals uint8
	}{
		{
			name:     "typical token",
			addr:     types.Address{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
			tokName:  "KlingCoin",
			symbol:   "KLC",
			decimals: 8,
		},
		{
			name:     "zero decimals",
			addr:     types.Address{0xff, 0xfe, 0xfd},
			tokName:  "NFT",
			symbol:   "N",
			decimals: 0,
		},
		{
			name:     "empty name and symbol",
			addr:     types.Address{0xaa},
			tokName:  "",
			symbol:   "",
			decimals: 12,
		},
		{
			name:     "max decimals",
			addr:     types.Address{},
			tokName:  "MaxDec",
			symbol:   "MD",
			decimals: 255,
		},
		{
			name:     "long name",
			addr:     types.Address{0x01},
			tokName:  string(make([]byte, 200)),
			symbol:   "SYM",
			decimals: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := EncodeMintData(tt.addr, tt.tokName, tt.symbol, tt.decimals)

			gotAddr, gotName, gotSymbol, gotDecimals, ok := DecodeMintData(data)
			if !ok {
				t.Fatal("DecodeMintData returned ok=false")
			}
			if gotAddr != tt.addr {
				t.Errorf("address = %x, want %x", gotAddr, tt.addr)
			}
			if gotName != tt.tokName {
				t.Errorf("name = %q, want %q", gotName, tt.tokName)
			}
			if gotSymbol != tt.symbol {
				t.Errorf("symbol = %q, want %q", gotSymbol, tt.symbol)
			}
			if gotDecimals != tt.decimals {
				t.Errorf("decimals = %d, want %d", gotDecimals, tt.decimals)
			}
		})
	}
}

func TestDecodeMintData_Legacy20Bytes(t *testing.T) {
	addr := types.Address{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	data := addr.Bytes()

	gotAddr, name, symbol, decimals, ok := DecodeMintData(data)
	if !ok {
		t.Fatal("DecodeMintData returned ok=false for 20-byte data")
	}
	if gotAddr != addr {
		t.Errorf("address = %x, want %x", gotAddr, addr)
	}
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
	if symbol != "" {
		t.Errorf("symbol = %q, want empty", symbol)
	}
	if decimals != 0 {
		t.Errorf("decimals = %d, want 0", decimals)
	}
}

func TestDecodeMintData_TooShort(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"10 bytes", make([]byte, 10)},
		{"19 bytes", make([]byte, 19)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, _, ok := DecodeMintData(tt.data)
			if ok {
				t.Error("DecodeMintData returned ok=true for short data")
			}
		})
	}
}

func TestDecodeMintData_TruncatedMetadata(t *testing.T) {
	// 20 bytes address + 1 byte decimals only (missing nameLen).
	data := make([]byte, types.AddressSize+1)
	data[types.AddressSize] = 8 // decimals

	addr, name, symbol, decimals, ok := DecodeMintData(data)
	if !ok {
		t.Fatal("DecodeMintData returned ok=false")
	}
	// Should return the address and zero metadata gracefully.
	if addr != (types.Address{}) {
		t.Errorf("address = %x, want zero", addr)
	}
	if name != "" || symbol != "" {
		t.Errorf("expected empty name/symbol, got %q/%q", name, symbol)
	}
	if decimals != 0 {
		t.Errorf("decimals = %d, want 0 (truncated metadata insufficient)", decimals)
	}
}

func TestEncodeMintData_TruncatesLongStrings(t *testing.T) {
	addr := types.Address{0x01}
	longName := string(make([]byte, 300))
	longSymbol := string(make([]byte, 300))

	data := EncodeMintData(addr, longName, longSymbol, 8)

	_, name, symbol, _, ok := DecodeMintData(data)
	if !ok {
		t.Fatal("DecodeMintData returned ok=false")
	}
	if len(name) > 255 {
		t.Errorf("name length = %d, should be truncated to 255", len(name))
	}
	if len(symbol) > 255 {
		t.Errorf("symbol length = %d, should be truncated to 255", len(symbol))
	}
}

func TestEncodeMintData_FirstBytesAreAddress(t *testing.T) {
	addr := types.Address{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04,
		0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}

	data := EncodeMintData(addr, "Token", "TKN", 6)

	// First 20 bytes must be the address (backward compat with scriptAddress).
	if !bytes.Equal(data[:types.AddressSize], addr[:]) {
		t.Errorf("first 20 bytes = %x, want %x", data[:types.AddressSize], addr[:])
	}
}

func TestExtractAndStoreMetadata(t *testing.T) {
	store := NewStore(newMemDB())

	tokenID := types.TokenID{0x01, 0x02, 0x03}
	addr := types.Address{0xaa, 0xbb}

	mintData := EncodeMintData(addr, "TestToken", "TT", 12)

	blk := &block.Block{
		Transactions: []*tx.Transaction{
			{
				Outputs: []tx.Output{
					{
						Script: types.Script{Type: types.ScriptTypeMint, Data: mintData},
						Token:  &types.TokenData{ID: tokenID, Amount: 1000},
					},
				},
			},
		},
	}

	ExtractAndStoreMetadata(store, blk)

	meta, err := store.Get(tokenID)
	if err != nil {
		t.Fatalf("Get(%x): %v", tokenID, err)
	}
	if meta.Name != "TestToken" {
		t.Errorf("name = %q, want %q", meta.Name, "TestToken")
	}
	if meta.Symbol != "TT" {
		t.Errorf("symbol = %q, want %q", meta.Symbol, "TT")
	}
	if meta.Decimals != 12 {
		t.Errorf("decimals = %d, want 12", meta.Decimals)
	}
	if meta.Creator != addr {
		t.Errorf("creator = %x, want %x", meta.Creator, addr)
	}
}

func TestExtractAndStoreMetadata_SkipsExisting(t *testing.T) {
	store := NewStore(newMemDB())

	tokenID := types.TokenID{0x01}
	original := &Metadata{Name: "Original", Symbol: "OG", Decimals: 8}
	if err := store.Put(tokenID, original); err != nil {
		t.Fatal(err)
	}

	addr := types.Address{0xcc}
	mintData := EncodeMintData(addr, "Overwrite", "OW", 6)

	blk := &block.Block{
		Transactions: []*tx.Transaction{
			{
				Outputs: []tx.Output{
					{
						Script: types.Script{Type: types.ScriptTypeMint, Data: mintData},
						Token:  &types.TokenData{ID: tokenID, Amount: 500},
					},
				},
			},
		},
	}

	ExtractAndStoreMetadata(store, blk)

	meta, err := store.Get(tokenID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "Original" {
		t.Errorf("name = %q, want %q (should not be overwritten)", meta.Name, "Original")
	}
}

func TestExtractAndStoreMetadata_SkipsLegacy(t *testing.T) {
	store := NewStore(newMemDB())

	tokenID := types.TokenID{0x02}
	addr := types.Address{0xdd}

	// Legacy: 20-byte data only (no metadata).
	blk := &block.Block{
		Transactions: []*tx.Transaction{
			{
				Outputs: []tx.Output{
					{
						Script: types.Script{Type: types.ScriptTypeMint, Data: addr.Bytes()},
						Token:  &types.TokenData{ID: tokenID, Amount: 100},
					},
				},
			},
		},
	}

	ExtractAndStoreMetadata(store, blk)

	has, _ := store.Has(tokenID)
	if has {
		t.Error("should not store metadata for legacy 20-byte mint")
	}
}

func TestExtractAndStoreMetadata_NilInputs(t *testing.T) {
	store := NewStore(newMemDB())

	// Should not panic.
	ExtractAndStoreMetadata(nil, &block.Block{})
	ExtractAndStoreMetadata(store, nil)
	ExtractAndStoreMetadata(store, &block.Block{})
	ExtractAndStoreMetadata(store, &block.Block{Transactions: []*tx.Transaction{nil}})
}

// newMemDB returns a simple in-memory DB for testing.
func newMemDB() *memDB {
	return &memDB{data: make(map[string][]byte)}
}

type memDB struct {
	data map[string][]byte
}

func (m *memDB) Get(key []byte) ([]byte, error) {
	v, ok := m.data[string(key)]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (m *memDB) Put(key, value []byte) error {
	m.data[string(key)] = append([]byte(nil), value...)
	return nil
}

func (m *memDB) Delete(key []byte) error {
	delete(m.data, string(key))
	return nil
}

func (m *memDB) Has(key []byte) (bool, error) {
	_, ok := m.data[string(key)]
	return ok, nil
}

func (m *memDB) Close() error { return nil }

func (m *memDB) ForEach(prefix []byte, fn func(key, value []byte) error) error {
	pfx := string(prefix)
	for k, v := range m.data {
		if len(k) >= len(pfx) && k[:len(pfx)] == pfx {
			if err := fn([]byte(k), v); err != nil {
				return err
			}
		}
	}
	return nil
}

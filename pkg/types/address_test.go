package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAddress_IsZero(t *testing.T) {
	var zero Address
	if !zero.IsZero() {
		t.Error("zero-value Address should be zero")
	}

	nonZero := Address{0x01}
	if nonZero.IsZero() {
		t.Error("non-zero Address should not be zero")
	}
}

func TestAddress_String(t *testing.T) {
	oldHRP := activeHRP
	defer func() { activeHRP = oldHRP }()

	SetAddressHRP(MainnetHRP)

	var a Address
	s := a.String()
	if !strings.HasPrefix(s, "kgx1") {
		t.Errorf("String() should start with 'kgx1', got %s", s)
	}

	a[0] = 0xab
	a[19] = 0xcd
	s = a.String()
	if !strings.HasPrefix(s, "kgx1") {
		t.Errorf("String() should start with 'kgx1', got %s", s)
	}
}

func TestAddress_String_Testnet(t *testing.T) {
	oldHRP := activeHRP
	defer func() { activeHRP = oldHRP }()

	SetAddressHRP(TestnetHRP)

	a := Address{0x01}
	s := a.String()
	if !strings.HasPrefix(s, "tkgx1") {
		t.Errorf("String() should start with 'tkgx1', got %s", s)
	}
}

func TestAddress_Bech32_Roundtrip(t *testing.T) {
	oldHRP := activeHRP
	defer func() { activeHRP = oldHRP }()

	SetAddressHRP(MainnetHRP)

	a := Address{0x8f, 0x3a, 0x44, 0xb8, 0x05, 0x6c, 0xaf, 0xec, 0x36, 0x8d,
		0xea, 0x0c, 0xbe, 0x0a, 0xd1, 0xd9, 0xbc, 0x3f, 0x43, 0x05}

	s := a.String()
	parsed, err := ParseAddress(s)
	if err != nil {
		t.Fatalf("ParseAddress(%q): %v", s, err)
	}
	if parsed != a {
		t.Errorf("roundtrip mismatch: got %x, want %x", parsed, a)
	}
}

func TestAddress_Hex(t *testing.T) {
	a := Address{0xab, 0xcd}
	h := a.Hex()
	if strings.Contains(h, ":") {
		t.Errorf("Hex() should not contain prefix, got %s", h)
	}
	if len(h) != 40 {
		t.Errorf("Hex() length = %d, want 40", len(h))
	}
	if !strings.HasPrefix(h, "abcd") {
		t.Errorf("Hex() should start with 'abcd', got %s", h[:4])
	}
}

func TestAddress_Bytes(t *testing.T) {
	a := Address{0x01, 0x02, 0x03}
	b := a.Bytes()

	if len(b) != AddressSize {
		t.Errorf("Bytes() length = %d, want %d", len(b), AddressSize)
	}
	if b[0] != 0x01 || b[1] != 0x02 || b[2] != 0x03 {
		t.Errorf("Bytes() content mismatch")
	}

	// Ensure it's a copy
	b[0] = 0xFF
	if a[0] == 0xFF {
		t.Error("Bytes() should return a copy, not a reference")
	}
}

func TestHexToAddress(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:  "valid 40 hex chars",
			input: "0123456789abcdef0123456789abcdef01234567",
		},
		{
			name:  "all zeros",
			input: strings.Repeat("0", 40),
		},
		{
			name:    "too short",
			input:   "abcd",
			wantErr: true,
		},
		{
			name:    "too long",
			input:   strings.Repeat("a", 42),
			wantErr: true,
		},
		{
			name:    "invalid hex",
			input:   strings.Repeat("z", 40),
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := HexToAddress(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("HexToAddress(%q) should have returned error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("HexToAddress(%q) unexpected error: %v", tt.input, err)
			}
			// Roundtrip via Hex() (not String() which now uses bech32)
			if a.Hex() != tt.input {
				t.Errorf("roundtrip: got %s, want %s", a.Hex(), tt.input)
			}
		})
	}
}

func TestParseAddress(t *testing.T) {
	oldHRP := activeHRP
	defer func() { activeHRP = oldHRP }()

	SetAddressHRP(MainnetHRP)

	rawHex := "0123456789abcdef0123456789abcdef01234567"

	// Generate bech32 version for test.
	a, _ := HexToAddress(rawHex)
	bech32Addr := a.String()

	SetAddressHRP(TestnetHRP)
	testnetBech32 := a.String()
	SetAddressHRP(MainnetHRP)

	tests := []struct {
		name    string
		input   string
		wantHex string
		wantErr bool
	}{
		{"raw hex", rawHex, rawHex, false},
		{"bech32 mainnet", bech32Addr, rawHex, false},
		{"bech32 testnet", testnetBech32, rawHex, false},
		{"legacy mainnet prefix", "kgx:" + rawHex, rawHex, false},
		{"legacy testnet prefix", "tkgx:" + rawHex, rawHex, false},
		{"invalid bech32", "kgx1invalid!!!", "", true},
		{"wrong length hex", "kgx:abcd", "", true},
		{"empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := ParseAddress(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseAddress(%q) should have returned error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAddress(%q) unexpected error: %v", tt.input, err)
			}
			if a.Hex() != tt.wantHex {
				t.Errorf("ParseAddress(%q) hex = %s, want %s", tt.input, a.Hex(), tt.wantHex)
			}
		})
	}
}

func TestAddress_JSON_RoundTrip(t *testing.T) {
	oldHRP := activeHRP
	defer func() { activeHRP = oldHRP }()

	SetAddressHRP(MainnetHRP)

	original := Address{0xab, 0xcd, 0xef}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// JSON should contain bech32 format.
	if !strings.Contains(string(data), "kgx1") {
		t.Errorf("JSON should contain bech32 format, got %s", string(data))
	}

	var decoded Address
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if original != decoded {
		t.Errorf("roundtrip mismatch: original=%x, decoded=%x", original, decoded)
	}
}

func TestAddress_JSON_UnmarshalRawHex(t *testing.T) {
	// Backward compat: unmarshal raw hex without prefix.
	rawJSON := `"0123456789abcdef0123456789abcdef01234567"`

	var a Address
	if err := json.Unmarshal([]byte(rawJSON), &a); err != nil {
		t.Fatalf("Unmarshal raw hex: %v", err)
	}
	if a.Hex() != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("unexpected address: %s", a.Hex())
	}
}

func TestAddress_JSON_UnmarshalBech32(t *testing.T) {
	oldHRP := activeHRP
	defer func() { activeHRP = oldHRP }()

	SetAddressHRP(MainnetHRP)

	// Create a known address and get its bech32 encoding.
	original := Address{0x01, 0x02, 0x03}
	bech32Str := original.String()

	jsonStr := `"` + bech32Str + `"`
	var decoded Address
	if err := json.Unmarshal([]byte(jsonStr), &decoded); err != nil {
		t.Fatalf("Unmarshal bech32: %v", err)
	}
	if decoded != original {
		t.Errorf("decoded=%x, want=%x", decoded, original)
	}
}

func TestSetAddressHRP(t *testing.T) {
	oldHRP := activeHRP
	defer func() { activeHRP = oldHRP }()

	SetAddressHRP(TestnetHRP)
	if GetAddressHRP() != TestnetHRP {
		t.Errorf("GetAddressHRP() = %s, want %s", GetAddressHRP(), TestnetHRP)
	}

	SetAddressHRP(MainnetHRP)
	if GetAddressHRP() != MainnetHRP {
		t.Errorf("GetAddressHRP() = %s, want %s", GetAddressHRP(), MainnetHRP)
	}
}

func TestSetAddressPrefix_Compat(t *testing.T) {
	oldHRP := activeHRP
	defer func() { activeHRP = oldHRP }()

	// Deprecated SetAddressPrefix should still work via the compat shim.
	SetAddressPrefix(TestnetPrefix)
	if GetAddressHRP() != TestnetHRP {
		t.Errorf("after SetAddressPrefix(testnet): GetAddressHRP() = %s, want %s", GetAddressHRP(), TestnetHRP)
	}

	SetAddressPrefix(MainnetPrefix)
	if GetAddressHRP() != MainnetHRP {
		t.Errorf("after SetAddressPrefix(mainnet): GetAddressHRP() = %s, want %s", GetAddressHRP(), MainnetHRP)
	}
}

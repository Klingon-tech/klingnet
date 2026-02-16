package types

import (
	"strings"
	"testing"
)

func TestHash_IsZero(t *testing.T) {
	var zero Hash
	if !zero.IsZero() {
		t.Error("zero-value Hash should be zero")
	}

	nonZero := Hash{0x01}
	if nonZero.IsZero() {
		t.Error("non-zero Hash should not be zero")
	}
}

func TestHash_String(t *testing.T) {
	var h Hash
	s := h.String()
	if len(s) != 64 {
		t.Errorf("String() length = %d, want 64", len(s))
	}
	if s != strings.Repeat("0", 64) {
		t.Errorf("zero hash String() = %s, want all zeros", s)
	}

	h[0] = 0xab
	h[31] = 0xcd
	s = h.String()
	if !strings.HasPrefix(s, "ab") {
		t.Errorf("String() should start with 'ab', got %s", s[:2])
	}
	if !strings.HasSuffix(s, "cd") {
		t.Errorf("String() should end with 'cd', got %s", s[62:])
	}
}

func TestHash_Bytes(t *testing.T) {
	h := Hash{0x01, 0x02, 0x03}
	b := h.Bytes()

	if len(b) != HashSize {
		t.Errorf("Bytes() length = %d, want %d", len(b), HashSize)
	}
	if b[0] != 0x01 || b[1] != 0x02 || b[2] != 0x03 {
		t.Errorf("Bytes() content mismatch")
	}

	// Ensure it's a copy, not a reference
	b[0] = 0xFF
	if h[0] == 0xFF {
		t.Error("Bytes() should return a copy, not a reference")
	}
}

func TestHexToHash(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:  "valid 64 hex chars",
			input: "af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262",
		},
		{
			name:  "all zeros",
			input: strings.Repeat("0", 64),
		},
		{
			name:    "too short",
			input:   "abcd",
			wantErr: true,
		},
		{
			name:    "too long",
			input:   strings.Repeat("a", 66),
			wantErr: true,
		},
		{
			name:    "invalid hex character",
			input:   strings.Repeat("g", 64),
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
			h, err := HexToHash(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("HexToHash(%q) should have returned error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("HexToHash(%q) unexpected error: %v", tt.input, err)
			}
			// Roundtrip check
			if h.String() != tt.input {
				t.Errorf("roundtrip: got %s, want %s", h.String(), tt.input)
			}
		})
	}
}

func TestChainID_IsZero(t *testing.T) {
	var zero ChainID
	if !zero.IsZero() {
		t.Error("zero-value ChainID should be zero")
	}

	nonZero := ChainID{0x01}
	if nonZero.IsZero() {
		t.Error("non-zero ChainID should not be zero")
	}
}

func TestChainID_String(t *testing.T) {
	c := ChainID{0xff}
	s := c.String()
	if !strings.HasPrefix(s, "ff") {
		t.Errorf("ChainID.String() = %s, expected to start with 'ff'", s)
	}
}

func TestTokenID_IsZero(t *testing.T) {
	var zero TokenID
	if !zero.IsZero() {
		t.Error("zero-value TokenID should be zero")
	}

	nonZero := TokenID{0x01}
	if nonZero.IsZero() {
		t.Error("non-zero TokenID should not be zero")
	}
}

func TestTokenID_String(t *testing.T) {
	tid := TokenID{0xde, 0xad}
	s := tid.String()
	if !strings.HasPrefix(s, "dead") {
		t.Errorf("TokenID.String() = %s, expected to start with 'dead'", s)
	}
}

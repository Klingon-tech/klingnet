package wallet

import (
	"strings"
	"testing"
)

func TestGenerateMnemonic(t *testing.T) {
	mnemonic, err := GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic() error: %v", err)
	}

	words := strings.Fields(mnemonic)
	if len(words) != 24 {
		t.Errorf("word count = %d, want 24", len(words))
	}
}

func TestGenerateMnemonic_Unique(t *testing.T) {
	m1, err := GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic() error: %v", err)
	}
	m2, err := GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic() error: %v", err)
	}

	if m1 == m2 {
		t.Error("two generated mnemonics should not be identical")
	}
}

func TestGenerateMnemonic_Valid(t *testing.T) {
	mnemonic, err := GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic() error: %v", err)
	}

	if !ValidateMnemonic(mnemonic) {
		t.Error("generated mnemonic should validate")
	}
}

func TestValidateMnemonic(t *testing.T) {
	tests := []struct {
		name     string
		mnemonic string
		valid    bool
	}{
		{
			name:     "valid 24-word BIP-39",
			mnemonic: "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art",
			valid:    true,
		},
		{
			name:     "valid 12-word BIP-39",
			mnemonic: "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
			valid:    true,
		},
		{
			name:     "empty string",
			mnemonic: "",
			valid:    false,
		},
		{
			name:     "random words",
			mnemonic: "not a valid mnemonic phrase at all",
			valid:    false,
		},
		{
			name:     "wrong checksum",
			mnemonic: "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon",
			valid:    false,
		},
		{
			name:     "single word",
			mnemonic: "abandon",
			valid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateMnemonic(tt.mnemonic); got != tt.valid {
				t.Errorf("ValidateMnemonic() = %v, want %v", got, tt.valid)
			}
		})
	}
}

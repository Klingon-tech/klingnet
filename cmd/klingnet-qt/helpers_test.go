package main

import (
	"testing"

	"github.com/Klingon-tech/klingnet-chain/config"
)

func TestFormatAmount(t *testing.T) {
	tests := []struct {
		name  string
		input uint64
		want  string
	}{
		{"zero", 0, "0.000000000000"},
		{"one micro", config.MicroCoin, "0.000001000000"},
		{"one milli", config.MilliCoin, "0.001000000000"},
		{"one coin", config.Coin, "1.000000000000"},
		{"fractional", 1_500_000_000_000, "1.500000000000"},
		{"large", 2_000_000 * config.Coin, "2000000.000000000000"},
		{"small frac", 1, "0.000000000001"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAmount(tt.input)
			if got != tt.want {
				t.Errorf("formatAmount(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseAmount(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    uint64
		wantErr bool
	}{
		{"zero", "0", 0, false},
		{"one coin", "1", config.Coin, false},
		{"fractional", "1.5", 1_500_000_000_000, false},
		{"small frac", "0.000001", config.MicroCoin, false},
		{"full precision", "0.000000000001", 1, false},
		{"trailing zeros", "1.000000000000", config.Coin, false},
		{"empty", "", 0, true},
		{"negative", "-1", 0, true},
		{"too many decimals", "1.0000000000001", 0, true},
		{"not a number", "abc", 0, true},
		{"bad fraction", "1.abc", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAmount(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseAmount(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseAmount(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatParseRoundtrip(t *testing.T) {
	amounts := []uint64{0, 1, config.MicroCoin, config.MilliCoin, config.Coin, 123456789012}
	for _, a := range amounts {
		s := formatAmount(a)
		got, err := parseAmount(s)
		if err != nil {
			t.Errorf("roundtrip(%d): format=%q, parse error: %v", a, s, err)
			continue
		}
		if got != a {
			t.Errorf("roundtrip(%d): format=%q, parse=%d", a, s, got)
		}
	}
}

func TestValidateAddress(t *testing.T) {
	// Valid 40-char hex address.
	valid := "0000000000000000000000000000000000000000"
	if _, err := validateAddress(valid); err != nil {
		t.Errorf("validateAddress(%q) unexpected error: %v", valid, err)
	}

	// Valid bech32 address.
	addr, _ := validateAddress(valid)
	bech32Addr := addr.String()
	if _, err := validateAddress(bech32Addr); err != nil {
		t.Errorf("validateAddress(%q) unexpected error: %v", bech32Addr, err)
	}

	// Invalid.
	if _, err := validateAddress("xyz"); err == nil {
		t.Error("validateAddress(\"xyz\") expected error, got nil")
	}

	if _, err := validateAddress(""); err == nil {
		t.Error("validateAddress(\"\") expected error, got nil")
	}
}

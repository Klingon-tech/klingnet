package types

import (
	"bytes"
	"testing"
)

func TestBech32_Roundtrip(t *testing.T) {
	data := []byte{0x8f, 0x3a, 0x44, 0xb8, 0x05, 0x6c, 0xaf, 0xec, 0x36, 0x8d,
		0xea, 0x0c, 0xbe, 0x0a, 0xd1, 0xd9, 0xbc, 0x3f, 0x43, 0x05}

	encoded, err := Bech32Encode("kgx", data)
	if err != nil {
		t.Fatalf("Bech32Encode: %v", err)
	}

	hrp, decoded, err := Bech32Decode(encoded)
	if err != nil {
		t.Fatalf("Bech32Decode: %v", err)
	}

	if hrp != "kgx" {
		t.Errorf("HRP = %q, want %q", hrp, "kgx")
	}
	if !bytes.Equal(decoded, data) {
		t.Errorf("decoded = %x, want %x", decoded, data)
	}
}

func TestBech32_KnownVector(t *testing.T) {
	// Encode a known 20-byte input and verify deterministic output.
	data := make([]byte, 20)
	for i := range data {
		data[i] = byte(i)
	}

	encoded1, err := Bech32Encode("kgx", data)
	if err != nil {
		t.Fatalf("Bech32Encode: %v", err)
	}
	encoded2, err := Bech32Encode("kgx", data)
	if err != nil {
		t.Fatalf("Bech32Encode: %v", err)
	}
	if encoded1 != encoded2 {
		t.Errorf("non-deterministic: %q != %q", encoded1, encoded2)
	}

	// Verify it starts with the expected prefix.
	if encoded1[:4] != "kgx1" {
		t.Errorf("expected kgx1 prefix, got %q", encoded1[:4])
	}
}

func TestBech32Decode_InvalidChecksum(t *testing.T) {
	data := make([]byte, 20)
	encoded, err := Bech32Encode("kgx", data)
	if err != nil {
		t.Fatalf("Bech32Encode: %v", err)
	}

	// Corrupt last character.
	corrupted := encoded[:len(encoded)-1] + "q"
	if corrupted == encoded {
		corrupted = encoded[:len(encoded)-1] + "p"
	}

	_, _, err = Bech32Decode(corrupted)
	if err == nil {
		t.Error("expected error for invalid checksum")
	}
}

func TestBech32Decode_InvalidChars(t *testing.T) {
	_, _, err := Bech32Decode("kgx1b!!invalid")
	if err == nil {
		t.Error("expected error for invalid characters")
	}
}

func TestBech32Decode_MixedCase(t *testing.T) {
	data := make([]byte, 20)
	encoded, err := Bech32Encode("kgx", data)
	if err != nil {
		t.Fatalf("Bech32Encode: %v", err)
	}

	// Mix case: uppercase first data char.
	runes := []rune(encoded)
	for i := 4; i < len(runes); i++ {
		if runes[i] >= 'a' && runes[i] <= 'z' {
			runes[i] = runes[i] - 'a' + 'A'
			break
		}
	}
	mixed := string(runes)
	if mixed == encoded {
		t.Skip("could not create mixed-case variant")
	}

	_, _, err = Bech32Decode(mixed)
	if err == nil {
		t.Error("expected error for mixed case")
	}
}

func TestBech32Encode_EmptyHRP(t *testing.T) {
	_, err := Bech32Encode("", []byte{0x01})
	if err == nil {
		t.Error("expected error for empty HRP")
	}
}

func TestBech32Decode_Empty(t *testing.T) {
	_, _, err := Bech32Decode("")
	if err == nil {
		t.Error("expected error for empty string")
	}
}

func TestBech32_DifferentHRPs(t *testing.T) {
	data := []byte{0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0x00, 0x11,
		0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb}

	enc1, err := Bech32Encode("kgx", data)
	if err != nil {
		t.Fatalf("Bech32Encode kgx: %v", err)
	}
	enc2, err := Bech32Encode("tkgx", data)
	if err != nil {
		t.Fatalf("Bech32Encode tkgx: %v", err)
	}

	if enc1 == enc2 {
		t.Error("different HRPs should produce different encodings")
	}

	// Both should decode to the same data.
	hrp1, dec1, err := Bech32Decode(enc1)
	if err != nil {
		t.Fatalf("decode kgx: %v", err)
	}
	hrp2, dec2, err := Bech32Decode(enc2)
	if err != nil {
		t.Fatalf("decode tkgx: %v", err)
	}

	if hrp1 != "kgx" || hrp2 != "tkgx" {
		t.Errorf("hrps: got %q and %q", hrp1, hrp2)
	}
	if !bytes.Equal(dec1, data) || !bytes.Equal(dec2, data) {
		t.Error("decoded data mismatch")
	}
}

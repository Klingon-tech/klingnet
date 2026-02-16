package wallet

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestSeedFromMnemonic(t *testing.T) {
	// BIP-39 test vector: 24 words "abandon...art" with empty passphrase
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"

	seed, err := SeedFromMnemonic(mnemonic, "")
	if err != nil {
		t.Fatalf("SeedFromMnemonic() error: %v", err)
	}

	if len(seed) != SeedSize {
		t.Errorf("seed length = %d, want %d", len(seed), SeedSize)
	}

	// Seed should not be all zeros
	allZero := true
	for _, b := range seed {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("seed should not be all zeros")
	}
}

func TestSeedFromMnemonic_KnownVector(t *testing.T) {
	// Standard BIP-39 test vector
	// Mnemonic: "abandon" x11 + "about", passphrase: "TREZOR"
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	passphrase := "TREZOR"

	seed, err := SeedFromMnemonic(mnemonic, passphrase)
	if err != nil {
		t.Fatalf("SeedFromMnemonic() error: %v", err)
	}

	// Known BIP-39 test vector result
	want, _ := hex.DecodeString("c55257c360c07c72029aebc1b53c05ed0362ada38ead3e3e9efa3708e53495531f09a6987599d18264c1e1c92f2cf141630c7a3c4ab7c81b2f001698e7463b04")
	if !bytes.Equal(seed, want) {
		t.Errorf("seed = %x, want %x", seed, want)
	}
}

func TestSeedFromMnemonic_PassphraseChanges(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

	seed1, err := SeedFromMnemonic(mnemonic, "")
	if err != nil {
		t.Fatalf("SeedFromMnemonic() error: %v", err)
	}

	seed2, err := SeedFromMnemonic(mnemonic, "my passphrase")
	if err != nil {
		t.Fatalf("SeedFromMnemonic() error: %v", err)
	}

	if bytes.Equal(seed1, seed2) {
		t.Error("different passphrases should produce different seeds")
	}
}

func TestSeedFromMnemonic_Deterministic(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

	seed1, err := SeedFromMnemonic(mnemonic, "test")
	if err != nil {
		t.Fatalf("SeedFromMnemonic() error: %v", err)
	}

	seed2, err := SeedFromMnemonic(mnemonic, "test")
	if err != nil {
		t.Fatalf("SeedFromMnemonic() error: %v", err)
	}

	if !bytes.Equal(seed1, seed2) {
		t.Error("same mnemonic + passphrase should produce same seed")
	}
}

func TestSeedFromMnemonic_InvalidMnemonic(t *testing.T) {
	_, err := SeedFromMnemonic("not valid words here", "")
	if err == nil {
		t.Error("should reject invalid mnemonic")
	}
}

func TestSeedFromMnemonic_EmptyMnemonic(t *testing.T) {
	_, err := SeedFromMnemonic("", "")
	if err == nil {
		t.Error("should reject empty mnemonic")
	}
}

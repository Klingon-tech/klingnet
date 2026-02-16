// Package wallet implements HD wallet functionality.
package wallet

import (
	"fmt"

	"github.com/tyler-smith/go-bip39"
)

// MnemonicEntropyBits is the entropy size for 24-word mnemonics.
const MnemonicEntropyBits = 256

// GenerateMnemonic creates a new 24-word BIP-39 mnemonic.
func GenerateMnemonic() (string, error) {
	entropy, err := bip39.NewEntropy(MnemonicEntropyBits)
	if err != nil {
		return "", fmt.Errorf("generate entropy: %w", err)
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return "", fmt.Errorf("generate mnemonic: %w", err)
	}
	return mnemonic, nil
}

// ValidateMnemonic checks if a mnemonic is valid per BIP-39
// (correct word count, valid words, valid checksum).
func ValidateMnemonic(mnemonic string) bool {
	return bip39.IsMnemonicValid(mnemonic)
}

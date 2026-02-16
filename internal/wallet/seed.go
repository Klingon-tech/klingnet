package wallet

import (
	"fmt"

	"github.com/tyler-smith/go-bip39"
)

// SeedSize is the length of a derived seed in bytes (512 bits).
const SeedSize = 64

// SeedFromMnemonic derives a 512-bit seed from a mnemonic and optional passphrase
// using PBKDF2-SHA512 as specified in BIP-39.
func SeedFromMnemonic(mnemonic, passphrase string) ([]byte, error) {
	if !ValidateMnemonic(mnemonic) {
		return nil, fmt.Errorf("invalid mnemonic")
	}
	seed, err := bip39.NewSeedWithErrorChecking(mnemonic, passphrase)
	if err != nil {
		return nil, fmt.Errorf("derive seed: %w", err)
	}
	return seed, nil
}

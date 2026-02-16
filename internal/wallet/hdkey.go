package wallet

import (
	"fmt"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
	"github.com/tyler-smith/go-bip32"
)

// BIP-44 derivation path constants.
// Full path: m/44'/CoinType'/account'/change/index
const (
	// PurposeBIP44 is the BIP-44 purpose field (hardened).
	PurposeBIP44 = bip32.FirstHardenedChild + 44

	// CoinTypeKlingnet is our registered (placeholder) coin type (hardened).
	// TODO: Register an actual coin type number.
	CoinTypeKlingnet = bip32.FirstHardenedChild + 8888

	// ChangeExternal is for receiving addresses.
	ChangeExternal = 0

	// ChangeInternal is for change addresses.
	ChangeInternal = 1
)

// HDKey represents a hierarchical deterministic key (BIP-32).
type HDKey struct {
	key *bip32.Key
}

// NewMasterKey creates a master HD key from a 64-byte seed.
func NewMasterKey(seed []byte) (*HDKey, error) {
	if len(seed) != SeedSize {
		return nil, fmt.Errorf("seed must be %d bytes, got %d", SeedSize, len(seed))
	}
	master, err := bip32.NewMasterKey(seed)
	if err != nil {
		return nil, fmt.Errorf("create master key: %w", err)
	}
	return &HDKey{key: master}, nil
}

// DeriveChild derives a child key at the given index.
// For hardened derivation, add bip32.FirstHardenedChild to the index.
func (k *HDKey) DeriveChild(index uint32) (*HDKey, error) {
	child, err := k.key.NewChildKey(index)
	if err != nil {
		return nil, fmt.Errorf("derive child %d: %w", index, err)
	}
	return &HDKey{key: child}, nil
}

// DerivePath derives a key along a sequence of indices.
func (k *HDKey) DerivePath(indices ...uint32) (*HDKey, error) {
	current := k
	for _, idx := range indices {
		child, err := current.DeriveChild(idx)
		if err != nil {
			return nil, err
		}
		current = child
	}
	return current, nil
}

// DeriveAddress derives the key at m/44'/8888'/account'/change/index.
func (k *HDKey) DeriveAddress(account, change, index uint32) (*HDKey, error) {
	return k.DerivePath(
		PurposeBIP44,
		CoinTypeKlingnet,
		bip32.FirstHardenedChild+account,
		change,
		index,
	)
}

// PrivateKeyBytes returns the raw 32-byte private key.
// Returns nil if this is a public-only key.
func (k *HDKey) PrivateKeyBytes() []byte {
	if !k.key.IsPrivate {
		return nil
	}
	// bip32 Key.Key is 33 bytes with a leading 0x00 for private keys.
	raw := k.key.Key
	if len(raw) == 33 && raw[0] == 0 {
		return raw[1:]
	}
	return raw
}

// PublicKeyBytes returns the compressed 33-byte public key.
func (k *HDKey) PublicKeyBytes() []byte {
	pub := k.key.PublicKey()
	return pub.Key
}

// Signer returns a crypto.Signer from this HD key's private key.
// Returns error if this is a public-only key.
func (k *HDKey) Signer() (*crypto.PrivateKey, error) {
	priv := k.PrivateKeyBytes()
	if priv == nil {
		return nil, fmt.Errorf("cannot create signer from public key")
	}
	return crypto.PrivateKeyFromBytes(priv)
}

// Address derives a Klingnet address from this key's public key.
// Address = first 20 bytes of BLAKE3(compressed_pubkey).
func (k *HDKey) Address() types.Address {
	pub := k.PublicKeyBytes()
	hash := crypto.Hash(pub)
	var addr types.Address
	copy(addr[:], hash[:types.AddressSize])
	return addr
}

// IsPrivate returns true if this key contains a private key.
func (k *HDKey) IsPrivate() bool {
	return k.key.IsPrivate
}

// Depth returns the derivation depth (0 for master).
func (k *HDKey) Depth() uint8 {
	return k.key.Depth
}

// Neuter returns a public-key-only copy (for watch-only wallets).
func (k *HDKey) Neuter() *HDKey {
	return &HDKey{key: k.key.PublicKey()}
}

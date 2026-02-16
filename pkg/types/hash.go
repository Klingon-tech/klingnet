// Package types defines core primitive types for the Klingnet blockchain.
package types

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// HashSize is the length of a hash in bytes.
const HashSize = 32

// Hash represents a 256-bit hash value.
type Hash [HashSize]byte

// ChainID uniquely identifies a chain (root or sub-chain).
type ChainID Hash

// TokenID identifies a token type, derived from issuance outpoint.
type TokenID Hash

// IsZero returns true if the hash is all zeros.
func (h Hash) IsZero() bool {
	return h == Hash{}
}

// String returns the hex-encoded hash.
func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

// Bytes returns a copy of the hash as a byte slice.
func (h Hash) Bytes() []byte {
	b := make([]byte, HashSize)
	copy(b, h[:])
	return b
}

// MarshalJSON encodes the hash as a hex string.
func (h Hash) MarshalJSON() ([]byte, error) {
	return json.Marshal(h.String())
}

// UnmarshalJSON decodes a hex string into a hash.
func (h *Hash) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		*h = Hash{}
		return nil
	}
	decoded, err := hex.DecodeString(s)
	if err != nil {
		return fmt.Errorf("invalid hash hex: %w", err)
	}
	if len(decoded) != HashSize {
		return fmt.Errorf("hash must be %d bytes, got %d", HashSize, len(decoded))
	}
	copy(h[:], decoded)
	return nil
}

// HexToHash converts a hex string to a Hash.
// Returns an error if the string is not exactly 64 hex characters.
func HexToHash(s string) (Hash, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return Hash{}, fmt.Errorf("invalid hex: %w", err)
	}
	if len(b) != HashSize {
		return Hash{}, fmt.Errorf("hash must be %d bytes, got %d", HashSize, len(b))
	}
	var h Hash
	copy(h[:], b)
	return h, nil
}

// IsZero returns true if the chain ID is all zeros.
func (c ChainID) IsZero() bool {
	return Hash(c).IsZero()
}

// String returns the hex-encoded chain ID.
func (c ChainID) String() string {
	return Hash(c).String()
}

// MarshalJSON encodes the chain ID as a hex string.
func (c ChainID) MarshalJSON() ([]byte, error) {
	return Hash(c).MarshalJSON()
}

// UnmarshalJSON decodes a hex string into a chain ID.
func (c *ChainID) UnmarshalJSON(data []byte) error {
	return (*Hash)(c).UnmarshalJSON(data)
}

// IsZero returns true if the token ID is all zeros.
func (t TokenID) IsZero() bool {
	return Hash(t).IsZero()
}

// String returns the hex-encoded token ID.
func (t TokenID) String() string {
	return Hash(t).String()
}

// MarshalJSON encodes the token ID as a hex string.
func (t TokenID) MarshalJSON() ([]byte, error) {
	return Hash(t).MarshalJSON()
}

// UnmarshalJSON decodes a hex string into a token ID.
func (t *TokenID) UnmarshalJSON(data []byte) error {
	return (*Hash)(t).UnmarshalJSON(data)
}

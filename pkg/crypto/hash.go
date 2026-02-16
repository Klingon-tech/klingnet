// Package crypto provides cryptographic primitives for Klingnet.
package crypto

import (
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
	"github.com/zeebo/blake3"
)

// Hash computes a BLAKE3-256 hash of the input data.
func Hash(data []byte) types.Hash {
	return blake3.Sum256(data)
}

// DoubleHash computes Hash(Hash(data)).
func DoubleHash(data []byte) types.Hash {
	first := Hash(data)
	return Hash(first[:])
}

// AddressFromPubKey derives an address from a compressed public key.
// Address = BLAKE3(compressed_pubkey)[:20].
func AddressFromPubKey(pubKey []byte) types.Address {
	h := Hash(pubKey)
	var addr types.Address
	copy(addr[:], h[:types.AddressSize])
	return addr
}

// HashConcat hashes the concatenation of two hashes.
// Used for building merkle trees.
func HashConcat(a, b types.Hash) types.Hash {
	var buf [64]byte
	copy(buf[:32], a[:])
	copy(buf[32:], b[:])
	return Hash(buf[:])
}

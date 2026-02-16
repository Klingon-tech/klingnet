package crypto

import (
	"fmt"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/schnorr"
)

// Signer signs messages with a private key using Schnorr/secp256k1.
type Signer interface {
	// Sign produces a Schnorr signature over a 32-byte hash.
	Sign(hash []byte) ([]byte, error)
	// PublicKey returns the compressed 33-byte public key.
	PublicKey() []byte
}

// Verifier verifies Schnorr/secp256k1 signatures.
type Verifier interface {
	// Verify checks a Schnorr signature against a hash and compressed public key.
	Verify(hash, signature, publicKey []byte) bool
}

// PrivateKey wraps a secp256k1 private key for Schnorr signing.
type PrivateKey struct {
	key *secp256k1.PrivateKey
}

// GenerateKey creates a new random secp256k1 private key.
func GenerateKey() (*PrivateKey, error) {
	key, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	return &PrivateKey{key: key}, nil
}

// PrivateKeyFromBytes creates a PrivateKey from a 32-byte secret.
func PrivateKeyFromBytes(b []byte) (*PrivateKey, error) {
	if len(b) != 32 {
		return nil, fmt.Errorf("private key must be 32 bytes, got %d", len(b))
	}
	key := secp256k1.PrivKeyFromBytes(b)
	return &PrivateKey{key: key}, nil
}

// Sign produces a Schnorr signature over a 32-byte hash.
func (pk *PrivateKey) Sign(hash []byte) ([]byte, error) {
	if len(hash) != 32 {
		return nil, fmt.Errorf("hash must be 32 bytes, got %d", len(hash))
	}
	sig, err := schnorr.Sign(pk.key, hash)
	if err != nil {
		return nil, fmt.Errorf("schnorr sign: %w", err)
	}
	return sig.Serialize(), nil
}

// PublicKey returns the compressed 33-byte public key.
func (pk *PrivateKey) PublicKey() []byte {
	return pk.key.PubKey().SerializeCompressed()
}

// Serialize returns the 32-byte private key scalar.
func (pk *PrivateKey) Serialize() []byte {
	return pk.key.Serialize()
}

// Zero securely zeroes the private key memory.
func (pk *PrivateKey) Zero() {
	pk.key.Zero()
}

// VerifySignature checks a Schnorr signature against a 32-byte hash
// and a compressed public key. Returns false on any error.
func VerifySignature(hash, signature, publicKey []byte) bool {
	pubKey, err := secp256k1.ParsePubKey(publicKey)
	if err != nil {
		return false
	}
	sig, err := schnorr.ParseSignature(signature)
	if err != nil {
		return false
	}
	return sig.Verify(hash, pubKey)
}

// SchnorrVerifier implements the Verifier interface.
type SchnorrVerifier struct{}

// Verify checks a Schnorr signature against a hash and compressed public key.
func (v SchnorrVerifier) Verify(hash, signature, publicKey []byte) bool {
	return VerifySignature(hash, signature, publicKey)
}

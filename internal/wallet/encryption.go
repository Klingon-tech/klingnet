package wallet

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// Encryption constants.
const (
	SaltSize = 32
	// Encrypted format: [salt(32)][memory(4)][iterations(4)][parallelism(1)][nonce(24)][ciphertext...]
	headerSize = SaltSize + 4 + 4 + 1
)

// EncryptionParams holds Argon2id parameters.
type EncryptionParams struct {
	Memory      uint32 // in KiB
	Iterations  uint32
	Parallelism uint8
}

// DefaultParams returns recommended Argon2id parameters.
func DefaultParams() EncryptionParams {
	return EncryptionParams{
		Memory:      64 * 1024, // 64 MB
		Iterations:  3,
		Parallelism: 4,
	}
}

// deriveKey uses Argon2id to derive a 32-byte encryption key from password and salt.
func deriveKey(password, salt []byte, params EncryptionParams) []byte {
	return argon2.IDKey(
		password,
		salt,
		params.Iterations,
		params.Memory,
		params.Parallelism,
		chacha20poly1305.KeySize,
	)
}

// Encrypt encrypts data with password using Argon2id + XChaCha20-Poly1305.
//
// Output format: salt(32) | memory(4) | iterations(4) | parallelism(1) | nonce(24) | ciphertext
func Encrypt(data, password []byte, params EncryptionParams) ([]byte, error) {
	// Generate random salt.
	salt := make([]byte, SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	// Derive encryption key.
	key := deriveKey(password, salt, params)

	// Create XChaCha20-Poly1305 AEAD.
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	// Generate random nonce.
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	// Encrypt.
	ciphertext := aead.Seal(nil, nonce, data, nil)

	// Build output: salt | params | nonce | ciphertext
	out := make([]byte, 0, headerSize+len(nonce)+len(ciphertext))
	out = append(out, salt...)
	out = binary.LittleEndian.AppendUint32(out, params.Memory)
	out = binary.LittleEndian.AppendUint32(out, params.Iterations)
	out = append(out, params.Parallelism)
	out = append(out, nonce...)
	out = append(out, ciphertext...)

	// Zero the derived key.
	for i := range key {
		key[i] = 0
	}

	return out, nil
}

// Decrypt decrypts data encrypted by Encrypt with the given password.
func Decrypt(encrypted, password []byte) ([]byte, error) {
	nonceSize := chacha20poly1305.NonceSizeX
	minSize := headerSize + nonceSize + chacha20poly1305.Overhead
	if len(encrypted) < minSize {
		return nil, fmt.Errorf("encrypted data too short: %d bytes, need at least %d", len(encrypted), minSize)
	}

	// Parse header.
	salt := encrypted[:SaltSize]
	memory := binary.LittleEndian.Uint32(encrypted[SaltSize:])
	iterations := binary.LittleEndian.Uint32(encrypted[SaltSize+4:])
	parallelism := encrypted[SaltSize+8]

	params := EncryptionParams{
		Memory:      memory,
		Iterations:  iterations,
		Parallelism: parallelism,
	}

	// Parse nonce and ciphertext.
	nonce := encrypted[headerSize : headerSize+nonceSize]
	ciphertext := encrypted[headerSize+nonceSize:]

	// Derive key.
	key := deriveKey(password, salt, params)

	// Create AEAD and decrypt.
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		// Zero the derived key.
		for i := range key {
			key[i] = 0
		}
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)

	// Zero the derived key.
	for i := range key {
		key[i] = 0
	}

	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}

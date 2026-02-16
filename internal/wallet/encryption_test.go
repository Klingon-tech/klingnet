package wallet

import (
	"bytes"
	"testing"
)

// fastParams returns low-cost Argon2 params for fast tests.
func fastParams() EncryptionParams {
	return EncryptionParams{
		Memory:      64, // 64 KiB (minimal)
		Iterations:  1,
		Parallelism: 1,
	}
}

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	plaintext := []byte("secret wallet data")
	password := []byte("strong-password-123")

	encrypted, err := Encrypt(plaintext, password, fastParams())
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	decrypted, err := Decrypt(encrypted, password)
	if err != nil {
		t.Fatalf("Decrypt() error: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptDecrypt_EmptyData(t *testing.T) {
	encrypted, err := Encrypt([]byte{}, []byte("pass"), fastParams())
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	decrypted, err := Decrypt(encrypted, []byte("pass"))
	if err != nil {
		t.Fatalf("Decrypt() error: %v", err)
	}

	if len(decrypted) != 0 {
		t.Errorf("decrypted empty data should be empty, got %d bytes", len(decrypted))
	}
}

func TestEncryptDecrypt_LargeData(t *testing.T) {
	plaintext := make([]byte, 10000)
	for i := range plaintext {
		plaintext[i] = byte(i % 256)
	}

	encrypted, err := Encrypt(plaintext, []byte("pass"), fastParams())
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	decrypted, err := Decrypt(encrypted, []byte("pass"))
	if err != nil {
		t.Fatalf("Decrypt() error: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Error("large data roundtrip failed")
	}
}

func TestDecrypt_WrongPassword(t *testing.T) {
	plaintext := []byte("secret data")

	encrypted, err := Encrypt(plaintext, []byte("correct"), fastParams())
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	_, err = Decrypt(encrypted, []byte("wrong"))
	if err == nil {
		t.Error("Decrypt with wrong password should fail")
	}
}

func TestDecrypt_TruncatedData(t *testing.T) {
	_, err := Decrypt([]byte("too short"), []byte("pass"))
	if err == nil {
		t.Error("Decrypt with truncated data should fail")
	}
}

func TestDecrypt_CorruptedCiphertext(t *testing.T) {
	encrypted, err := Encrypt([]byte("data"), []byte("pass"), fastParams())
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	// Corrupt the last byte (part of auth tag)
	encrypted[len(encrypted)-1] ^= 0xFF

	_, err = Decrypt(encrypted, []byte("pass"))
	if err == nil {
		t.Error("Decrypt with corrupted ciphertext should fail")
	}
}

func TestEncrypt_DifferentEachTime(t *testing.T) {
	plaintext := []byte("same data")
	password := []byte("same pass")

	enc1, err := Encrypt(plaintext, password, fastParams())
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}
	enc2, err := Encrypt(plaintext, password, fastParams())
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	if bytes.Equal(enc1, enc2) {
		t.Error("encrypting same data twice should produce different output (random salt/nonce)")
	}

	// Both should still decrypt correctly
	d1, _ := Decrypt(enc1, password)
	d2, _ := Decrypt(enc2, password)
	if !bytes.Equal(d1, plaintext) || !bytes.Equal(d2, plaintext) {
		t.Error("both encryptions should decrypt to same plaintext")
	}
}

func TestEncrypt_OutputFormat(t *testing.T) {
	plaintext := []byte("test")
	params := fastParams()

	encrypted, err := Encrypt(plaintext, []byte("pass"), params)
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	// Minimum size: header(41) + nonce(24) + ciphertext(len(plaintext) + 16 overhead)
	expectedMin := headerSize + 24 + len(plaintext) + 16
	if len(encrypted) < expectedMin {
		t.Errorf("encrypted length = %d, expected at least %d", len(encrypted), expectedMin)
	}
}

func TestDefaultParams(t *testing.T) {
	p := DefaultParams()
	if p.Memory != 64*1024 {
		t.Errorf("Memory = %d, want %d", p.Memory, 64*1024)
	}
	if p.Iterations != 3 {
		t.Errorf("Iterations = %d, want 3", p.Iterations)
	}
	if p.Parallelism != 4 {
		t.Errorf("Parallelism = %d, want 4", p.Parallelism)
	}
}

func TestEncryptDecrypt_WalletSeed(t *testing.T) {
	// Realistic scenario: encrypt a 64-byte seed
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	seed, err := SeedFromMnemonic(mnemonic, "")
	if err != nil {
		t.Fatalf("SeedFromMnemonic() error: %v", err)
	}

	password := []byte("wallet-password-2024!")
	encrypted, err := Encrypt(seed, password, fastParams())
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	decrypted, err := Decrypt(encrypted, password)
	if err != nil {
		t.Fatalf("Decrypt() error: %v", err)
	}

	if !bytes.Equal(decrypted, seed) {
		t.Error("decrypted seed does not match original")
	}
}

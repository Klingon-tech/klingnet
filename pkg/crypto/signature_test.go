package crypto

import (
	"bytes"
	"testing"
)

func TestGenerateKey(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	pub := key.PublicKey()
	if len(pub) != 33 {
		t.Errorf("PublicKey() length = %d, want 33", len(pub))
	}

	ser := key.Serialize()
	if len(ser) != 32 {
		t.Errorf("Serialize() length = %d, want 32", len(ser))
	}
}

func TestGenerateKey_Unique(t *testing.T) {
	k1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	k2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	if bytes.Equal(k1.Serialize(), k2.Serialize()) {
		t.Error("two generated keys should not be identical")
	}
}

func TestPrivateKeyFromBytes(t *testing.T) {
	original, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	restored, err := PrivateKeyFromBytes(original.Serialize())
	if err != nil {
		t.Fatalf("PrivateKeyFromBytes() error: %v", err)
	}

	if !bytes.Equal(original.PublicKey(), restored.PublicKey()) {
		t.Error("restored key should have same public key")
	}
}

func TestPrivateKeyFromBytes_InvalidLength(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"too short", make([]byte, 16)},
		{"too long", make([]byte, 64)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := PrivateKeyFromBytes(tt.data)
			if err == nil {
				t.Error("expected error for invalid key length")
			}
		})
	}
}

func TestSign_Verify(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	hash := Hash([]byte("test message"))
	sig, err := key.Sign(hash[:])
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	if len(sig) != 64 {
		t.Errorf("signature length = %d, want 64", len(sig))
	}

	if !VerifySignature(hash[:], sig, key.PublicKey()) {
		t.Error("signature should verify against the correct key and hash")
	}
}

func TestSign_Deterministic(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	hash := Hash([]byte("deterministic test"))
	sig1, err := key.Sign(hash[:])
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}
	sig2, err := key.Sign(hash[:])
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	if !bytes.Equal(sig1, sig2) {
		t.Error("Schnorr signatures should be deterministic (same key + same hash = same sig)")
	}
}

func TestSign_InvalidHashLength(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	_, err = key.Sign([]byte("too short"))
	if err == nil {
		t.Error("Sign() should reject non-32-byte hash")
	}
}

func TestVerify_WrongHash(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	hash := Hash([]byte("message"))
	sig, err := key.Sign(hash[:])
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	wrongHash := Hash([]byte("different message"))
	if VerifySignature(wrongHash[:], sig, key.PublicKey()) {
		t.Error("signature should not verify with wrong hash")
	}
}

func TestVerify_WrongKey(t *testing.T) {
	key1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	key2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	hash := Hash([]byte("message"))
	sig, err := key1.Sign(hash[:])
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	if VerifySignature(hash[:], sig, key2.PublicKey()) {
		t.Error("signature should not verify with wrong public key")
	}
}

func TestVerify_CorruptedSignature(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	hash := Hash([]byte("message"))
	sig, err := key.Sign(hash[:])
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	// Flip a bit
	corrupted := make([]byte, len(sig))
	copy(corrupted, sig)
	corrupted[0] ^= 0x01

	if VerifySignature(hash[:], corrupted, key.PublicKey()) {
		t.Error("corrupted signature should not verify")
	}
}

func TestVerify_InvalidInputs(t *testing.T) {
	tests := []struct {
		name      string
		hash      []byte
		signature []byte
		publicKey []byte
	}{
		{"nil hash", nil, make([]byte, 64), make([]byte, 33)},
		{"empty signature", make([]byte, 32), nil, make([]byte, 33)},
		{"empty public key", make([]byte, 32), make([]byte, 64), nil},
		{"short signature", make([]byte, 32), make([]byte, 10), make([]byte, 33)},
		{"garbage public key", make([]byte, 32), make([]byte, 64), []byte("bad")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic, just return false
			if VerifySignature(tt.hash, tt.signature, tt.publicKey) {
				t.Error("should return false for invalid inputs")
			}
		})
	}
}

func TestPrivateKey_Zero(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	// Verify key works before zeroing
	hash := Hash([]byte("test"))
	_, err = key.Sign(hash[:])
	if err != nil {
		t.Fatalf("Sign() should work before Zero(): %v", err)
	}

	// Zero the key
	key.Zero()

	// After zeroing, the serialized key should be all zeros
	ser := key.Serialize()
	allZero := true
	for _, b := range ser {
		if b != 0 {
			allZero = false
			break
		}
	}
	if !allZero {
		t.Error("Serialize() should return zeros after Zero()")
	}
}

func TestPrivateKey_SignVerify_Roundtrip(t *testing.T) {
	// Full roundtrip: generate -> serialize -> restore -> sign -> verify
	original, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	pubKey := original.PublicKey()
	privBytes := original.Serialize()

	restored, err := PrivateKeyFromBytes(privBytes)
	if err != nil {
		t.Fatalf("PrivateKeyFromBytes() error: %v", err)
	}

	hash := Hash([]byte("roundtrip test"))
	sig, err := restored.Sign(hash[:])
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	if !VerifySignature(hash[:], sig, pubKey) {
		t.Error("roundtrip: signature from restored key should verify with original pubkey")
	}
}

func TestSchnorrVerifier_Interface(t *testing.T) {
	// Verify SchnorrVerifier satisfies Verifier interface
	var v Verifier = SchnorrVerifier{}

	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	hash := Hash([]byte("interface test"))
	sig, err := key.Sign(hash[:])
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	if !v.Verify(hash[:], sig, key.PublicKey()) {
		t.Error("SchnorrVerifier should verify valid signature")
	}
}

func TestPrivateKey_SignerInterface(t *testing.T) {
	// Verify PrivateKey satisfies Signer interface
	var s Signer
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	s = key

	hash := Hash([]byte("signer interface test"))
	sig, err := s.Sign(hash[:])
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	if !VerifySignature(hash[:], sig, s.PublicKey()) {
		t.Error("Signer interface: signature should verify")
	}
}

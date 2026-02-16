package wallet

import (
	"bytes"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
)

// testSeed returns a deterministic seed for testing.
// Uses the BIP-39 test vector: "abandon" x11 + "about" with passphrase "TREZOR".
func testSeed(t *testing.T) []byte {
	t.Helper()
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	seed, err := SeedFromMnemonic(mnemonic, "TREZOR")
	if err != nil {
		t.Fatalf("SeedFromMnemonic() error: %v", err)
	}
	return seed
}

func TestNewMasterKey(t *testing.T) {
	seed := testSeed(t)
	master, err := NewMasterKey(seed)
	if err != nil {
		t.Fatalf("NewMasterKey() error: %v", err)
	}

	if !master.IsPrivate() {
		t.Error("master key should be private")
	}

	if master.Depth() != 0 {
		t.Errorf("master key depth = %d, want 0", master.Depth())
	}

	priv := master.PrivateKeyBytes()
	if len(priv) != 32 {
		t.Errorf("private key length = %d, want 32", len(priv))
	}

	pub := master.PublicKeyBytes()
	if len(pub) != 33 {
		t.Errorf("public key length = %d, want 33", len(pub))
	}
}

func TestNewMasterKey_InvalidSeedLength(t *testing.T) {
	tests := []struct {
		name string
		seed []byte
	}{
		{"empty", []byte{}},
		{"too short", make([]byte, 32)},
		{"too long", make([]byte, 128)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewMasterKey(tt.seed)
			if err == nil {
				t.Error("expected error for invalid seed length")
			}
		})
	}
}

func TestNewMasterKey_Deterministic(t *testing.T) {
	seed := testSeed(t)

	m1, err := NewMasterKey(seed)
	if err != nil {
		t.Fatalf("NewMasterKey() error: %v", err)
	}
	m2, err := NewMasterKey(seed)
	if err != nil {
		t.Fatalf("NewMasterKey() error: %v", err)
	}

	if !bytes.Equal(m1.PrivateKeyBytes(), m2.PrivateKeyBytes()) {
		t.Error("same seed should produce same master key")
	}
}

func TestDeriveChild(t *testing.T) {
	seed := testSeed(t)
	master, err := NewMasterKey(seed)
	if err != nil {
		t.Fatalf("NewMasterKey() error: %v", err)
	}

	child, err := master.DeriveChild(0)
	if err != nil {
		t.Fatalf("DeriveChild(0) error: %v", err)
	}

	if child.Depth() != 1 {
		t.Errorf("child depth = %d, want 1", child.Depth())
	}

	if !child.IsPrivate() {
		t.Error("child derived from private key should be private")
	}

	// Different index produces different key
	child2, err := master.DeriveChild(1)
	if err != nil {
		t.Fatalf("DeriveChild(1) error: %v", err)
	}

	if bytes.Equal(child.PrivateKeyBytes(), child2.PrivateKeyBytes()) {
		t.Error("different indices should produce different keys")
	}
}

func TestDeriveChild_Deterministic(t *testing.T) {
	seed := testSeed(t)
	m1, _ := NewMasterKey(seed)
	m2, _ := NewMasterKey(seed)

	c1, _ := m1.DeriveChild(42)
	c2, _ := m2.DeriveChild(42)

	if !bytes.Equal(c1.PrivateKeyBytes(), c2.PrivateKeyBytes()) {
		t.Error("same seed + same index should produce same child")
	}
}

func TestDerivePath(t *testing.T) {
	seed := testSeed(t)
	master, _ := NewMasterKey(seed)

	// Derive step by step
	c1, _ := master.DeriveChild(PurposeBIP44)
	c2, _ := c1.DeriveChild(CoinTypeKlingnet)

	// Derive in one call
	combined, err := master.DerivePath(PurposeBIP44, CoinTypeKlingnet)
	if err != nil {
		t.Fatalf("DerivePath() error: %v", err)
	}

	if !bytes.Equal(c2.PrivateKeyBytes(), combined.PrivateKeyBytes()) {
		t.Error("DerivePath should equal sequential DeriveChild")
	}
}

func TestDeriveAddress(t *testing.T) {
	seed := testSeed(t)
	master, _ := NewMasterKey(seed)

	key, err := master.DeriveAddress(0, ChangeExternal, 0)
	if err != nil {
		t.Fatalf("DeriveAddress() error: %v", err)
	}

	// Depth should be 5: m / purpose' / coin' / account' / change / index
	if key.Depth() != 5 {
		t.Errorf("address key depth = %d, want 5", key.Depth())
	}

	if !key.IsPrivate() {
		t.Error("derived address key should be private")
	}

	// Different account produces different address
	key2, err := master.DeriveAddress(1, ChangeExternal, 0)
	if err != nil {
		t.Fatalf("DeriveAddress() error: %v", err)
	}

	if bytes.Equal(key.PrivateKeyBytes(), key2.PrivateKeyBytes()) {
		t.Error("different accounts should produce different keys")
	}

	// Change vs external should differ
	keyChange, err := master.DeriveAddress(0, ChangeInternal, 0)
	if err != nil {
		t.Fatalf("DeriveAddress() error: %v", err)
	}

	if bytes.Equal(key.PrivateKeyBytes(), keyChange.PrivateKeyBytes()) {
		t.Error("external and change keys should differ")
	}
}

func TestAddress(t *testing.T) {
	seed := testSeed(t)
	master, _ := NewMasterKey(seed)
	key, _ := master.DeriveAddress(0, ChangeExternal, 0)

	addr := key.Address()
	if addr.IsZero() {
		t.Error("derived address should not be zero")
	}

	// Deterministic
	addr2 := key.Address()
	if addr != addr2 {
		t.Error("Address() should be deterministic")
	}
}

func TestNeuter(t *testing.T) {
	seed := testSeed(t)
	master, _ := NewMasterKey(seed)

	pub := master.Neuter()

	if pub.IsPrivate() {
		t.Error("neutered key should not be private")
	}

	if pub.PrivateKeyBytes() != nil {
		t.Error("neutered key PrivateKeyBytes() should return nil")
	}

	// Public keys should match
	if !bytes.Equal(master.PublicKeyBytes(), pub.PublicKeyBytes()) {
		t.Error("neutered key should have same public key")
	}
}

func TestNeuter_DeriveChild(t *testing.T) {
	seed := testSeed(t)
	master, _ := NewMasterKey(seed)

	// Derive child from private key, then neuter
	privChild, _ := master.DeriveChild(0)
	neuteredChild := privChild.Neuter()

	// Derive child from neutered parent
	neuteredParent := master.Neuter()
	pubChild, err := neuteredParent.DeriveChild(0)
	if err != nil {
		t.Fatalf("DeriveChild from public key error: %v", err)
	}

	// Both should produce the same public key (BIP-32 property)
	if !bytes.Equal(neuteredChild.PublicKeyBytes(), pubChild.PublicKeyBytes()) {
		t.Error("public derivation should match neutered private derivation")
	}
}

func TestSigner(t *testing.T) {
	seed := testSeed(t)
	master, _ := NewMasterKey(seed)
	key, _ := master.DeriveAddress(0, ChangeExternal, 0)

	signer, err := key.Signer()
	if err != nil {
		t.Fatalf("Signer() error: %v", err)
	}

	hash := crypto.Hash([]byte("test message"))
	sig, err := signer.Sign(hash[:])
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	if !crypto.VerifySignature(hash[:], sig, signer.PublicKey()) {
		t.Error("signature from HD-derived key should verify")
	}
}

func TestSigner_PublicKeyOnly(t *testing.T) {
	seed := testSeed(t)
	master, _ := NewMasterKey(seed)
	pub := master.Neuter()

	_, err := pub.Signer()
	if err == nil {
		t.Error("Signer() from public key should return error")
	}
}

func TestFullWalletFlow(t *testing.T) {
	// Generate mnemonic -> seed -> master -> derive address -> sign -> verify
	mnemonic, err := GenerateMnemonic()
	if err != nil {
		t.Fatalf("GenerateMnemonic() error: %v", err)
	}

	seed, err := SeedFromMnemonic(mnemonic, "")
	if err != nil {
		t.Fatalf("SeedFromMnemonic() error: %v", err)
	}

	master, err := NewMasterKey(seed)
	if err != nil {
		t.Fatalf("NewMasterKey() error: %v", err)
	}

	key, err := master.DeriveAddress(0, ChangeExternal, 0)
	if err != nil {
		t.Fatalf("DeriveAddress() error: %v", err)
	}

	addr := key.Address()
	if addr.IsZero() {
		t.Error("derived address should not be zero")
	}

	signer, err := key.Signer()
	if err != nil {
		t.Fatalf("Signer() error: %v", err)
	}

	hash := crypto.Hash([]byte("transaction data"))
	sig, err := signer.Sign(hash[:])
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	if !crypto.VerifySignature(hash[:], sig, signer.PublicKey()) {
		t.Error("full wallet flow: signature should verify")
	}
}

package wallet

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func testKeystore(t *testing.T) *Keystore {
	t.Helper()
	dir := t.TempDir()
	ks, err := NewKeystore(dir)
	if err != nil {
		t.Fatalf("NewKeystore() error: %v", err)
	}
	return ks
}

func testSeedBytes(t *testing.T) []byte {
	t.Helper()
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	seed, err := SeedFromMnemonic(mnemonic, "")
	if err != nil {
		t.Fatalf("SeedFromMnemonic() error: %v", err)
	}
	return seed
}

func TestKeystore_CreateAndLoad(t *testing.T) {
	ks := testKeystore(t)
	seed := testSeedBytes(t)
	password := []byte("test-password")

	err := ks.Create("mywallet", seed, password, fastParams())
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	loaded, err := ks.Load("mywallet", password)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !bytes.Equal(loaded, seed) {
		t.Error("loaded seed does not match original")
	}
}

func TestKeystore_CreateDuplicate(t *testing.T) {
	ks := testKeystore(t)
	seed := testSeedBytes(t)

	err := ks.Create("dup", seed, []byte("pass"), fastParams())
	if err != nil {
		t.Fatalf("first Create() error: %v", err)
	}

	err = ks.Create("dup", seed, []byte("pass"), fastParams())
	if err == nil {
		t.Error("second Create() should fail for duplicate name")
	}
}

func TestKeystore_LoadWrongPassword(t *testing.T) {
	ks := testKeystore(t)
	seed := testSeedBytes(t)

	ks.Create("wallet", seed, []byte("correct"), fastParams())

	_, err := ks.Load("wallet", []byte("wrong"))
	if err == nil {
		t.Error("Load() with wrong password should fail")
	}
}

func TestKeystore_LoadNonexistent(t *testing.T) {
	ks := testKeystore(t)

	_, err := ks.Load("doesnotexist", []byte("pass"))
	if err == nil {
		t.Error("Load() for nonexistent wallet should fail")
	}
}

func TestKeystore_List(t *testing.T) {
	ks := testKeystore(t)
	seed := testSeedBytes(t)

	// Empty at first.
	names, err := ks.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected 0 wallets, got %d", len(names))
	}

	// Create two wallets.
	ks.Create("alpha", seed, []byte("p"), fastParams())
	ks.Create("beta", seed, []byte("p"), fastParams())

	names, err = ks.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(names) != 2 {
		t.Errorf("expected 2 wallets, got %d", len(names))
	}
}

func TestKeystore_Delete(t *testing.T) {
	ks := testKeystore(t)
	seed := testSeedBytes(t)

	ks.Create("todelete", seed, []byte("p"), fastParams())

	err := ks.Delete("todelete")
	if err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	// Should be gone.
	_, err = ks.Load("todelete", []byte("p"))
	if err == nil {
		t.Error("wallet should be deleted")
	}
}

func TestKeystore_DeleteNonexistent(t *testing.T) {
	ks := testKeystore(t)

	err := ks.Delete("ghost")
	if err == nil {
		t.Error("Delete() for nonexistent wallet should fail")
	}
}

func TestKeystore_AddAccount(t *testing.T) {
	ks := testKeystore(t)
	seed := testSeedBytes(t)

	ks.Create("wallet", seed, []byte("p"), fastParams())

	err := ks.AddAccount("wallet", AccountEntry{
		Index:   0,
		Name:    "default",
		Address: "abcdef0123456789abcdef0123456789abcdef01",
	})
	if err != nil {
		t.Fatalf("AddAccount() error: %v", err)
	}

	accounts, err := ks.ListAccounts("wallet")
	if err != nil {
		t.Fatalf("ListAccounts() error: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	if accounts[0].Name != "default" {
		t.Errorf("account name = %q, want %q", accounts[0].Name, "default")
	}
}

func TestKeystore_AddAccountDuplicateIndex(t *testing.T) {
	ks := testKeystore(t)
	seed := testSeedBytes(t)

	ks.Create("wallet", seed, []byte("p"), fastParams())

	ks.AddAccount("wallet", AccountEntry{Index: 0, Name: "first", Address: "aa"})

	err := ks.AddAccount("wallet", AccountEntry{Index: 0, Name: "second", Address: "bb"})
	if err == nil {
		t.Error("should reject duplicate account index")
	}
}

func TestKeystore_FilePermissions(t *testing.T) {
	ks := testKeystore(t)
	seed := testSeedBytes(t)

	ks.Create("secure", seed, []byte("p"), fastParams())

	path := filepath.Join(ks.path, "secure.wallet")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}

	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		t.Errorf("wallet file should be 0600, got %o", perm)
	}
}

func TestKeystore_ChangeIndex(t *testing.T) {
	ks := testKeystore(t)
	seed := testSeedBytes(t)

	ks.Create("wallet", seed, []byte("p"), fastParams())

	// Initially zero.
	idx, err := ks.GetChangeIndex("wallet")
	if err != nil {
		t.Fatalf("GetChangeIndex: %v", err)
	}
	if idx != 0 {
		t.Errorf("initial change index = %d, want 0", idx)
	}

	// Increment.
	if err := ks.IncrementChangeIndex("wallet"); err != nil {
		t.Fatalf("IncrementChangeIndex: %v", err)
	}

	idx, _ = ks.GetChangeIndex("wallet")
	if idx != 1 {
		t.Errorf("after first increment: index = %d, want 1", idx)
	}

	// Increment again.
	ks.IncrementChangeIndex("wallet")
	idx, _ = ks.GetChangeIndex("wallet")
	if idx != 2 {
		t.Errorf("after second increment: index = %d, want 2", idx)
	}
}

func TestKeystore_ChangeIndex_Nonexistent(t *testing.T) {
	ks := testKeystore(t)

	_, err := ks.GetChangeIndex("ghost")
	if err == nil {
		t.Error("GetChangeIndex for nonexistent wallet should fail")
	}

	err = ks.IncrementChangeIndex("ghost")
	if err == nil {
		t.Error("IncrementChangeIndex for nonexistent wallet should fail")
	}
}

func TestKeystore_ExternalIndex(t *testing.T) {
	ks := testKeystore(t)
	seed := testSeedBytes(t)

	ks.Create("wallet", seed, []byte("p"), fastParams())

	// Initially zero.
	idx, err := ks.GetExternalIndex("wallet")
	if err != nil {
		t.Fatalf("GetExternalIndex: %v", err)
	}
	if idx != 0 {
		t.Errorf("initial external index = %d, want 0", idx)
	}

	// Increment.
	if err := ks.IncrementExternalIndex("wallet"); err != nil {
		t.Fatalf("IncrementExternalIndex: %v", err)
	}

	idx, _ = ks.GetExternalIndex("wallet")
	if idx != 1 {
		t.Errorf("after first increment: index = %d, want 1", idx)
	}

	// Increment again.
	ks.IncrementExternalIndex("wallet")
	idx, _ = ks.GetExternalIndex("wallet")
	if idx != 2 {
		t.Errorf("after second increment: index = %d, want 2", idx)
	}
}

func TestKeystore_ExternalIndex_Nonexistent(t *testing.T) {
	ks := testKeystore(t)

	_, err := ks.GetExternalIndex("ghost")
	if err == nil {
		t.Error("GetExternalIndex for nonexistent wallet should fail")
	}

	err = ks.IncrementExternalIndex("ghost")
	if err == nil {
		t.Error("IncrementExternalIndex for nonexistent wallet should fail")
	}
}

func TestKeystore_SetExternalIndex(t *testing.T) {
	ks := testKeystore(t)
	seed := testSeedBytes(t)

	ks.Create("wallet", seed, []byte("p"), fastParams())

	// Set to 5.
	if err := ks.SetExternalIndex("wallet", 5); err != nil {
		t.Fatalf("SetExternalIndex: %v", err)
	}
	idx, _ := ks.GetExternalIndex("wallet")
	if idx != 5 {
		t.Errorf("external index = %d, want 5", idx)
	}

	// Set to 0 (reset).
	if err := ks.SetExternalIndex("wallet", 0); err != nil {
		t.Fatalf("SetExternalIndex to 0: %v", err)
	}
	idx, _ = ks.GetExternalIndex("wallet")
	if idx != 0 {
		t.Errorf("external index = %d, want 0", idx)
	}

	// Nonexistent wallet.
	if err := ks.SetExternalIndex("ghost", 1); err == nil {
		t.Error("SetExternalIndex for nonexistent wallet should fail")
	}
}

func TestKeystore_SetChangeIndex(t *testing.T) {
	ks := testKeystore(t)
	seed := testSeedBytes(t)

	ks.Create("wallet", seed, []byte("p"), fastParams())

	// Set to 3.
	if err := ks.SetChangeIndex("wallet", 3); err != nil {
		t.Fatalf("SetChangeIndex: %v", err)
	}
	idx, _ := ks.GetChangeIndex("wallet")
	if idx != 3 {
		t.Errorf("change index = %d, want 3", idx)
	}

	// Set to 0 (reset).
	if err := ks.SetChangeIndex("wallet", 0); err != nil {
		t.Fatalf("SetChangeIndex to 0: %v", err)
	}
	idx, _ = ks.GetChangeIndex("wallet")
	if idx != 0 {
		t.Errorf("change index = %d, want 0", idx)
	}

	// Nonexistent wallet.
	if err := ks.SetChangeIndex("ghost", 1); err == nil {
		t.Error("SetChangeIndex for nonexistent wallet should fail")
	}
}

func TestKeystore_ExternalIndex_IndependentOfChangeIndex(t *testing.T) {
	ks := testKeystore(t)
	seed := testSeedBytes(t)

	ks.Create("wallet", seed, []byte("p"), fastParams())

	// Increment change index.
	ks.IncrementChangeIndex("wallet")
	ks.IncrementChangeIndex("wallet")

	// External index should still be 0.
	extIdx, _ := ks.GetExternalIndex("wallet")
	if extIdx != 0 {
		t.Errorf("external index = %d, want 0 (should be independent of change index)", extIdx)
	}

	// Increment external index.
	ks.IncrementExternalIndex("wallet")

	// Change index should still be 2.
	changeIdx, _ := ks.GetChangeIndex("wallet")
	if changeIdx != 2 {
		t.Errorf("change index = %d, want 2 (should be independent of external index)", changeIdx)
	}
}

func TestKeystore_FullFlow(t *testing.T) {
	ks := testKeystore(t)
	password := []byte("strong-password")

	// Generate mnemonic and seed.
	mnemonic, _ := GenerateMnemonic()
	seed, _ := SeedFromMnemonic(mnemonic, "")

	// Create wallet.
	err := ks.Create("main", seed, password, fastParams())
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// Derive address and add account.
	master, _ := NewMasterKey(seed)
	key, _ := master.DeriveAddress(0, ChangeExternal, 0)
	addr := key.Address()

	err = ks.AddAccount("main", AccountEntry{
		Index:   0,
		Name:    "default",
		Address: addr.String(),
	})
	if err != nil {
		t.Fatalf("AddAccount() error: %v", err)
	}

	// Reload and verify seed matches.
	loaded, err := ks.Load("main", password)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !bytes.Equal(loaded, seed) {
		t.Error("loaded seed mismatch")
	}

	// Verify accounts persisted.
	accounts, _ := ks.ListAccounts("main")
	if len(accounts) != 1 || accounts[0].Address != addr.String() {
		t.Error("account not persisted correctly")
	}
}

package wallet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// keystoreFile is the on-disk JSON format for an encrypted wallet.
type keystoreFile struct {
	Version           int            `json:"version"`
	CreatedAt         time.Time      `json:"created_at"`
	EncryptedSeed     []byte         `json:"encrypted_seed"`
	Accounts          []AccountEntry `json:"accounts"`
	NextChangeIndex   uint32         `json:"next_change_index"`   // BIP-44 internal chain index.
	NextExternalIndex uint32         `json:"next_external_index"` // BIP-44 external chain index.
}

// AccountEntry stores metadata for a derived account.
type AccountEntry struct {
	Index   uint32 `json:"index"`
	Change  uint32 `json:"change"` // 0=external (deposit), 1=internal (change)
	Name    string `json:"name"`
	Address string `json:"address"` // hex-encoded
}

// Derivation returns the BIP-44 (change, index) pair for this account entry.
func (a AccountEntry) Derivation() (change uint32, index uint32) {
	return a.Change, a.Index
}

// Keystore manages encrypted key storage on disk.
type Keystore struct {
	path string
}

// NewKeystore creates a keystore that reads/writes to the given directory.
// The directory is created if it doesn't exist.
func NewKeystore(path string) (*Keystore, error) {
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, fmt.Errorf("create keystore dir: %w", err)
	}
	return &Keystore{path: path}, nil
}

// walletPath returns the file path for a wallet by name.
func (ks *Keystore) walletPath(name string) string {
	return filepath.Join(ks.path, name+".wallet")
}

// Create creates a new encrypted wallet file from a mnemonic seed.
func (ks *Keystore) Create(name string, seed, password []byte, params EncryptionParams) error {
	path := ks.walletPath(name)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("wallet %q already exists", name)
	}

	encrypted, err := Encrypt(seed, password, params)
	if err != nil {
		return fmt.Errorf("encrypt seed: %w", err)
	}

	kf := keystoreFile{
		Version:       1,
		CreatedAt:     time.Now().UTC(),
		EncryptedSeed: encrypted,
		Accounts:      []AccountEntry{},
	}

	return ks.writeFile(path, &kf)
}

// Load decrypts a wallet and returns the seed bytes.
func (ks *Keystore) Load(name string, password []byte) ([]byte, error) {
	kf, err := ks.readFile(ks.walletPath(name))
	if err != nil {
		return nil, err
	}

	seed, err := Decrypt(kf.EncryptedSeed, password)
	if err != nil {
		return nil, fmt.Errorf("decrypt wallet: %w", err)
	}

	return seed, nil
}

// AddAccount records a derived account in the wallet metadata.
func (ks *Keystore) AddAccount(walletName string, acct AccountEntry) error {
	path := ks.walletPath(walletName)
	kf, err := ks.readFile(path)
	if err != nil {
		return err
	}

	// Normalize to canonical storage (explicit change + raw index).
	change, index := acct.Derivation()
	acct.Change = change
	acct.Index = index

	// Check for duplicate derivation path or duplicate address.
	for _, existing := range kf.Accounts {
		exChange, exIndex := existing.Derivation()
		if exChange == acct.Change && exIndex == acct.Index {
			// Idempotent insert if metadata points to the same address.
			if existing.Address == acct.Address {
				return nil
			}
			return fmt.Errorf("account path change=%d index=%d already exists", acct.Change, acct.Index)
		}
		if existing.Address != "" && existing.Address == acct.Address {
			return nil
		}
	}

	kf.Accounts = append(kf.Accounts, acct)
	return ks.writeFile(path, kf)
}

// ListAccounts returns the account entries for a wallet.
func (ks *Keystore) ListAccounts(walletName string) ([]AccountEntry, error) {
	kf, err := ks.readFile(ks.walletPath(walletName))
	if err != nil {
		return nil, err
	}
	return kf.Accounts, nil
}

// List returns the names of all wallet files in the keystore.
func (ks *Keystore) List() ([]string, error) {
	entries, err := os.ReadDir(ks.path)
	if err != nil {
		return nil, fmt.Errorf("read keystore dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if ext := filepath.Ext(name); ext == ".wallet" {
			names = append(names, name[:len(name)-len(ext)])
		}
	}
	return names, nil
}

// GetChangeIndex returns the next change address index for a wallet.
func (ks *Keystore) GetChangeIndex(name string) (uint32, error) {
	kf, err := ks.readFile(ks.walletPath(name))
	if err != nil {
		return 0, err
	}
	return kf.NextChangeIndex, nil
}

// IncrementChangeIndex advances the change address index by 1.
func (ks *Keystore) IncrementChangeIndex(name string) error {
	path := ks.walletPath(name)
	kf, err := ks.readFile(path)
	if err != nil {
		return err
	}
	kf.NextChangeIndex++
	return ks.writeFile(path, kf)
}

// GetExternalIndex returns the next external address index for a wallet.
func (ks *Keystore) GetExternalIndex(name string) (uint32, error) {
	kf, err := ks.readFile(ks.walletPath(name))
	if err != nil {
		return 0, err
	}
	return kf.NextExternalIndex, nil
}

// IncrementExternalIndex advances the external address index by 1.
func (ks *Keystore) IncrementExternalIndex(name string) error {
	path := ks.walletPath(name)
	kf, err := ks.readFile(path)
	if err != nil {
		return err
	}
	kf.NextExternalIndex++
	return ks.writeFile(path, kf)
}

// SetExternalIndex sets the next external address index to the given value.
func (ks *Keystore) SetExternalIndex(name string, idx uint32) error {
	path := ks.walletPath(name)
	kf, err := ks.readFile(path)
	if err != nil {
		return err
	}
	kf.NextExternalIndex = idx
	return ks.writeFile(path, kf)
}

// SetChangeIndex sets the next change address index to the given value.
func (ks *Keystore) SetChangeIndex(name string, idx uint32) error {
	path := ks.walletPath(name)
	kf, err := ks.readFile(path)
	if err != nil {
		return err
	}
	kf.NextChangeIndex = idx
	return ks.writeFile(path, kf)
}

// Delete removes a wallet file.
func (ks *Keystore) Delete(name string) error {
	path := ks.walletPath(name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("wallet %q not found", name)
	}
	return os.Remove(path)
}

func (ks *Keystore) writeFile(path string, kf *keystoreFile) error {
	data, err := json.MarshalIndent(kf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal wallet: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write wallet: %w", err)
	}
	return nil
}

func (ks *Keystore) readFile(path string) (*keystoreFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read wallet: %w", err)
	}
	var kf keystoreFile
	if err := json.Unmarshal(data, &kf); err != nil {
		return nil, fmt.Errorf("parse wallet: %w", err)
	}
	if kf.Version != 1 {
		return nil, fmt.Errorf("unsupported wallet version: %d", kf.Version)
	}
	return &kf, nil
}

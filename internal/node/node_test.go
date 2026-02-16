package node

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	tests := []struct {
		input, want string
	}{
		{"~/foo/bar", filepath.Join(home, "foo/bar")},
		{"~/.klingnet/key", filepath.Join(home, ".klingnet/key")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"", ""},
	}
	for _, tt := range tests {
		got := expandHome(tt.input)
		if got != tt.want {
			t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLoadValidatorKey(t *testing.T) {
	// Generate a random key, write it hex-encoded to a temp file.
	privKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	keyHex := hex.EncodeToString(privKey.Serialize())

	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "validator.key")
	if err := os.WriteFile(keyPath, []byte(keyHex+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := loadValidatorKey(keyPath)
	if err != nil {
		t.Fatalf("loadValidatorKey: %v", err)
	}
	if hex.EncodeToString(loaded.Serialize()) != keyHex {
		t.Errorf("key mismatch: got %x, want %s", loaded.Serialize(), keyHex)
	}
	loaded.Zero()
}

func TestLoadValidatorKey_Missing(t *testing.T) {
	_, err := loadValidatorKey("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadValidatorKey_InvalidHex(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "bad.key")
	if err := os.WriteFile(keyPath, []byte("not-hex-data"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := loadValidatorKey(keyPath)
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestResolveCoinbase_FromString(t *testing.T) {
	// Use a hex address string (20 bytes = 40 hex chars, no "1" to avoid bech32 path).
	addrHex := "aabbccddee00aabbccddee00aabbccddee00aabb"
	addr, err := resolveCoinbase(addrHex, nil)
	if err != nil {
		t.Fatalf("resolveCoinbase: %v", err)
	}
	if addr[0] != 0xaa || addr[19] != 0xbb {
		t.Errorf("unexpected address: %x", addr)
	}
}

func TestResolveCoinbase_FromKey(t *testing.T) {
	privKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	defer privKey.Zero()

	addr, err := resolveCoinbase("", privKey)
	if err != nil {
		t.Fatalf("resolveCoinbase: %v", err)
	}
	expected := crypto.AddressFromPubKey(privKey.PublicKey())
	if addr != expected {
		t.Errorf("address mismatch: got %x, want %x", addr, expected)
	}
}

func TestResolveCoinbase_NoSource(t *testing.T) {
	_, err := resolveCoinbase("", nil)
	if err == nil {
		t.Fatal("expected error when no coinbase source")
	}
}

func TestCreateEngine_PoA(t *testing.T) {
	genesis := config.GenesisFor(config.Testnet)
	engine, err := createEngine(genesis)
	if err != nil {
		t.Fatalf("createEngine: %v", err)
	}
	if engine == nil {
		t.Fatal("engine is nil")
	}
}

func TestCreateEngine_UnsupportedType(t *testing.T) {
	genesis := &config.Genesis{
		Protocol: config.ProtocolConfig{
			Consensus: config.ConsensusRules{
				Type: "unknown",
			},
		},
	}
	_, err := createEngine(genesis)
	if err == nil {
		t.Fatal("expected error for unsupported consensus type")
	}
}

func TestIsPoW(t *testing.T) {
	genesis := config.GenesisFor(config.Testnet)
	engine, _ := createEngine(genesis)
	if isPoW(engine) {
		t.Error("testnet PoA engine should not be PoW")
	}
}

func TestNodeLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmpDir := t.TempDir()

	cfg := config.Default(config.Testnet)
	cfg.DataDir = tmpDir
	cfg.P2P.Port = 0 // Use random port to avoid conflicts.
	cfg.P2P.NoDiscover = true
	cfg.P2P.Seeds = nil
	cfg.RPC.Port = 0 // Use random port.
	cfg.Wallet.Enabled = true

	// Ensure data dirs exist.
	if err := config.EnsureDataDirs(cfg); err != nil {
		t.Fatalf("EnsureDataDirs: %v", err)
	}

	n, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if n.Height() != 0 {
		t.Errorf("expected height 0, got %d", n.Height())
	}

	if n.RPCAddr() == "" {
		t.Error("RPCAddr should not be empty")
	}

	if err := n.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Stop should not panic or error.
	n.Stop()
}

func TestLoadFromFile(t *testing.T) {
	tmpDir := t.TempDir()

	cfg, err := config.LoadFromFile(tmpDir, config.Testnet)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.Network != config.Testnet {
		t.Errorf("expected testnet, got %s", cfg.Network)
	}
	if cfg.DataDir != tmpDir {
		t.Errorf("expected datadir %s, got %s", tmpDir, cfg.DataDir)
	}

	// Verify default config file was created.
	confPath := filepath.Join(tmpDir, "klingnet.conf")
	if _, err := os.Stat(confPath); os.IsNotExist(err) {
		t.Error("config file should have been created")
	}
}

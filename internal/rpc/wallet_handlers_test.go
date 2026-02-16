package rpc

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/chain"
	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	klog "github.com/Klingon-tech/klingnet-chain/internal/log"
	"github.com/Klingon-tech/klingnet-chain/internal/mempool"
	"github.com/Klingon-tech/klingnet-chain/internal/miner"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/internal/subchain"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/internal/wallet"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// walletTestEnv holds components for wallet RPC tests.
type walletTestEnv struct {
	server        *Server
	chain         *chain.Chain
	utxoStore     *utxo.Store
	pool          *mempool.Pool
	genesis       *config.Genesis
	engine        *consensus.PoA
	validatorKey  *crypto.PrivateKey
	validatorAddr types.Address
	addrHex       string
	url           string
}

func setupWalletTestEnv(t *testing.T) *walletTestEnv {
	t.Helper()
	klog.Init("error", false, "")

	validatorKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	validatorPub := validatorKey.PublicKey()
	validatorAddr := crypto.AddressFromPubKey(validatorPub)
	pubHex := hex.EncodeToString(validatorPub)
	addrHex := validatorAddr.String()

	gen := &config.Genesis{
		ChainID:   "klingnet-test-wallet",
		ChainName: "Wallet Test",
		Timestamp: uint64(time.Now().Unix()),
		Alloc:     map[string]uint64{addrHex: 100_000 * config.Coin},
		Protocol: config.ProtocolConfig{
			Consensus: config.ConsensusRules{
				Type:        config.ConsensusPoA,
				BlockTime:   1,
				Validators:  []string{pubHex},
				BlockReward: config.MilliCoin,
				MaxSupply:   2_000_000 * config.Coin,
				MinFeeRate:  10,
			},
		},
	}

	db := storage.NewMemory()
	utxoStore := utxo.NewStore(db)

	validatorPubBytes, _ := hex.DecodeString(pubHex)
	engine, err := consensus.NewPoA([][]byte{validatorPubBytes})
	if err != nil {
		t.Fatalf("create poa: %v", err)
	}
	if err := engine.SetSigner(validatorKey); err != nil {
		t.Fatalf("set signer: %v", err)
	}

	ch, err := chain.New(types.ChainID{}, db, utxoStore, engine)
	if err != nil {
		t.Fatalf("create chain: %v", err)
	}
	if err := ch.InitFromGenesis(gen); err != nil {
		t.Fatalf("init genesis: %v", err)
	}

	adapter := miner.NewUTXOAdapter(utxoStore)
	pool := mempool.New(adapter, 1000)
	pool.SetMinFeeRate(gen.Protocol.Consensus.MinFeeRate)

	srv := New("127.0.0.1:0", ch, utxoStore, pool, nil, gen, engine)

	// Create a keystore in temp dir.
	ksDir := t.TempDir()
	ks, err := wallet.NewKeystore(ksDir)
	if err != nil {
		t.Fatalf("create keystore: %v", err)
	}
	srv.SetKeystore(ks)
	srv.SetWalletTxIndex(NewWalletTxIndex(db))

	if err := srv.Start(); err != nil {
		t.Fatalf("start rpc: %v", err)
	}
	t.Cleanup(func() { srv.Stop() })

	return &walletTestEnv{
		server:        srv,
		chain:         ch,
		utxoStore:     utxoStore,
		pool:          pool,
		genesis:       gen,
		engine:        engine,
		validatorKey:  validatorKey,
		validatorAddr: validatorAddr,
		addrHex:       addrHex,
		url:           fmt.Sprintf("http://%s/", srv.Addr()),
	}
}

// ── Wallet create ──────────────────────────────────────────────────────

func TestRPC_WalletCreate(t *testing.T) {
	env := setupWalletTestEnv(t)

	resp := rpcCall(t, env.url, "wallet_create", WalletCreateParam{
		Name:     "test",
		Password: "pass123",
	})
	if resp.Error != nil {
		t.Fatalf("wallet_create error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletCreateResult
	json.Unmarshal(data, &result)

	if result.Mnemonic == "" {
		t.Error("mnemonic should not be empty")
	}
	if result.Address == "" {
		t.Error("address should not be empty")
	}

	// Verify mnemonic is valid.
	if !wallet.ValidateMnemonic(result.Mnemonic) {
		t.Error("returned mnemonic should be valid")
	}
}

func TestRPC_WalletCreate_DuplicateName(t *testing.T) {
	env := setupWalletTestEnv(t)

	// Create first.
	resp := rpcCall(t, env.url, "wallet_create", WalletCreateParam{
		Name: "dup", Password: "pass",
	})
	if resp.Error != nil {
		t.Fatalf("first create: %s", resp.Error.Message)
	}

	// Create second with same name.
	resp2 := rpcCall(t, env.url, "wallet_create", WalletCreateParam{
		Name: "dup", Password: "pass",
	})
	if resp2.Error == nil {
		t.Fatal("expected error for duplicate wallet name")
	}
}

// ── Wallet import ──────────────────────────────────────────────────────

func TestRPC_WalletImport(t *testing.T) {
	env := setupWalletTestEnv(t)

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	resp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name:     "imported",
		Password: "pass",
		Mnemonic: mnemonic,
	})
	if resp.Error != nil {
		t.Fatalf("wallet_import error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletImportResult
	json.Unmarshal(data, &result)

	if result.Address == "" {
		t.Error("address should not be empty")
	}
}

func TestRPC_WalletImport_AddressDiscovery(t *testing.T) {
	env := setupWalletTestEnv(t)

	// Derive addresses from the known mnemonic for ext index 0, 2 and change index 0.
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	seed, _ := wallet.SeedFromMnemonic(mnemonic, "")
	master, _ := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}

	ext0Key, _ := master.DeriveAddress(0, wallet.ChangeExternal, 0)
	ext0Addr := ext0Key.Address()

	ext2Key, _ := master.DeriveAddress(0, wallet.ChangeExternal, 2)
	ext2Addr := ext2Key.Address()

	chg0Key, _ := master.DeriveAddress(0, wallet.ChangeInternal, 0)
	chg0Addr := chg0Key.Address()

	// Fund ext index 0, ext index 2, and change index 0 with UTXOs.
	for i, addr := range []types.Address{ext0Addr, ext2Addr, chg0Addr} {
		op := types.Outpoint{Index: uint32(i)}
		copy(op.TxID[:], fmt.Appendf(nil, "disc-test-utxo-%d-000000000000000", i))
		if err := env.utxoStore.Put(&utxo.UTXO{
			Outpoint: op,
			Value:    1 * config.Coin,
			Script:   types.Script{Type: types.ScriptTypeP2PKH, Data: addr.Bytes()},
		}); err != nil {
			t.Fatalf("put utxo %d: %v", i, err)
		}
	}

	// Import the wallet — scanWalletAddresses should discover all 3 addresses.
	resp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name:     "disc-test",
		Password: "pass",
		Mnemonic: mnemonic,
	})
	if resp.Error != nil {
		t.Fatalf("wallet_import error: %s", resp.Error.Message)
	}

	// List accounts and verify all 3 are present.
	accounts, err := env.server.keystore.ListAccounts("disc-test")
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}

	// Build a set of discovered account keys (index + change).
	type acctKey struct {
		Index  uint32
		Change uint32
	}
	found := make(map[acctKey]bool)
	for _, a := range accounts {
		found[acctKey{a.Index, a.Change}] = true
	}

	// Expect: ext 0 (index=0,change=0), ext 2 (index=2,change=0), change 0 (index=0,change=1).
	for _, want := range []acctKey{
		{0, wallet.ChangeExternal},
		{2, wallet.ChangeExternal},
		{0, wallet.ChangeInternal},
	} {
		if !found[want] {
			t.Errorf("account (index=%d, change=%d) not discovered; found: %v", want.Index, want.Change, found)
		}
	}

	// ext index 1 has no UTXOs, so should NOT be discovered.
	if found[acctKey{1, wallet.ChangeExternal}] {
		t.Error("ext index 1 should not be discovered (no UTXOs)")
	}

	// Verify NextExternalIndex is set to 3 (highest used ext = 2, so next = 3).
	extIdx, _ := env.server.keystore.GetExternalIndex("disc-test")
	if extIdx != 3 {
		t.Errorf("NextExternalIndex = %d, want 3", extIdx)
	}

	// Verify NextChangeIndex is set to 1 (highest used change = 0, so next = 1).
	chgIdx, _ := env.server.keystore.GetChangeIndex("disc-test")
	if chgIdx != 1 {
		t.Errorf("NextChangeIndex = %d, want 1", chgIdx)
	}
}

func TestRPC_WalletImport_InvalidMnemonic(t *testing.T) {
	env := setupWalletTestEnv(t)

	resp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name:     "bad",
		Password: "pass",
		Mnemonic: "not a valid mnemonic phrase at all",
	})
	if resp.Error == nil {
		t.Fatal("expected error for invalid mnemonic")
	}
}

// ── Wallet list ────────────────────────────────────────────────────────

func TestRPC_WalletList(t *testing.T) {
	env := setupWalletTestEnv(t)

	// Empty at first.
	resp := rpcCall(t, env.url, "wallet_list", nil)
	if resp.Error != nil {
		t.Fatalf("wallet_list error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletListResult
	json.Unmarshal(data, &result)

	if len(result.Wallets) != 0 {
		t.Errorf("expected 0 wallets, got %d", len(result.Wallets))
	}

	// Create one.
	rpcCall(t, env.url, "wallet_create", WalletCreateParam{Name: "w1", Password: "p"})

	// List again.
	resp2 := rpcCall(t, env.url, "wallet_list", nil)
	data2, _ := json.Marshal(resp2.Result)
	var result2 WalletListResult
	json.Unmarshal(data2, &result2)

	if len(result2.Wallets) != 1 {
		t.Errorf("expected 1 wallet, got %d", len(result2.Wallets))
	}
}

// ── Wallet new address ─────────────────────────────────────────────────

func TestRPC_WalletNewAddress(t *testing.T) {
	env := setupWalletTestEnv(t)

	// Create wallet first.
	rpcCall(t, env.url, "wallet_create", WalletCreateParam{Name: "addr-test", Password: "pass"})

	// Generate new address.
	resp := rpcCall(t, env.url, "wallet_newAddress", WalletNewAddressParam{
		Name: "addr-test", Password: "pass",
	})
	if resp.Error != nil {
		t.Fatalf("wallet_newAddress error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletAddressResult
	json.Unmarshal(data, &result)

	if result.Index != 1 {
		t.Errorf("index = %d, want 1 (first new address after default)", result.Index)
	}
	if result.Address == "" {
		t.Error("address should not be empty")
	}
}

func TestRPC_WalletNewAddress_WrongPassword(t *testing.T) {
	env := setupWalletTestEnv(t)

	rpcCall(t, env.url, "wallet_create", WalletCreateParam{Name: "pw-test", Password: "correct"})

	resp := rpcCall(t, env.url, "wallet_newAddress", WalletNewAddressParam{
		Name: "pw-test", Password: "wrong",
	})
	if resp.Error == nil {
		t.Fatal("expected error for wrong password")
	}
}

// ── Wallet list addresses ──────────────────────────────────────────────

func TestRPC_WalletListAddresses(t *testing.T) {
	env := setupWalletTestEnv(t)

	rpcCall(t, env.url, "wallet_create", WalletCreateParam{Name: "addrs", Password: "p"})

	resp := rpcCall(t, env.url, "wallet_listAddresses", WalletUnlockParam{
		Name: "addrs", Password: "p",
	})
	if resp.Error != nil {
		t.Fatalf("wallet_listAddresses error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletAddressListResult
	json.Unmarshal(data, &result)

	if len(result.Accounts) != 1 {
		t.Errorf("expected 1 account (default), got %d", len(result.Accounts))
	}
}

// ── Wallet send ────────────────────────────────────────────────────────

func TestRPC_WalletSend(t *testing.T) {
	env := setupWalletTestEnv(t)

	// Import a known mnemonic and pre-load UTXOs for the derived address.
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	importResp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "sender", Password: "pass", Mnemonic: mnemonic,
	})
	if importResp.Error != nil {
		t.Fatalf("import: %s", importResp.Error.Message)
	}

	var importResult WalletImportResult
	d, _ := json.Marshal(importResp.Result)
	json.Unmarshal(d, &importResult)

	// Pre-load UTXOs for this address.
	senderAddr, _ := types.ParseAddress(importResult.Address)

	// Create a fake UTXO (we need to put it directly in the UTXO store).
	fakeOutpoint := types.Outpoint{Index: 0}
	copy(fakeOutpoint.TxID[:], []byte("test-tx-for-send-00000000000000"))
	fakeUTXO := &utxo.UTXO{
		Outpoint: fakeOutpoint,
		Value:    10 * config.Coin,
		Script: types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: senderAddr.Bytes(),
		},
	}
	if err := env.utxoStore.Put(fakeUTXO); err != nil {
		t.Fatalf("put utxo: %v", err)
	}

	// Send transaction.
	resp := rpcCall(t, env.url, "wallet_send", WalletSendParam{
		Name:     "sender",
		Password: "pass",
		To:       env.addrHex,
		Amount:   1 * config.Coin,
	})
	if resp.Error != nil {
		t.Fatalf("wallet_send error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletSendResult
	json.Unmarshal(data, &result)

	if result.TxHash == "" {
		t.Error("tx_hash should not be empty")
	}

	// Verify tx is in mempool.
	if env.pool.Count() != 1 {
		t.Errorf("mempool count = %d, want 1", env.pool.Count())
	}
}

func TestRPC_WalletSend_InsufficientFunds(t *testing.T) {
	env := setupWalletTestEnv(t)

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "broke", Password: "pass", Mnemonic: mnemonic,
	})

	// No UTXOs loaded — should fail.
	resp := rpcCall(t, env.url, "wallet_send", WalletSendParam{
		Name:     "broke",
		Password: "pass",
		To:       env.addrHex,
		Amount:   1 * config.Coin,
	})
	if resp.Error == nil {
		t.Fatal("expected error for insufficient funds")
	}
}

// ── Wallet send many ─────────────────────────────────────────────────

func TestRPC_WalletSendMany(t *testing.T) {
	env := setupWalletTestEnv(t)

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	importResp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "sendmany-test", Password: "pass", Mnemonic: mnemonic,
	})
	if importResp.Error != nil {
		t.Fatalf("import: %s", importResp.Error.Message)
	}

	var importResult WalletImportResult
	d, _ := json.Marshal(importResp.Result)
	json.Unmarshal(d, &importResult)

	senderAddr, _ := types.ParseAddress(importResult.Address)

	fakeOutpoint := types.Outpoint{Index: 0}
	copy(fakeOutpoint.TxID[:], []byte("test-tx-for-sendmany-0000000000"))
	fakeUTXO := &utxo.UTXO{
		Outpoint: fakeOutpoint,
		Value:    20 * config.Coin,
		Script: types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: senderAddr.Bytes(),
		},
	}
	if err := env.utxoStore.Put(fakeUTXO); err != nil {
		t.Fatalf("put utxo: %v", err)
	}

	// Send to 2 recipients.
	resp := rpcCall(t, env.url, "wallet_sendMany", WalletSendManyParam{
		Name:     "sendmany-test",
		Password: "pass",
		Recipients: []Recipient{
			{To: env.addrHex, Amount: 1 * config.Coin},
			{To: env.addrHex, Amount: 2 * config.Coin},
		},
	})
	if resp.Error != nil {
		t.Fatalf("wallet_sendMany error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletSendManyResult
	json.Unmarshal(data, &result)

	if result.TxHash == "" {
		t.Error("tx_hash should not be empty")
	}
	if env.pool.Count() != 1 {
		t.Errorf("mempool count = %d, want 1", env.pool.Count())
	}
}

func TestRPC_WalletSendMany_InsufficientFunds(t *testing.T) {
	env := setupWalletTestEnv(t)

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "sendmany-broke", Password: "pass", Mnemonic: mnemonic,
	})

	resp := rpcCall(t, env.url, "wallet_sendMany", WalletSendManyParam{
		Name:     "sendmany-broke",
		Password: "pass",
		Recipients: []Recipient{
			{To: env.addrHex, Amount: 1 * config.Coin},
		},
	})
	if resp.Error == nil {
		t.Fatal("expected error for insufficient funds")
	}
}

func TestRPC_WalletSendMany_EmptyRecipients(t *testing.T) {
	env := setupWalletTestEnv(t)

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "sendmany-empty", Password: "pass", Mnemonic: mnemonic,
	})

	resp := rpcCall(t, env.url, "wallet_sendMany", WalletSendManyParam{
		Name:       "sendmany-empty",
		Password:   "pass",
		Recipients: []Recipient{},
	})
	if resp.Error == nil {
		t.Fatal("expected error for empty recipients")
	}
}

func TestRPC_WalletSendMany_InvalidAddress(t *testing.T) {
	env := setupWalletTestEnv(t)

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	importResp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "sendmany-badaddr", Password: "pass", Mnemonic: mnemonic,
	})
	if importResp.Error != nil {
		t.Fatalf("import: %s", importResp.Error.Message)
	}

	var importResult WalletImportResult
	d, _ := json.Marshal(importResp.Result)
	json.Unmarshal(d, &importResult)

	senderAddr, _ := types.ParseAddress(importResult.Address)
	fakeOutpoint := types.Outpoint{Index: 0}
	copy(fakeOutpoint.TxID[:], []byte("test-tx-for-sendmany-badaddr00"))
	if err := env.utxoStore.Put(&utxo.UTXO{
		Outpoint: fakeOutpoint,
		Value:    10 * config.Coin,
		Script:   types.Script{Type: types.ScriptTypeP2PKH, Data: senderAddr.Bytes()},
	}); err != nil {
		t.Fatalf("put utxo: %v", err)
	}

	resp := rpcCall(t, env.url, "wallet_sendMany", WalletSendManyParam{
		Name:     "sendmany-badaddr",
		Password: "pass",
		Recipients: []Recipient{
			{To: "not-a-valid-address", Amount: 1 * config.Coin},
		},
	})
	if resp.Error == nil {
		t.Fatal("expected error for invalid address")
	}
}

// ── Wallet export key ──────────────────────────────────────────────────

func TestRPC_WalletExportKey(t *testing.T) {
	env := setupWalletTestEnv(t)

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	importResp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "export-test", Password: "pass", Mnemonic: mnemonic,
	})
	if importResp.Error != nil {
		t.Fatalf("import: %s", importResp.Error.Message)
	}

	var importResult WalletImportResult
	d, _ := json.Marshal(importResp.Result)
	json.Unmarshal(d, &importResult)

	resp := rpcCall(t, env.url, "wallet_exportKey", WalletExportKeyParam{
		Name: "export-test", Password: "pass", Account: 0, Index: 0,
	})
	if resp.Error != nil {
		t.Fatalf("wallet_exportKey error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletExportKeyResult
	json.Unmarshal(data, &result)

	if result.PrivateKey == "" {
		t.Error("private_key should not be empty")
	}
	if result.PubKey == "" {
		t.Error("pubkey should not be empty")
	}
	if result.Address == "" {
		t.Error("address should not be empty")
	}

	// Verify private key is 32 bytes hex (64 chars).
	if len(result.PrivateKey) != 64 {
		t.Errorf("private key hex length = %d, want 64", len(result.PrivateKey))
	}

	// Verify address matches the imported wallet's address.
	if result.Address != importResult.Address {
		t.Errorf("address = %s, want %s", result.Address, importResult.Address)
	}

	// Verify private key can reconstruct the public key.
	privBytes, _ := hex.DecodeString(result.PrivateKey)
	privKey, err := crypto.PrivateKeyFromBytes(privBytes)
	if err != nil {
		t.Fatalf("reconstruct key: %v", err)
	}
	pubHex := hex.EncodeToString(privKey.PublicKey())
	if pubHex != result.PubKey {
		t.Errorf("reconstructed pubkey = %s, want %s", pubHex, result.PubKey)
	}
}

// ── Wallet mint token ───────────────────────────────────────────────────

func TestRPC_WalletMintToken(t *testing.T) {
	env := setupWalletTestEnv(t)

	// Import a known mnemonic and pre-load UTXOs.
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	importResp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "minter", Password: "pass", Mnemonic: mnemonic,
	})
	if importResp.Error != nil {
		t.Fatalf("import: %s", importResp.Error.Message)
	}

	var importResult WalletImportResult
	d, _ := json.Marshal(importResp.Result)
	json.Unmarshal(d, &importResult)

	senderAddr, _ := types.ParseAddress(importResult.Address)

	// Pre-load 100 KGX UTXO for this address (token creation fee is 50 KGX).
	fakeOutpoint := types.Outpoint{Index: 0}
	copy(fakeOutpoint.TxID[:], []byte("test-tx-for-mint-00000000000000"))
	fakeUTXO := &utxo.UTXO{
		Outpoint: fakeOutpoint,
		Value:    100 * config.Coin,
		Script: types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: senderAddr.Bytes(),
		},
	}
	if err := env.utxoStore.Put(fakeUTXO); err != nil {
		t.Fatalf("put utxo: %v", err)
	}

	// Mint token.
	resp := rpcCall(t, env.url, "wallet_mintToken", WalletMintTokenParam{
		Name:      "minter",
		Password:  "pass",
		TokenName: "Test Token",
		Symbol:    "TTK",
		Decimals:  8,
		Amount:    1_000_000,
	})
	if resp.Error != nil {
		t.Fatalf("wallet_mintToken error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletMintTokenResult
	json.Unmarshal(data, &result)

	if result.TxHash == "" {
		t.Error("tx_hash should not be empty")
	}
	if result.TokenID == "" {
		t.Error("token_id should not be empty")
	}

	// Verify token ID is 32 bytes hex (64 chars).
	if len(result.TokenID) != 64 {
		t.Errorf("token_id hex length = %d, want 64", len(result.TokenID))
	}

	// Verify tx is in mempool.
	if env.pool.Count() != 1 {
		t.Errorf("mempool count = %d, want 1", env.pool.Count())
	}
}

func TestRPC_WalletMintToken_InsufficientFunds(t *testing.T) {
	env := setupWalletTestEnv(t)

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "broke-minter", Password: "pass", Mnemonic: mnemonic,
	})

	// No UTXOs loaded — should fail.
	resp := rpcCall(t, env.url, "wallet_mintToken", WalletMintTokenParam{
		Name:      "broke-minter",
		Password:  "pass",
		TokenName: "Fail Token",
		Symbol:    "FAIL",
		Decimals:  8,
		Amount:    1000,
	})
	if resp.Error == nil {
		t.Fatal("expected error for insufficient funds")
	}
}

// ── Wallet stake ────────────────────────────────────────────────────────

func TestRPC_WalletStake(t *testing.T) {
	env := setupWalletTestEnv(t)

	// Import a known mnemonic and pre-load UTXOs.
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	importResp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "staker", Password: "pass", Mnemonic: mnemonic,
	})
	if importResp.Error != nil {
		t.Fatalf("import: %s", importResp.Error.Message)
	}

	var importResult WalletImportResult
	d, _ := json.Marshal(importResp.Result)
	json.Unmarshal(d, &importResult)

	senderAddr, _ := types.ParseAddress(importResult.Address)

	// Pre-load 2000 KGX UTXO for this address.
	fakeOutpoint := types.Outpoint{Index: 0}
	copy(fakeOutpoint.TxID[:], []byte("test-tx-for-stake-0000000000000"))
	fakeUTXO := &utxo.UTXO{
		Outpoint: fakeOutpoint,
		Value:    2000 * config.Coin,
		Script: types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: senderAddr.Bytes(),
		},
	}
	if err := env.utxoStore.Put(fakeUTXO); err != nil {
		t.Fatalf("put utxo: %v", err)
	}

	// Stake transaction (genesis min stake is 0 in test, so any amount works).
	resp := rpcCall(t, env.url, "wallet_stake", WalletStakeParam{
		Name:     "staker",
		Password: "pass",
		Amount:   1000 * config.Coin,
	})
	if resp.Error != nil {
		t.Fatalf("wallet_stake error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletStakeResult
	json.Unmarshal(data, &result)

	if result.TxHash == "" {
		t.Error("tx_hash should not be empty")
	}
	if result.PubKey == "" {
		t.Error("pubkey should not be empty")
	}

	// Verify pubkey is 33 bytes hex (66 chars).
	if len(result.PubKey) != 66 {
		t.Errorf("pubkey hex length = %d, want 66", len(result.PubKey))
	}

	// Verify tx is in mempool.
	if env.pool.Count() != 1 {
		t.Errorf("mempool count = %d, want 1", env.pool.Count())
	}
}

func TestRPC_WalletStake_InsufficientFunds(t *testing.T) {
	env := setupWalletTestEnv(t)

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "broke-staker", Password: "pass", Mnemonic: mnemonic,
	})

	// No UTXOs loaded — should fail.
	resp := rpcCall(t, env.url, "wallet_stake", WalletStakeParam{
		Name:     "broke-staker",
		Password: "pass",
		Amount:   1000 * config.Coin,
	})
	if resp.Error == nil {
		t.Fatal("expected error for insufficient funds")
	}
}

// ── Wallet disabled ────────────────────────────────────────────────────

func TestRPC_WalletDisabled(t *testing.T) {
	// Use the regular test env (no keystore set).
	env := setupTestEnv(t)

	methods := []struct {
		method string
		params interface{}
	}{
		{"wallet_create", WalletCreateParam{Name: "x", Password: "p"}},
		{"wallet_import", WalletImportParam{Name: "x", Password: "p", Mnemonic: "m"}},
		{"wallet_list", nil},
		{"wallet_newAddress", WalletNewAddressParam{Name: "x", Password: "p"}},
		{"wallet_listAddresses", WalletUnlockParam{Name: "x", Password: "p"}},
		{"wallet_send", WalletSendParam{Name: "x", Password: "p", To: "aa", Amount: 1}},
		{"wallet_exportKey", WalletExportKeyParam{Name: "x", Password: "p"}},
		{"wallet_stake", WalletStakeParam{Name: "x", Password: "p", Amount: 1}},
		{"wallet_mintToken", WalletMintTokenParam{Name: "x", Password: "p", TokenName: "T", Symbol: "T", Amount: 1}},
		{"wallet_createSubChain", WalletCreateSubChainParam{Name: "x", Password: "p", ChainName: "c", Symbol: "S", ConsensusType: "poa"}},
		{"subchain_send", SubChainSendParam{ChainID: "aa", Name: "x", Password: "p", To: "bb", Amount: 1}},
	}

	for _, tc := range methods {
		t.Run(tc.method, func(t *testing.T) {
			resp := rpcCall(t, env.url, tc.method, tc.params)
			if resp.Error == nil {
				t.Fatalf("%s: expected error when wallet is disabled", tc.method)
			}
			if resp.Error.Code != CodeInternalError {
				t.Errorf("%s: error code = %d, want %d", tc.method, resp.Error.Code, CodeInternalError)
			}
		})
	}
}

// ── Wallet create sub-chain ─────────────────────────────────────────────

// setupSubChainWalletEnv creates a wallet test env with SubChain rules enabled
// and a sub-chain manager wired to the RPC server.
func setupSubChainWalletEnv(t *testing.T) (*walletTestEnv, *subchain.Manager) {
	t.Helper()
	klog.Init("error", false, "")

	validatorKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	validatorPub := validatorKey.PublicKey()
	validatorAddr := crypto.AddressFromPubKey(validatorPub)
	pubHex := hex.EncodeToString(validatorPub)
	addrHex := validatorAddr.String()

	gen := &config.Genesis{
		ChainID:   "klingnet-test-subchain",
		ChainName: "SubChain Wallet Test",
		Timestamp: uint64(time.Now().Unix()),
		Alloc:     map[string]uint64{addrHex: 100_000 * config.Coin},
		Protocol: config.ProtocolConfig{
			Consensus: config.ConsensusRules{
				Type:        config.ConsensusPoA,
				BlockTime:   1,
				Validators:  []string{pubHex},
				BlockReward: config.MilliCoin,
				MaxSupply:   2_000_000 * config.Coin,
				MinFeeRate:  10,
			},
			SubChain: config.SubChainRules{
				Enabled:      true,
				MaxDepth:     1,
				MaxPerParent: 10,
				MinDeposit:   1 * config.Coin, // Low deposit for tests.
				AllowPoW:     true,
			},
		},
	}

	db := storage.NewMemory()
	utxoStore := utxo.NewStore(db)

	validatorPubBytes, _ := hex.DecodeString(pubHex)
	engine, err := consensus.NewPoA([][]byte{validatorPubBytes})
	if err != nil {
		t.Fatalf("create poa: %v", err)
	}
	if err := engine.SetSigner(validatorKey); err != nil {
		t.Fatalf("set signer: %v", err)
	}

	ch, err := chain.New(types.ChainID{}, db, utxoStore, engine)
	if err != nil {
		t.Fatalf("create chain: %v", err)
	}
	if err := ch.InitFromGenesis(gen); err != nil {
		t.Fatalf("init genesis: %v", err)
	}

	adapter := miner.NewUTXOAdapter(utxoStore)
	pool := mempool.New(adapter, 1000)
	pool.SetMinFeeRate(gen.Protocol.Consensus.MinFeeRate)

	srv := New("127.0.0.1:0", ch, utxoStore, pool, nil, gen, engine)

	// Create keystore.
	ksDir := t.TempDir()
	ks, err := wallet.NewKeystore(ksDir)
	if err != nil {
		t.Fatalf("create keystore: %v", err)
	}
	srv.SetKeystore(ks)

	// Create sub-chain manager.
	mgr, err := subchain.NewManager(subchain.ManagerConfig{
		ParentDB: storage.NewMemory(),
		ParentID: types.ChainID{},
		Rules:    &gen.Protocol.SubChain,
	})
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	srv.SetSubChainManager(mgr)

	if err := srv.Start(); err != nil {
		t.Fatalf("start rpc: %v", err)
	}
	t.Cleanup(func() { srv.Stop() })

	return &walletTestEnv{
		server:        srv,
		chain:         ch,
		utxoStore:     utxoStore,
		pool:          pool,
		genesis:       gen,
		engine:        engine,
		validatorKey:  validatorKey,
		validatorAddr: validatorAddr,
		addrHex:       addrHex,
		url:           fmt.Sprintf("http://%s/", srv.Addr()),
	}, mgr
}

func TestRPC_WalletCreateSubChain(t *testing.T) {
	env, _ := setupSubChainWalletEnv(t)

	// Import wallet and pre-load UTXOs.
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	importResp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "sc-creator", Password: "pass", Mnemonic: mnemonic,
	})
	if importResp.Error != nil {
		t.Fatalf("import: %s", importResp.Error.Message)
	}

	var importResult WalletImportResult
	d, _ := json.Marshal(importResp.Result)
	json.Unmarshal(d, &importResult)

	senderAddr, _ := types.ParseAddress(importResult.Address)

	// Pre-load 100 KGX UTXO.
	fakeOutpoint := types.Outpoint{Index: 0}
	copy(fakeOutpoint.TxID[:], []byte("test-tx-for-sccreate-000000000"))
	fakeUTXO := &utxo.UTXO{
		Outpoint: fakeOutpoint,
		Value:    100 * config.Coin,
		Script: types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: senderAddr.Bytes(),
		},
	}
	if err := env.utxoStore.Put(fakeUTXO); err != nil {
		t.Fatalf("put utxo: %v", err)
	}

	// Create sub-chain.
	validatorPubHex := hex.EncodeToString(env.validatorKey.PublicKey())
	resp := rpcCall(t, env.url, "wallet_createSubChain", WalletCreateSubChainParam{
		Name:          "sc-creator",
		Password:      "pass",
		ChainName:     "Test Sub",
		Symbol:        "TSUB",
		ConsensusType: "poa",
		BlockTime:     2,
		BlockReward:   config.MilliCoin,
		MaxSupply:     1_000_000 * config.Coin,
		MinFeeRate:    10,
		Validators:    []string{validatorPubHex},
	})
	if resp.Error != nil {
		t.Fatalf("wallet_createSubChain error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletCreateSubChainResult
	json.Unmarshal(data, &result)

	if result.TxHash == "" {
		t.Error("tx_hash should not be empty")
	}
	if result.ChainID == "" {
		t.Error("chain_id should not be empty")
	}
	if len(result.ChainID) != 64 {
		t.Errorf("chain_id hex length = %d, want 64", len(result.ChainID))
	}

	// Verify tx is in root mempool.
	if env.pool.Count() != 1 {
		t.Errorf("root mempool count = %d, want 1", env.pool.Count())
	}
}

func TestRPC_WalletCreateSubChain_InsufficientFunds(t *testing.T) {
	env, _ := setupSubChainWalletEnv(t)

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "broke-sc", Password: "pass", Mnemonic: mnemonic,
	})

	// No UTXOs loaded — should fail.
	resp := rpcCall(t, env.url, "wallet_createSubChain", WalletCreateSubChainParam{
		Name:          "broke-sc",
		Password:      "pass",
		ChainName:     "Fail Sub",
		Symbol:        "FAIL",
		ConsensusType: "poa",
		BlockTime:     1,
		BlockReward:   config.MilliCoin,
		MaxSupply:     1_000_000 * config.Coin,
		MinFeeRate:    10,
		Validators:    []string{hex.EncodeToString(env.validatorKey.PublicKey())},
	})
	if resp.Error == nil {
		t.Fatal("expected error for insufficient funds")
	}
}

func TestRPC_SubChainGetBalance(t *testing.T) {
	env, mgr := setupSubChainWalletEnv(t)

	// Spawn a sub-chain via the manager.
	scKey, _ := crypto.GenerateKey()
	scPubHex := hex.EncodeToString(scKey.PublicKey())

	rd := subchain.RegistrationData{
		Name:          "Balance Sub",
		Symbol:        "BSUB",
		ConsensusType: "poa",
		BlockTime:     1,
		BlockReward:   config.MilliCoin,
		MaxSupply:     1_000_000 * config.Coin,
		MinFeeRate:    10,
		Validators:    []string{scPubHex},
	}
	rdJSON, _ := json.Marshal(rd)

	var txHash types.Hash
	txHash[0] = 0xBB
	if err := mgr.HandleRegistration(txHash, 0, 50_000_000_000_000, rdJSON, 10); err != nil {
		t.Fatalf("handle registration: %v", err)
	}
	chainID := subchain.DeriveChainID(txHash, 0)
	chainIDHex := hex.EncodeToString(chainID[:])

	// Inject a UTXO into the sub-chain's store.
	sr, ok := mgr.GetChain(chainID)
	if !ok {
		t.Fatal("sub-chain not spawned")
	}

	testAddr := types.Address{0x01, 0x02, 0x03}
	testUTXO := &utxo.UTXO{
		Outpoint: types.Outpoint{TxID: types.Hash{0xCC}, Index: 0},
		Value:    42 * config.Coin,
		Script: types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: testAddr.Bytes(),
		},
	}
	if err := sr.UTXOs.Put(testUTXO); err != nil {
		t.Fatalf("put sub-chain utxo: %v", err)
	}

	// Query balance.
	addrHex := hex.EncodeToString(testAddr[:])
	resp := rpcCall(t, env.url, "subchain_getBalance", SubChainBalanceParam{
		ChainID: chainIDHex,
		Address: addrHex,
	})
	if resp.Error != nil {
		t.Fatalf("subchain_getBalance error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result SubChainBalanceResult
	json.Unmarshal(data, &result)

	if result.Balance != 42*config.Coin {
		t.Errorf("balance = %d, want %d", result.Balance, 42*config.Coin)
	}
	if result.Spendable != 42*config.Coin {
		t.Errorf("spendable = %d, want %d", result.Spendable, 42*config.Coin)
	}
	if result.ChainID != chainIDHex {
		t.Errorf("chain_id = %s, want %s", result.ChainID, chainIDHex)
	}
}

func TestRPC_SubChainGetBalance_NotSynced(t *testing.T) {
	env, _ := setupSubChainWalletEnv(t)

	fakeID := hex.EncodeToString(make([]byte, 32))
	resp := rpcCall(t, env.url, "subchain_getBalance", SubChainBalanceParam{
		ChainID: fakeID,
		Address: hex.EncodeToString(make([]byte, 20)),
	})
	if resp.Error == nil {
		t.Fatal("expected error for non-synced sub-chain")
	}
	if resp.Error.Code != CodeNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeNotFound)
	}
}

func TestRPC_SubChainSend(t *testing.T) {
	env, mgr := setupSubChainWalletEnv(t)

	// Import wallet.
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	importResp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "sc-sender", Password: "pass", Mnemonic: mnemonic,
	})
	if importResp.Error != nil {
		t.Fatalf("import: %s", importResp.Error.Message)
	}
	var importResult WalletImportResult
	d, _ := json.Marshal(importResp.Result)
	json.Unmarshal(d, &importResult)
	senderAddr, _ := types.ParseAddress(importResult.Address)

	// Spawn a sub-chain.
	scKey, _ := crypto.GenerateKey()
	scPubHex := hex.EncodeToString(scKey.PublicKey())

	rd := subchain.RegistrationData{
		Name:          "Send Sub",
		Symbol:        "SSUB",
		ConsensusType: "poa",
		BlockTime:     1,
		BlockReward:   config.MilliCoin,
		MaxSupply:     1_000_000 * config.Coin,
		MinFeeRate:    10,
		Validators:    []string{scPubHex},
	}
	rdJSON, _ := json.Marshal(rd)

	var txHash types.Hash
	txHash[0] = 0xDD
	if err := mgr.HandleRegistration(txHash, 0, 50_000_000_000_000, rdJSON, 10); err != nil {
		t.Fatalf("handle registration: %v", err)
	}
	chainID := subchain.DeriveChainID(txHash, 0)
	chainIDHex := hex.EncodeToString(chainID[:])

	sr, ok := mgr.GetChain(chainID)
	if !ok {
		t.Fatal("sub-chain not spawned")
	}

	// Inject UTXO for the wallet address in the sub-chain store.
	scUTXO := &utxo.UTXO{
		Outpoint: types.Outpoint{TxID: types.Hash{0xEE}, Index: 0},
		Value:    10 * config.Coin,
		Script: types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: senderAddr.Bytes(),
		},
	}
	if err := sr.UTXOs.Put(scUTXO); err != nil {
		t.Fatalf("put sub-chain utxo: %v", err)
	}

	recipientAddr := hex.EncodeToString(make([]byte, 20))

	// Send on sub-chain.
	resp := rpcCall(t, env.url, "subchain_send", SubChainSendParam{
		ChainID:  chainIDHex,
		Name:     "sc-sender",
		Password: "pass",
		To:       recipientAddr,
		Amount:   1 * config.Coin,
	})
	if resp.Error != nil {
		t.Fatalf("subchain_send error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result SubChainSendResult
	json.Unmarshal(data, &result)

	if result.TxHash == "" {
		t.Error("tx_hash should not be empty")
	}

	// Verify tx is in sub-chain mempool, NOT root mempool.
	if sr.Pool.Count() != 1 {
		t.Errorf("sub-chain mempool count = %d, want 1", sr.Pool.Count())
	}
	if env.pool.Count() != 0 {
		t.Errorf("root mempool count = %d, want 0", env.pool.Count())
	}
}

// ── Wallet history ─────────────────────────────────────────────────────

func TestRPC_WalletGetHistory_Mined(t *testing.T) {
	env := setupWalletTestEnv(t)

	// Import a wallet with the validator's key so we can check coinbase history.
	// We'll use the validator address directly by creating a wallet and placing
	// the validator's coinbase address as a known account.
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	importResp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "history-test", Password: "pass", Mnemonic: mnemonic,
	})
	if importResp.Error != nil {
		t.Fatalf("import: %s", importResp.Error.Message)
	}

	// Produce a block using the miner. The coinbase goes to the validator address.
	m := miner.New(env.chain, env.engine,
		env.pool, env.validatorAddr, env.genesis.Protocol.Consensus.BlockReward,
		env.genesis.Protocol.Consensus.MaxSupply, env.chain.Supply)

	blk, err := m.ProduceBlock()
	if err != nil {
		t.Fatalf("produce block: %v", err)
	}
	if err := env.chain.ProcessBlock(blk); err != nil {
		t.Fatalf("process block: %v", err)
	}

	// Now query history for the validator address wallet.
	// But first we need to create a wallet that has the validator address.
	// Since the mnemonic wallet's address != validator address, we need a different approach.
	// Instead, let's just check that the genesis alloc shows up as "mined" (coinbase at height 0).
	// The genesis block has the coinbase tx that allocates to the validator address.
	// We need a wallet that owns the validator address.
	dummySeed := make([]byte, 64)
	copy(dummySeed, []byte("dummy-seed-for-test-0000000000000000000000000000000000000000"))
	if err := env.server.keystore.Create("validator-wallet", dummySeed, []byte("pass"), wallet.DefaultParams()); err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	if err := env.server.keystore.AddAccount("validator-wallet", wallet.AccountEntry{
		Index:   0,
		Name:    "Default",
		Address: env.addrHex,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	resp := rpcCall(t, env.url, "wallet_getHistory", WalletGetHistoryParam{
		Name: "validator-wallet", Password: "pass", Limit: 50,
	})
	if resp.Error != nil {
		t.Fatalf("wallet_getHistory error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletGetHistoryResult
	json.Unmarshal(data, &result)

	if result.Total == 0 {
		t.Fatal("expected at least one history entry")
	}

	// Check that we have a "mined" entry.
	hasMined := false
	for _, e := range result.Entries {
		if e.Type == "mined" {
			hasMined = true
			break
		}
	}
	if !hasMined {
		t.Error("expected a 'mined' entry in history")
	}
}

func TestRPC_WalletGetHistory_Sent(t *testing.T) {
	env := setupWalletTestEnv(t)

	// Import wallet.
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	importResp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "sender", Password: "pass", Mnemonic: mnemonic,
	})
	if importResp.Error != nil {
		t.Fatalf("import: %s", importResp.Error.Message)
	}

	var importResult WalletImportResult
	d, _ := json.Marshal(importResp.Result)
	json.Unmarshal(d, &importResult)

	senderAddr, _ := types.ParseAddress(importResult.Address)

	// Pre-load UTXOs for this address.
	fakeOutpoint := types.Outpoint{Index: 0}
	copy(fakeOutpoint.TxID[:], []byte("test-tx-for-hist-000000000000000"))
	fakeUTXO := &utxo.UTXO{
		Outpoint: fakeOutpoint,
		Value:    10 * config.Coin,
		Script: types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: senderAddr.Bytes(),
		},
	}
	if err := env.utxoStore.Put(fakeUTXO); err != nil {
		t.Fatalf("put utxo: %v", err)
	}

	// Send a transaction.
	sendResp := rpcCall(t, env.url, "wallet_send", WalletSendParam{
		Name:     "sender",
		Password: "pass",
		To:       env.addrHex,
		Amount:   1 * config.Coin,
	})
	if sendResp.Error != nil {
		t.Fatalf("wallet_send error: %s", sendResp.Error.Message)
	}

	var sendResult WalletSendResult
	sd, _ := json.Marshal(sendResp.Result)
	json.Unmarshal(sd, &sendResult)

	// Mine the tx into a block.
	m := miner.New(env.chain, env.engine,
		env.pool, env.validatorAddr, env.genesis.Protocol.Consensus.BlockReward,
		env.genesis.Protocol.Consensus.MaxSupply, env.chain.Supply)

	blk, err := m.ProduceBlock()
	if err != nil {
		t.Fatalf("produce block: %v", err)
	}
	if err := env.chain.ProcessBlock(blk); err != nil {
		t.Fatalf("process block: %v", err)
	}
	env.pool.RemoveConfirmed(blk.Transactions)

	// Get history.
	resp := rpcCall(t, env.url, "wallet_getHistory", WalletGetHistoryParam{
		Name: "sender", Password: "pass", Limit: 50,
	})
	if resp.Error != nil {
		t.Fatalf("wallet_getHistory error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletGetHistoryResult
	json.Unmarshal(data, &result)

	if result.Total == 0 {
		t.Fatal("expected at least one history entry")
	}

	// Check that we have a "sent" entry.
	hasSent := false
	for _, e := range result.Entries {
		if e.Type == "sent" {
			hasSent = true
			if e.TxHash != sendResult.TxHash {
				t.Errorf("sent tx hash = %s, want %s", e.TxHash, sendResult.TxHash)
			}
			break
		}
	}
	if !hasSent {
		t.Errorf("expected a 'sent' entry in history, got types: %v", historyTypes(result.Entries))
	}
}

func TestRPC_WalletGetHistory_Pagination(t *testing.T) {
	env := setupWalletTestEnv(t)

	// Create a wallet that owns the validator address.
	dummySeed := make([]byte, 64)
	copy(dummySeed, []byte("dummy-seed-for-test-0000000000000000000000000000000000000000"))
	if err := env.server.keystore.Create("paginated", dummySeed, []byte("pass"), wallet.DefaultParams()); err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	if err := env.server.keystore.AddAccount("paginated", wallet.AccountEntry{
		Index: 0, Name: "Default", Address: env.addrHex,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	// Produce 3 blocks so we have 3 coinbase entries + the genesis coinbase.
	m := miner.New(env.chain, env.engine,
		env.pool, env.validatorAddr, env.genesis.Protocol.Consensus.BlockReward,
		env.genesis.Protocol.Consensus.MaxSupply, env.chain.Supply)
	for i := 0; i < 3; i++ {
		blk, err := m.ProduceBlock()
		if err != nil {
			t.Fatalf("produce block %d: %v", i, err)
		}
		if err := env.chain.ProcessBlock(blk); err != nil {
			t.Fatalf("process block %d: %v", i, err)
		}
	}

	// Request with limit=2, offset=0.
	resp := rpcCall(t, env.url, "wallet_getHistory", WalletGetHistoryParam{
		Name: "paginated", Password: "pass", Limit: 2, Offset: 0,
	})
	if resp.Error != nil {
		t.Fatalf("wallet_getHistory error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletGetHistoryResult
	json.Unmarshal(data, &result)

	if result.Total < 4 {
		t.Errorf("total = %d, want >= 4 (genesis + 3 blocks)", result.Total)
	}
	if len(result.Entries) != 2 {
		t.Errorf("entries = %d, want 2 (limit)", len(result.Entries))
	}

	// Request with offset=2.
	resp2 := rpcCall(t, env.url, "wallet_getHistory", WalletGetHistoryParam{
		Name: "paginated", Password: "pass", Limit: 2, Offset: 2,
	})
	if resp2.Error != nil {
		t.Fatalf("wallet_getHistory page 2 error: %s", resp2.Error.Message)
	}

	data2, _ := json.Marshal(resp2.Result)
	var result2 WalletGetHistoryResult
	json.Unmarshal(data2, &result2)

	if result2.Total != result.Total {
		t.Errorf("total changed between pages: %d vs %d", result.Total, result2.Total)
	}
	if len(result2.Entries) != 2 {
		t.Errorf("page 2 entries = %d, want 2", len(result2.Entries))
	}
}

func TestRPC_WalletGetHistory_WrongPassword(t *testing.T) {
	env := setupWalletTestEnv(t)

	dummySeed := make([]byte, 64)
	copy(dummySeed, []byte("dummy-seed-for-test-0000000000000000000000000000000000000000"))
	if err := env.server.keystore.Create("locked", dummySeed, []byte("correct"), wallet.DefaultParams()); err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	resp := rpcCall(t, env.url, "wallet_getHistory", WalletGetHistoryParam{
		Name: "locked", Password: "wrong",
	})
	if resp.Error == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestRPC_WalletGetHistory_WalletNotEnabled(t *testing.T) {
	env := setupWalletTestEnv(t)

	// Disable wallet.
	env.server.keystore = nil

	resp := rpcCall(t, env.url, "wallet_getHistory", WalletGetHistoryParam{
		Name: "any", Password: "any",
	})
	if resp.Error == nil {
		t.Fatal("expected error when wallet not enabled")
	}
}

func TestRPC_WalletGetHistory_TokenSent(t *testing.T) {
	env := setupWalletTestEnv(t)

	// Import a wallet and give it enough KGX for mint fee + token send fee.
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	importResp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "token-sender", Password: "pass", Mnemonic: mnemonic,
	})
	if importResp.Error != nil {
		t.Fatalf("import: %s", importResp.Error.Message)
	}
	var importResult WalletImportResult
	d, _ := json.Marshal(importResp.Result)
	json.Unmarshal(d, &importResult)
	senderAddr, _ := types.ParseAddress(importResult.Address)

	// Give 200 KGX to cover mint fee (50 KGX) + token send fee.
	fakeOutpoint := types.Outpoint{Index: 0}
	copy(fakeOutpoint.TxID[:], []byte("test-tx-tokenhist-00000000000000"))
	if err := env.utxoStore.Put(&utxo.UTXO{
		Outpoint: fakeOutpoint,
		Value:    200 * config.Coin,
		Script:   types.Script{Type: types.ScriptTypeP2PKH, Data: senderAddr.Bytes()},
	}); err != nil {
		t.Fatalf("put utxo: %v", err)
	}

	// 1) Mint tokens.
	mintResp := rpcCall(t, env.url, "wallet_mintToken", WalletMintTokenParam{
		Name: "token-sender", Password: "pass",
		TokenName: "History Test Token", Symbol: "HTT", Decimals: 8, Amount: 10_000,
	})
	if mintResp.Error != nil {
		t.Fatalf("mint error: %s", mintResp.Error.Message)
	}
	var mintResult WalletMintTokenResult
	md, _ := json.Marshal(mintResp.Result)
	json.Unmarshal(md, &mintResult)

	// Mine the mint tx.
	m := miner.New(env.chain, env.engine,
		env.pool, env.validatorAddr, env.genesis.Protocol.Consensus.BlockReward,
		env.genesis.Protocol.Consensus.MaxSupply, env.chain.Supply)
	blk, err := m.ProduceBlock()
	if err != nil {
		t.Fatalf("produce block: %v", err)
	}
	if err := env.chain.ProcessBlock(blk); err != nil {
		t.Fatalf("process block: %v", err)
	}
	env.pool.RemoveConfirmed(blk.Transactions)

	// 2) Send tokens to the validator address.
	sendResp := rpcCall(t, env.url, "wallet_sendToken", WalletSendTokenParam{
		Name: "token-sender", Password: "pass",
		TokenID: mintResult.TokenID, To: env.addrHex, Amount: 500,
	})
	if sendResp.Error != nil {
		t.Fatalf("send token error: %s", sendResp.Error.Message)
	}

	// Mine the token send tx.
	blk2, err := m.ProduceBlock()
	if err != nil {
		t.Fatalf("produce block 2: %v", err)
	}
	if err := env.chain.ProcessBlock(blk2); err != nil {
		t.Fatalf("process block 2: %v", err)
	}
	env.pool.RemoveConfirmed(blk2.Transactions)

	// 3) Check history — should contain "mint" and "token_sent".
	resp := rpcCall(t, env.url, "wallet_getHistory", WalletGetHistoryParam{
		Name: "token-sender", Password: "pass", Limit: 50,
	})
	if resp.Error != nil {
		t.Fatalf("wallet_getHistory error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletGetHistoryResult
	json.Unmarshal(data, &result)

	var hasMintEntry, hasTokenSent bool
	for _, e := range result.Entries {
		switch e.Type {
		case "mint":
			hasMintEntry = true
			if e.TokenID != mintResult.TokenID {
				t.Errorf("mint TokenID = %s, want %s", e.TokenID, mintResult.TokenID)
			}
			if e.TokenAmount != 10_000 {
				t.Errorf("mint TokenAmount = %d, want 10000", e.TokenAmount)
			}
		case "token_sent":
			hasTokenSent = true
			if e.TokenID != mintResult.TokenID {
				t.Errorf("token_sent TokenID = %s, want %s", e.TokenID, mintResult.TokenID)
			}
			if e.TokenAmount != 500 {
				t.Errorf("token_sent TokenAmount = %d, want 500", e.TokenAmount)
			}
		}
	}

	if !hasMintEntry {
		t.Errorf("expected 'mint' entry, got types: %v", historyTypes(result.Entries))
	}
	if !hasTokenSent {
		t.Errorf("expected 'token_sent' entry, got types: %v", historyTypes(result.Entries))
	}
}

// ── Multi-address UTXO aggregation ──────────────────────────────────────

func TestRPC_WalletSend_FromChangeAddress(t *testing.T) {
	env := setupWalletTestEnv(t)

	// Import a known mnemonic.
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	importResp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "multi-addr", Password: "pass", Mnemonic: mnemonic,
	})
	if importResp.Error != nil {
		t.Fatalf("import: %s", importResp.Error.Message)
	}

	var importResult WalletImportResult
	d, _ := json.Marshal(importResp.Result)
	json.Unmarshal(d, &importResult)

	// Derive the change address (internal chain index 0).
	seed, _ := wallet.SeedFromMnemonic(mnemonic, "")
	master, _ := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	changeKey, _ := master.DeriveAddress(0, wallet.ChangeInternal, 0)
	changeAddr := changeKey.Address()

	// Register the change address in the wallet's account list.
	if err := env.server.keystore.AddAccount("multi-addr", wallet.AccountEntry{
		Index:   0,
		Change:  wallet.ChangeInternal,
		Name:    "Change 0",
		Address: changeAddr.String(),
	}); err != nil {
		t.Fatalf("add change account: %v", err)
	}

	// Put UTXO *only* on the change address (account 0 has nothing).
	fakeOutpoint := types.Outpoint{Index: 0}
	copy(fakeOutpoint.TxID[:], []byte("test-tx-for-change-000000000000"))
	changeUTXO := &utxo.UTXO{
		Outpoint: fakeOutpoint,
		Value:    5 * config.Coin,
		Script: types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: changeAddr.Bytes(),
		},
	}
	if err := env.utxoStore.Put(changeUTXO); err != nil {
		t.Fatalf("put change utxo: %v", err)
	}

	// Send from the wallet — it should find the change address UTXO.
	resp := rpcCall(t, env.url, "wallet_send", WalletSendParam{
		Name:     "multi-addr",
		Password: "pass",
		To:       env.addrHex,
		Amount:   1 * config.Coin,
	})
	if resp.Error != nil {
		t.Fatalf("wallet_send error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result WalletSendResult
	json.Unmarshal(data, &result)

	if result.TxHash == "" {
		t.Error("tx_hash should not be empty")
	}

	// Verify tx is in mempool.
	if env.pool.Count() != 1 {
		t.Errorf("mempool count = %d, want 1", env.pool.Count())
	}
}

func TestRPC_WalletSend_MultipleAddresses(t *testing.T) {
	env := setupWalletTestEnv(t)

	// Import wallet.
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	importResp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "multi-all", Password: "pass", Mnemonic: mnemonic,
	})
	if importResp.Error != nil {
		t.Fatalf("import: %s", importResp.Error.Message)
	}

	var importResult WalletImportResult
	d, _ := json.Marshal(importResp.Result)
	json.Unmarshal(d, &importResult)
	senderAddr, _ := types.ParseAddress(importResult.Address)

	// Derive second external address.
	seed, _ := wallet.SeedFromMnemonic(mnemonic, "")
	master, _ := wallet.NewMasterKey(seed)
	for i := range seed {
		seed[i] = 0
	}
	addr1Key, _ := master.DeriveAddress(0, wallet.ChangeExternal, 1)
	addr1 := addr1Key.Address()

	// Register address 1 in wallet.
	if err := env.server.keystore.AddAccount("multi-all", wallet.AccountEntry{
		Index:   1,
		Name:    "Address 1",
		Address: addr1.String(),
	}); err != nil {
		t.Fatalf("add account 1: %v", err)
	}

	// Put small UTXOs on both addresses — neither alone is enough.
	fakeOp0 := types.Outpoint{Index: 0}
	copy(fakeOp0.TxID[:], []byte("test-tx-multi-addr0-000000000000"))
	if err := env.utxoStore.Put(&utxo.UTXO{
		Outpoint: fakeOp0,
		Value:    1 * config.Coin, // 1 KGX on account 0
		Script:   types.Script{Type: types.ScriptTypeP2PKH, Data: senderAddr.Bytes()},
	}); err != nil {
		t.Fatalf("put utxo 0: %v", err)
	}

	fakeOp1 := types.Outpoint{Index: 0}
	copy(fakeOp1.TxID[:], []byte("test-tx-multi-addr1-000000000000"))
	if err := env.utxoStore.Put(&utxo.UTXO{
		Outpoint: fakeOp1,
		Value:    1 * config.Coin, // 1 KGX on address 1
		Script:   types.Script{Type: types.ScriptTypeP2PKH, Data: addr1.Bytes()},
	}); err != nil {
		t.Fatalf("put utxo 1: %v", err)
	}

	// Send 1.5 KGX — requires combining UTXOs from both addresses.
	resp := rpcCall(t, env.url, "wallet_send", WalletSendParam{
		Name:     "multi-all",
		Password: "pass",
		To:       env.addrHex,
		Amount:   1_500_000_000_000, // 1.5 KGX
	})
	if resp.Error != nil {
		t.Fatalf("wallet_send error: %s", resp.Error.Message)
	}

	if env.pool.Count() != 1 {
		t.Errorf("mempool count = %d, want 1", env.pool.Count())
	}
}

// ── Token metadata validation ──────────────────────────────────────────

func TestRPC_WalletMintToken_InvalidName(t *testing.T) {
	env := setupWalletTestEnv(t)

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "val-name", Password: "pass", Mnemonic: mnemonic,
	})

	tests := []struct {
		name      string
		tokenName string
		wantErr   string
	}{
		{"special chars", "Bad<Token>", "token_name must be"},
		{"emoji", "Tok\U0001F600en", "token_name must be"},
		{"too long", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "token_name must be"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := rpcCall(t, env.url, "wallet_mintToken", WalletMintTokenParam{
				Name:      "val-name",
				Password:  "pass",
				TokenName: tc.tokenName,
				Symbol:    "TST",
				Decimals:  8,
				Amount:    1000,
			})
			if resp.Error == nil {
				t.Fatal("expected error for invalid token name")
			}
			if resp.Error.Code != CodeInvalidParams {
				t.Errorf("error code = %d, want %d", resp.Error.Code, CodeInvalidParams)
			}
		})
	}
}

func TestRPC_WalletMintToken_InvalidSymbol(t *testing.T) {
	env := setupWalletTestEnv(t)

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "val-sym", Password: "pass", Mnemonic: mnemonic,
	})

	tests := []struct {
		name   string
		symbol string
	}{
		{"lowercase", "abc"},
		{"too short", "X"},
		{"too long", "ABCDEFGHIJK"},
		{"special chars", "A!B"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := rpcCall(t, env.url, "wallet_mintToken", WalletMintTokenParam{
				Name:      "val-sym",
				Password:  "pass",
				TokenName: "Valid Name",
				Symbol:    tc.symbol,
				Decimals:  8,
				Amount:    1000,
			})
			if resp.Error == nil {
				t.Fatal("expected error for invalid symbol")
			}
			if resp.Error.Code != CodeInvalidParams {
				t.Errorf("error code = %d, want %d", resp.Error.Code, CodeInvalidParams)
			}
		})
	}
}

func TestRPC_WalletMintToken_InvalidDecimals(t *testing.T) {
	env := setupWalletTestEnv(t)

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "val-dec", Password: "pass", Mnemonic: mnemonic,
	})

	resp := rpcCall(t, env.url, "wallet_mintToken", WalletMintTokenParam{
		Name:      "val-dec",
		Password:  "pass",
		TokenName: "Valid Name",
		Symbol:    "TST",
		Decimals:  19,
		Amount:    1000,
	})
	if resp.Error == nil {
		t.Fatal("expected error for decimals > 18")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}

func TestRPC_WalletMintToken_ValidMetadata(t *testing.T) {
	env := setupWalletTestEnv(t)

	// Import and fund wallet.
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	importResp := rpcCall(t, env.url, "wallet_import", WalletImportParam{
		Name: "val-ok", Password: "pass", Mnemonic: mnemonic,
	})
	if importResp.Error != nil {
		t.Fatalf("import: %s", importResp.Error.Message)
	}
	var importResult WalletImportResult
	d, _ := json.Marshal(importResp.Result)
	json.Unmarshal(d, &importResult)
	senderAddr, _ := types.ParseAddress(importResult.Address)

	fakeOutpoint := types.Outpoint{Index: 0}
	copy(fakeOutpoint.TxID[:], []byte("test-tx-val-ok-0000000000000000"))
	if err := env.utxoStore.Put(&utxo.UTXO{
		Outpoint: fakeOutpoint,
		Value:    100 * config.Coin,
		Script:   types.Script{Type: types.ScriptTypeP2PKH, Data: senderAddr.Bytes()},
	}); err != nil {
		t.Fatalf("put utxo: %v", err)
	}

	// Valid: alphanumeric name with spaces/hyphens, uppercase symbol, decimals <= 18.
	resp := rpcCall(t, env.url, "wallet_mintToken", WalletMintTokenParam{
		Name:      "val-ok",
		Password:  "pass",
		TokenName: "My Test-Token 1",
		Symbol:    "MTK",
		Decimals:  18,
		Amount:    1_000_000,
	})
	if resp.Error != nil {
		t.Fatalf("wallet_mintToken error: %s", resp.Error.Message)
	}
}

func historyTypes(entries []TxHistoryEntry) []string {
	types := make([]string, len(entries))
	for i, e := range entries {
		types[i] = e.Type
	}
	return types
}

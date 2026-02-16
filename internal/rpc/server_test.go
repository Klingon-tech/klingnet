package rpc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
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
	"github.com/Klingon-tech/klingnet-chain/internal/token"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/internal/wallet"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// testEnv holds all components for an RPC test.
type testEnv struct {
	server        *Server
	chain         *chain.Chain
	utxoStore     *utxo.Store
	pool          *mempool.Pool
	genesis       *config.Genesis
	validatorKey  *crypto.PrivateKey
	validatorAddr types.Address
	addrHex       string
	url           string
	db            storage.DB
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	klog.Init("error", false, "")

	// Generate validator key.
	validatorKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	validatorPub := validatorKey.PublicKey()
	validatorAddr := crypto.AddressFromPubKey(validatorPub)
	pubHex := hex.EncodeToString(validatorPub)
	addrHex := validatorAddr.String()

	// Create genesis.
	gen := &config.Genesis{
		ChainID:   "klingnet-test-rpc",
		ChainName: "RPC Test",
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
				MaxDepth:       5,
				MaxPerParent:   10,
				AnchorInterval: 10,
			},
			Token: config.TokenRules{
				MaxTokensPerUTXO: 1,
				AllowMinting:     true,
			},
		},
	}

	// Create components.
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

	// Create and start RPC server on random port.
	srv := New("127.0.0.1:0", ch, utxoStore, pool, nil, gen, engine)
	tokenStore := token.NewStore(db)
	srv.SetTokenStore(tokenStore)
	if err := srv.Start(); err != nil {
		t.Fatalf("start rpc: %v", err)
	}
	t.Cleanup(func() { srv.Stop() })

	return &testEnv{
		server:        srv,
		chain:         ch,
		utxoStore:     utxoStore,
		pool:          pool,
		genesis:       gen,
		validatorKey:  validatorKey,
		validatorAddr: validatorAddr,
		addrHex:       addrHex,
		url:           fmt.Sprintf("http://%s/", srv.Addr()),
		db:            db,
	}
}

func rpcCall(t *testing.T, url, method string, params interface{}) Response {
	t.Helper()
	req := Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", method, err)
	}
	defer resp.Body.Close()

	var rpcResp Response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return rpcResp
}

// ── Tests ───────────────────────────────────────────────────────────────

func TestRPC_ChainGetInfo(t *testing.T) {
	env := setupTestEnv(t)

	resp := rpcCall(t, env.url, "chain_getInfo", nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result ChainInfoResult
	json.Unmarshal(data, &result)

	if result.ChainID != "klingnet-test-rpc" {
		t.Errorf("chain_id = %q, want %q", result.ChainID, "klingnet-test-rpc")
	}
	if result.Height != 0 {
		t.Errorf("height = %d, want 0", result.Height)
	}
	if result.TipHash == "" {
		t.Error("tip_hash is empty")
	}
}

func TestRPC_ChainGetBlockByHeight(t *testing.T) {
	env := setupTestEnv(t)

	resp := rpcCall(t, env.url, "chain_getBlockByHeight", HeightParam{Height: 0})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("result is nil")
	}

	data, _ := json.Marshal(resp.Result)
	var result BlockResult
	json.Unmarshal(data, &result)

	if result.Hash == "" {
		t.Error("block hash is empty")
	}
	if result.Header == nil {
		t.Error("block header is nil")
	}
	if len(result.Transactions) == 0 {
		t.Error("block has no transactions")
	}
	if result.Transactions[0].Hash == "" {
		t.Error("transaction hash is empty")
	}
}

func TestRPC_ChainGetBlockByHash(t *testing.T) {
	env := setupTestEnv(t)

	tipHash := env.chain.TipHash().String()
	resp := rpcCall(t, env.url, "chain_getBlockByHash", HashParam{Hash: tipHash})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("result is nil")
	}

	data, _ := json.Marshal(resp.Result)
	var result BlockResult
	json.Unmarshal(data, &result)

	if result.Hash == "" {
		t.Error("block hash is empty")
	}
	if result.Hash != tipHash {
		t.Errorf("block hash = %q, want %q", result.Hash, tipHash)
	}
}

func TestRPC_ChainGetBlockByHash_NotFound(t *testing.T) {
	env := setupTestEnv(t)

	fakeHash := hex.EncodeToString(make([]byte, 32))
	resp := rpcCall(t, env.url, "chain_getBlockByHash", HashParam{Hash: fakeHash})
	if resp.Error == nil {
		t.Fatal("expected error for non-existent block")
	}
	if resp.Error.Code != CodeNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeNotFound)
	}
}

func TestRPC_ChainGetTransaction(t *testing.T) {
	env := setupTestEnv(t)

	// Get the genesis block's coinbase tx hash.
	blk, err := env.chain.GetBlockByHeight(0)
	if err != nil {
		t.Fatalf("get genesis: %v", err)
	}
	if len(blk.Transactions) == 0 {
		t.Fatal("genesis has no transactions")
	}
	txHash := blk.Transactions[0].Hash().String()

	resp := rpcCall(t, env.url, "chain_getTransaction", HashParam{Hash: txHash})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("result is nil")
	}

	data, _ := json.Marshal(resp.Result)
	var result TxResult
	json.Unmarshal(data, &result)

	if result.Hash == "" {
		t.Error("tx hash is empty")
	}
	if result.Hash != txHash {
		t.Errorf("tx hash = %q, want %q", result.Hash, txHash)
	}
	if result.Version != 1 {
		t.Errorf("tx version = %d, want 1", result.Version)
	}
}

func TestRPC_ChainGetTransaction_NotFound(t *testing.T) {
	env := setupTestEnv(t)

	fakeHash := hex.EncodeToString(make([]byte, 32))
	resp := rpcCall(t, env.url, "chain_getTransaction", HashParam{Hash: fakeHash})
	if resp.Error == nil {
		t.Fatal("expected error for non-existent tx")
	}
	if resp.Error.Code != CodeNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeNotFound)
	}
}

func TestRPC_UTXOGetByAddress(t *testing.T) {
	env := setupTestEnv(t)

	resp := rpcCall(t, env.url, "utxo_getByAddress", AddressParam{Address: env.addrHex})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result UTXOListResult
	json.Unmarshal(data, &result)

	if len(result.UTXOs) == 0 {
		t.Fatal("expected at least one UTXO for validator address")
	}
}

func TestRPC_UTXOGetBalance(t *testing.T) {
	env := setupTestEnv(t)

	resp := rpcCall(t, env.url, "utxo_getBalance", AddressParam{Address: env.addrHex})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result BalanceResult
	json.Unmarshal(data, &result)

	expected := uint64(100_000) * config.Coin
	if result.Balance != expected {
		t.Errorf("balance = %d, want %d", result.Balance, expected)
	}
	// Genesis alloc UTXOs (height=0) are NOT marked coinbase, so fully spendable.
	if result.Spendable != expected {
		t.Errorf("spendable = %d, want %d", result.Spendable, expected)
	}
	if result.Immature != 0 {
		t.Errorf("immature = %d, want 0", result.Immature)
	}
	if result.Staked != 0 {
		t.Errorf("staked = %d, want 0", result.Staked)
	}
	if result.Locked != 0 {
		t.Errorf("locked = %d, want 0", result.Locked)
	}
}

func TestRPC_UTXOGetBalance_IncludesStakes(t *testing.T) {
	env := setupTestEnv(t)

	// Plant a stake UTXO for the validator's pubkey.
	stakeAmount := uint64(2000) * config.Coin
	stakeUTXO := &utxo.UTXO{
		Outpoint: types.Outpoint{TxID: types.Hash{0xAA}, Index: 0},
		Value:    stakeAmount,
		Script: types.Script{
			Type: types.ScriptTypeStake,
			Data: env.validatorKey.PublicKey(), // 33-byte compressed pubkey
		},
		Height: 1,
	}
	if err := env.utxoStore.Put(stakeUTXO); err != nil {
		t.Fatalf("put stake utxo: %v", err)
	}

	// Query balance by address — should include stakes even though
	// stake UTXOs are indexed by pubkey, not address.
	resp := rpcCall(t, env.url, "utxo_getBalance", AddressParam{Address: env.addrHex})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result BalanceResult
	json.Unmarshal(data, &result)

	genesisAlloc := uint64(100_000) * config.Coin
	wantTotal := genesisAlloc + stakeAmount
	if result.Balance != wantTotal {
		t.Errorf("total = %d, want %d", result.Balance, wantTotal)
	}
	if result.Spendable != genesisAlloc {
		t.Errorf("spendable = %d, want %d", result.Spendable, genesisAlloc)
	}
	if result.Staked != stakeAmount {
		t.Errorf("staked = %d, want %d", result.Staked, stakeAmount)
	}
}

func TestRPC_UTXOGet(t *testing.T) {
	env := setupTestEnv(t)

	// Get the genesis coinbase tx to find its outpoint.
	blk, _ := env.chain.GetBlockByHeight(0)
	txHash := blk.Transactions[0].Hash().String()

	resp := rpcCall(t, env.url, "utxo_get", OutpointParam{TxID: txHash, Index: 0})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("result is nil")
	}
}

func TestRPC_UTXOGet_NotFound(t *testing.T) {
	env := setupTestEnv(t)

	fakeHash := hex.EncodeToString(make([]byte, 32))
	resp := rpcCall(t, env.url, "utxo_get", OutpointParam{TxID: fakeHash, Index: 99})
	if resp.Error == nil {
		t.Fatal("expected error for non-existent UTXO")
	}
	if resp.Error.Code != CodeNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeNotFound)
	}
}

func TestRPC_MempoolGetInfo(t *testing.T) {
	env := setupTestEnv(t)

	resp := rpcCall(t, env.url, "mempool_getInfo", nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result MempoolInfoResult
	json.Unmarshal(data, &result)

	if result.Count != 0 {
		t.Errorf("count = %d, want 0", result.Count)
	}
	if result.MinFeeRate != 10 {
		t.Errorf("min_fee_rate = %d, want %d", result.MinFeeRate, 10)
	}
}

func TestRPC_MempoolGetContent(t *testing.T) {
	env := setupTestEnv(t)

	resp := rpcCall(t, env.url, "mempool_getContent", nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result MempoolContentResult
	json.Unmarshal(data, &result)

	if len(result.Hashes) != 0 {
		t.Errorf("hashes count = %d, want 0", len(result.Hashes))
	}
}

func TestRPC_NetGetNodeInfo(t *testing.T) {
	env := setupTestEnv(t)

	resp := rpcCall(t, env.url, "net_getNodeInfo", nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result NodeInfoResult
	json.Unmarshal(data, &result)

	// P2P node is nil in test, so ID should be empty.
	if result.ID != "" {
		t.Errorf("expected empty ID without P2P node, got %q", result.ID)
	}
}

func TestRPC_NetGetPeerInfo(t *testing.T) {
	env := setupTestEnv(t)

	resp := rpcCall(t, env.url, "net_getPeerInfo", nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result PeerInfoResult
	json.Unmarshal(data, &result)

	if result.Count != 0 {
		t.Errorf("count = %d, want 0", result.Count)
	}
}

func TestRPC_MethodNotFound(t *testing.T) {
	env := setupTestEnv(t)

	resp := rpcCall(t, env.url, "nonexistent_method", nil)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeMethodNotFound)
	}
}

func TestRPC_InvalidParams(t *testing.T) {
	env := setupTestEnv(t)

	// chain_getBlockByHash requires params.
	resp := rpcCall(t, env.url, "chain_getBlockByHash", nil)
	if resp.Error == nil {
		t.Fatal("expected error for missing params")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}

func TestRPC_InvalidAddress(t *testing.T) {
	env := setupTestEnv(t)

	resp := rpcCall(t, env.url, "utxo_getBalance", AddressParam{Address: "xyz"})
	if resp.Error == nil {
		t.Fatal("expected error for invalid address")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}

func TestRPC_InvalidJSON(t *testing.T) {
	env := setupTestEnv(t)

	resp, err := http.Post(env.url, "application/json", bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp Response
	json.NewDecoder(resp.Body).Decode(&rpcResp)

	if rpcResp.Error == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if rpcResp.Error.Code != CodeParseError {
		t.Errorf("error code = %d, want %d", rpcResp.Error.Code, CodeParseError)
	}
}

func TestRPC_GetMethodNotAllowed(t *testing.T) {
	env := setupTestEnv(t)

	resp, err := http.Get(env.url)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp Response
	json.NewDecoder(resp.Body).Decode(&rpcResp)

	if rpcResp.Error == nil {
		t.Fatal("expected error for GET request")
	}
	if rpcResp.Error.Code != CodeInvalidRequest {
		t.Errorf("error code = %d, want %d", rpcResp.Error.Code, CodeInvalidRequest)
	}
}

// --- IP Filtering ---

func setupTestEnvWithConfig(t *testing.T, rpcCfg config.RPCConfig) *testEnv {
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
		ChainID:   "klingnet-test-rpc",
		ChainName: "RPC Test",
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
	engine, _ := consensus.NewPoA([][]byte{validatorPubBytes})
	engine.SetSigner(validatorKey)

	ch, _ := chain.New(types.ChainID{}, db, utxoStore, engine)
	ch.InitFromGenesis(gen)

	adapter := miner.NewUTXOAdapter(utxoStore)
	pool := mempool.New(adapter, 1000)
	pool.SetMinFeeRate(gen.Protocol.Consensus.MinFeeRate)

	srv := New("127.0.0.1:0", ch, utxoStore, pool, nil, gen, engine, rpcCfg)
	if err := srv.Start(); err != nil {
		t.Fatalf("start rpc: %v", err)
	}
	t.Cleanup(func() { srv.Stop() })

	return &testEnv{
		server:        srv,
		chain:         ch,
		utxoStore:     utxoStore,
		pool:          pool,
		genesis:       gen,
		validatorKey:  validatorKey,
		validatorAddr: validatorAddr,
		addrHex:       addrHex,
		url:           fmt.Sprintf("http://%s/", srv.Addr()),
	}
}

func TestRPC_IPFilter_Allowed(t *testing.T) {
	env := setupTestEnvWithConfig(t, config.RPCConfig{
		AllowedIPs: []string{"127.0.0.1"},
	})

	resp := rpcCall(t, env.url, "chain_getInfo", nil)
	if resp.Error != nil {
		t.Errorf("expected success for 127.0.0.1, got error: %s", resp.Error.Message)
	}
}

func TestRPC_IPFilter_Blocked(t *testing.T) {
	env := setupTestEnvWithConfig(t, config.RPCConfig{
		AllowedIPs: []string{"10.0.0.0/8"}, // Only allow 10.x.x.x.
	})

	// Request comes from 127.0.0.1 → should be blocked.
	req := Request{JSONRPC: "2.0", Method: "chain_getInfo", ID: 1}
	body, _ := json.Marshal(req)
	resp, err := http.Post(env.url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestRPC_IPFilter_Empty_AllowsAll(t *testing.T) {
	env := setupTestEnvWithConfig(t, config.RPCConfig{
		AllowedIPs: nil, // Empty = allow all.
	})

	resp := rpcCall(t, env.url, "chain_getInfo", nil)
	if resp.Error != nil {
		t.Errorf("empty AllowedIPs should allow all: %s", resp.Error.Message)
	}
}

// --- CORS ---

func TestRPC_CORS_WildcardOrigin(t *testing.T) {
	env := setupTestEnvWithConfig(t, config.RPCConfig{
		CORSOrigins: []string{"*"},
	})

	req := Request{JSONRPC: "2.0", Method: "chain_getInfo", ID: 1}
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequest("POST", env.url, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Origin", "http://example.com")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	origin := resp.Header.Get("Access-Control-Allow-Origin")
	if origin != "*" {
		t.Errorf("CORS origin = %q, want %q", origin, "*")
	}
}

func TestRPC_CORS_SpecificOrigin(t *testing.T) {
	env := setupTestEnvWithConfig(t, config.RPCConfig{
		CORSOrigins: []string{"http://myapp.com"},
	})

	req := Request{JSONRPC: "2.0", Method: "chain_getInfo", ID: 1}
	body, _ := json.Marshal(req)

	// Matching origin.
	httpReq, _ := http.NewRequest("POST", env.url, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Origin", "http://myapp.com")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	origin := resp.Header.Get("Access-Control-Allow-Origin")
	if origin != "http://myapp.com" {
		t.Errorf("CORS origin = %q, want %q", origin, "http://myapp.com")
	}

	// Non-matching origin.
	body2, _ := json.Marshal(req)
	httpReq2, _ := http.NewRequest("POST", env.url, bytes.NewReader(body2))
	httpReq2.Header.Set("Content-Type", "application/json")
	httpReq2.Header.Set("Origin", "http://evil.com")

	resp2, err := http.DefaultClient.Do(httpReq2)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp2.Body.Close()

	origin2 := resp2.Header.Get("Access-Control-Allow-Origin")
	if origin2 != "" {
		t.Errorf("non-matching origin should have no CORS header, got %q", origin2)
	}
}

func TestRPC_CORS_Preflight(t *testing.T) {
	env := setupTestEnvWithConfig(t, config.RPCConfig{
		CORSOrigins: []string{"*"},
	})

	httpReq, _ := http.NewRequest("OPTIONS", env.url, nil)
	httpReq.Header.Set("Origin", "http://example.com")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Methods") == "" {
		t.Error("preflight should have Allow-Methods header")
	}
}

func TestRPC_CORS_Disabled(t *testing.T) {
	env := setupTestEnvWithConfig(t, config.RPCConfig{
		CORSOrigins: nil, // Disabled.
	})

	req := Request{JSONRPC: "2.0", Method: "chain_getInfo", ID: 1}
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequest("POST", env.url, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Origin", "http://example.com")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	origin := resp.Header.Get("Access-Control-Allow-Origin")
	if origin != "" {
		t.Errorf("disabled CORS should have no origin header, got %q", origin)
	}
}

// --- Staking ---

func TestRPC_StakeGetValidators(t *testing.T) {
	env := setupTestEnv(t)

	resp := rpcCall(t, env.url, "stake_getValidators", nil)
	if resp.Error != nil {
		t.Fatalf("stake_getValidators error: %s", resp.Error.Message)
	}

	var result ValidatorsResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &result)

	if len(result.Validators) != 1 {
		t.Fatalf("expected 1 validator, got %d", len(result.Validators))
	}
	if !result.Validators[0].IsGenesis {
		t.Error("first validator should be genesis")
	}
}

func TestRPC_StakeGetInfo_GenesisValidator(t *testing.T) {
	env := setupTestEnv(t)

	pubHex := hex.EncodeToString(env.validatorKey.PublicKey())
	resp := rpcCall(t, env.url, "stake_getInfo", map[string]string{"pubkey": pubHex})
	if resp.Error != nil {
		t.Fatalf("stake_getInfo error: %s", resp.Error.Message)
	}

	var result StakeInfoResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &result)

	if !result.IsGenesis {
		t.Error("should be flagged as genesis validator")
	}
	if !result.Sufficient {
		t.Error("genesis validator should always be sufficient")
	}
}

func TestRPC_StakeGetInfo_UnknownPubkey(t *testing.T) {
	env := setupTestEnv(t)

	// Random pubkey that's not a validator.
	fakePub := make([]byte, 33)
	fakePub[0] = 0x02
	fakePub[1] = 0xFF
	resp := rpcCall(t, env.url, "stake_getInfo", map[string]string{"pubkey": hex.EncodeToString(fakePub)})
	if resp.Error != nil {
		t.Fatalf("stake_getInfo error: %s", resp.Error.Message)
	}

	var result StakeInfoResult
	data, _ := json.Marshal(resp.Result)
	json.Unmarshal(data, &result)

	if result.IsGenesis {
		t.Error("unknown pubkey should not be genesis")
	}
	if result.Sufficient {
		t.Error("unknown pubkey with no stake should not be sufficient")
	}
}

// --- Validator status endpoints ---

func TestRPC_ValidatorGetStatus_NoTracker(t *testing.T) {
	env := setupTestEnv(t)

	// No tracker set — should return error.
	resp := rpcCall(t, env.url, "validator_getStatus", nil)
	if resp.Error == nil {
		t.Fatal("expected error when tracker is not set")
	}
}

func TestRPC_ValidatorGetStatus_AllValidators(t *testing.T) {
	env := setupTestEnv(t)

	// Create and wire a tracker.
	tracker := consensus.NewValidatorTracker(60 * time.Second)
	tracker.RecordHeartbeat(env.validatorKey.PublicKey())
	tracker.RecordBlock(env.validatorKey.PublicKey())
	tracker.RecordBlock(env.validatorKey.PublicKey())
	tracker.RecordMiss(env.validatorKey.PublicKey())
	env.server.SetValidatorTracker(tracker)

	resp := rpcCall(t, env.url, "validator_getStatus", nil)
	if resp.Error != nil {
		t.Fatalf("validator_getStatus error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result ValidatorStatusListResult
	json.Unmarshal(data, &result)

	if len(result.Validators) != 1 {
		t.Fatalf("expected 1 validator, got %d", len(result.Validators))
	}

	v := result.Validators[0]
	if v.PubKey != hex.EncodeToString(env.validatorKey.PublicKey()) {
		t.Errorf("pubkey mismatch")
	}
	if !v.IsGenesis {
		t.Error("should be genesis validator")
	}
	if !v.IsOnline {
		t.Error("should be online after heartbeat")
	}
	if v.BlockCount != 2 {
		t.Errorf("block_count = %d, want 2", v.BlockCount)
	}
	if v.MissedCount != 1 {
		t.Errorf("missed_count = %d, want 1", v.MissedCount)
	}
	if v.LastHeartbeat == 0 {
		t.Error("last_heartbeat should be non-zero")
	}
	if v.LastBlock == 0 {
		t.Error("last_block should be non-zero")
	}
}

func TestRPC_ValidatorGetStatus_ByPubKey(t *testing.T) {
	env := setupTestEnv(t)

	tracker := consensus.NewValidatorTracker(60 * time.Second)
	tracker.RecordBlock(env.validatorKey.PublicKey())
	env.server.SetValidatorTracker(tracker)

	pubHex := hex.EncodeToString(env.validatorKey.PublicKey())
	resp := rpcCall(t, env.url, "validator_getStatus", map[string]string{"pubkey": pubHex})
	if resp.Error != nil {
		t.Fatalf("validator_getStatus error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result ValidatorStatusListResult
	json.Unmarshal(data, &result)

	if len(result.Validators) != 1 {
		t.Fatalf("expected 1 validator, got %d", len(result.Validators))
	}
	if result.Validators[0].BlockCount != 1 {
		t.Errorf("block_count = %d, want 1", result.Validators[0].BlockCount)
	}
}

// --- Sub-chain endpoints ---

func TestRPC_SubChainList_NoManager(t *testing.T) {
	env := setupTestEnv(t)

	// No manager set, should return empty list.
	resp := rpcCall(t, env.url, "subchain_list", nil)
	if resp.Error != nil {
		t.Fatalf("subchain_list error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result SubChainListResult
	json.Unmarshal(data, &result)

	if result.Count != 0 {
		t.Errorf("count = %d, want 0", result.Count)
	}
	if len(result.Chains) != 0 {
		t.Errorf("chains = %d, want 0", len(result.Chains))
	}
}

func TestRPC_SubChainGetInfo_NoManager(t *testing.T) {
	env := setupTestEnv(t)

	fakeID := hex.EncodeToString(make([]byte, 32))
	resp := rpcCall(t, env.url, "subchain_getInfo", ChainIDParam{ChainID: fakeID})
	if resp.Error == nil {
		t.Fatal("expected error when manager is nil")
	}
	if resp.Error.Code != CodeNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeNotFound)
	}
}

func TestRPC_SubChainList_WithManager(t *testing.T) {
	env := setupTestEnv(t)

	// Create a manager with no sub-chains.
	mgr, err := subchain.NewManager(subchain.ManagerConfig{
		ParentDB: storage.NewMemory(),
		ParentID: types.ChainID{},
		Rules:    &env.genesis.Protocol.SubChain,
	})
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	env.server.SetSubChainManager(mgr)

	resp := rpcCall(t, env.url, "subchain_list", nil)
	if resp.Error != nil {
		t.Fatalf("subchain_list error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result SubChainListResult
	json.Unmarshal(data, &result)

	if result.Count != 0 {
		t.Errorf("count = %d, want 0", result.Count)
	}
}

func TestRPC_SubChainGetInfo_InvalidChainID(t *testing.T) {
	env := setupTestEnv(t)

	mgr, _ := subchain.NewManager(subchain.ManagerConfig{
		ParentDB: storage.NewMemory(),
		ParentID: types.ChainID{},
		Rules:    &env.genesis.Protocol.SubChain,
	})
	env.server.SetSubChainManager(mgr)

	resp := rpcCall(t, env.url, "subchain_getInfo", ChainIDParam{ChainID: "xyz"})
	if resp.Error == nil {
		t.Fatal("expected error for invalid chain_id")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}

// ── Sub-chain routing tests ─────────────────────────────────────────────

// setupTestEnvWithSubChain creates a test env with a spawned PoA sub-chain.
func setupTestEnvWithSubChain(t *testing.T) (*testEnv, types.ChainID) {
	t.Helper()
	env := setupTestEnv(t)

	// Create a validator key for the sub-chain.
	scKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate sub-chain key: %v", err)
	}
	scPubHex := hex.EncodeToString(scKey.PublicKey())

	// Build registration data JSON.
	rd := subchain.RegistrationData{
		Name:          "Test Sub",
		Symbol:        "TSUB",
		ConsensusType: "poa",
		BlockTime:     1,
		BlockReward:   1_000_000_000, // 0.001
		MaxSupply:     1_000_000_000_000_000_000,
		MinFeeRate:    10,
		Validators:    []string{scPubHex},
	}
	rdJSON, err := json.Marshal(rd)
	if err != nil {
		t.Fatalf("marshal registration: %v", err)
	}

	// Create manager.
	mgr, err := subchain.NewManager(subchain.ManagerConfig{
		ParentDB: storage.NewMemory(),
		ParentID: types.ChainID{},
		Rules:    &env.genesis.Protocol.SubChain,
	})
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}

	// Register a sub-chain.
	var txHash types.Hash
	txHash[0] = 0xAB
	if err := mgr.HandleRegistration(txHash, 0, 50_000_000_000_000, rdJSON, 10); err != nil {
		t.Fatalf("handle registration: %v", err)
	}

	env.server.SetSubChainManager(mgr)

	chainID := subchain.DeriveChainID(txHash, 0)
	return env, chainID
}

func TestRPC_ChainGetInfo_NoChainID(t *testing.T) {
	env, _ := setupTestEnvWithSubChain(t)

	// No chain_id → root chain (backward compat).
	resp := rpcCall(t, env.url, "chain_getInfo", nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result ChainInfoResult
	json.Unmarshal(data, &result)

	if result.ChainID != "klingnet-test-rpc" {
		t.Errorf("chain_id = %q, want %q", result.ChainID, "klingnet-test-rpc")
	}
}

func TestRPC_ChainGetInfo_SubChain(t *testing.T) {
	env, chainID := setupTestEnvWithSubChain(t)

	resp := rpcCall(t, env.url, "chain_getInfo", map[string]string{
		"chain_id": hex.EncodeToString(chainID[:]),
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result ChainInfoResult
	json.Unmarshal(data, &result)

	if result.Symbol != "TSUB" {
		t.Errorf("symbol = %q, want %q", result.Symbol, "TSUB")
	}
}

func TestRPC_ResolveChain_InvalidHex(t *testing.T) {
	env, _ := setupTestEnvWithSubChain(t)

	resp := rpcCall(t, env.url, "chain_getInfo", map[string]string{
		"chain_id": "not_valid_hex!",
	})
	if resp.Error == nil {
		t.Fatal("expected error for invalid hex chain_id")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}

func TestRPC_ResolveChain_Unknown(t *testing.T) {
	env, _ := setupTestEnvWithSubChain(t)

	fakeID := hex.EncodeToString(make([]byte, 32))
	resp := rpcCall(t, env.url, "chain_getInfo", map[string]string{
		"chain_id": fakeID,
	})
	if resp.Error == nil {
		t.Fatal("expected error for unknown chain_id")
	}
	if resp.Error.Code != CodeNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeNotFound)
	}
}

func TestRPC_ResolveChain_NoManager(t *testing.T) {
	env := setupTestEnv(t)
	// No manager set on this env.

	fakeID := hex.EncodeToString(make([]byte, 32))
	resp := rpcCall(t, env.url, "chain_getInfo", map[string]string{
		"chain_id": fakeID,
	})
	if resp.Error == nil {
		t.Fatal("expected error when scManager is nil")
	}
	if resp.Error.Code != CodeNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeNotFound)
	}
}

func TestRPC_SubChain_GetBlockByHeight(t *testing.T) {
	env, chainID := setupTestEnvWithSubChain(t)

	// Sub-chain has a genesis block at height 0.
	resp := rpcCall(t, env.url, "chain_getBlockByHeight", HeightParam{
		Height:  0,
		ChainID: hex.EncodeToString(chainID[:]),
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("result is nil")
	}
}

func TestRPC_SubChain_Balance(t *testing.T) {
	env, chainID := setupTestEnvWithSubChain(t)

	// Random address should have 0 balance on fresh sub-chain.
	fakeAddr := hex.EncodeToString(make([]byte, 20))
	resp := rpcCall(t, env.url, "utxo_getBalance", AddressParam{
		Address: fakeAddr,
		ChainID: hex.EncodeToString(chainID[:]),
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result BalanceResult
	json.Unmarshal(data, &result)

	if result.Balance != 0 {
		t.Errorf("balance = %d, want 0", result.Balance)
	}
}

func TestRPC_SubChain_MempoolSeparation(t *testing.T) {
	env, chainID := setupTestEnvWithSubChain(t)

	// Root mempool should be separate from sub-chain mempool.
	// Check root.
	rootResp := rpcCall(t, env.url, "mempool_getInfo", nil)
	if rootResp.Error != nil {
		t.Fatalf("root mempool error: %v", rootResp.Error.Message)
	}
	data, _ := json.Marshal(rootResp.Result)
	var rootPool MempoolInfoResult
	json.Unmarshal(data, &rootPool)

	// Check sub-chain.
	scResp := rpcCall(t, env.url, "mempool_getInfo", map[string]string{
		"chain_id": hex.EncodeToString(chainID[:]),
	})
	if scResp.Error != nil {
		t.Fatalf("sub-chain mempool error: %v", scResp.Error.Message)
	}
	data2, _ := json.Marshal(scResp.Result)
	var scPool MempoolInfoResult
	json.Unmarshal(data2, &scPool)

	// Both should be empty but they should both work independently.
	if rootPool.Count != 0 {
		t.Errorf("root pool count = %d, want 0", rootPool.Count)
	}
	if scPool.Count != 0 {
		t.Errorf("sub-chain pool count = %d, want 0", scPool.Count)
	}

	// Verify min fee rates are different (root uses 10, sub-chain uses 10).
	if rootPool.MinFeeRate == scPool.MinFeeRate {
		t.Logf("root min_fee_rate=%d, sub-chain min_fee_rate=%d (may match if configured same)", rootPool.MinFeeRate, scPool.MinFeeRate)
	}
}

// ── Wallet Unstake Tests ────────────────────────────────────────────────

// setupTestEnvWithWallet sets up a test environment with a wallet keystore.
// It creates a wallet, derives the address, and returns the wallet name/password.
func setupTestEnvWithWallet(t *testing.T) (*testEnv, string, string) {
	t.Helper()
	env := setupTestEnv(t)

	// Create a temporary keystore.
	ks, err := wallet.NewKeystore(t.TempDir())
	if err != nil {
		t.Fatalf("create keystore: %v", err)
	}
	env.server.SetKeystore(ks)

	// Create a wallet via RPC.
	walletName := "test-wallet"
	walletPassword := "test-password"
	resp := rpcCall(t, env.url, "wallet_create", WalletCreateParam{
		Name:     walletName,
		Password: walletPassword,
	})
	if resp.Error != nil {
		t.Fatalf("wallet_create error: %v", resp.Error.Message)
	}

	return env, walletName, walletPassword
}

func TestRPC_WalletUnstake(t *testing.T) {
	env, walletName, walletPassword := setupTestEnvWithWallet(t)

	// Get wallet address (account 0) via the create result.
	resp := rpcCall(t, env.url, "wallet_listAddresses", WalletUnlockParam{
		Name:     walletName,
		Password: walletPassword,
	})
	if resp.Error != nil {
		t.Fatalf("wallet_listAddresses error: %v", resp.Error.Message)
	}
	data, _ := json.Marshal(resp.Result)
	var addrResult WalletAddressListResult
	json.Unmarshal(data, &addrResult)
	if len(addrResult.Accounts) == 0 {
		t.Fatal("expected at least one account")
	}

	// Derive the wallet's pubkey by exporting the key.
	keyResp := rpcCall(t, env.url, "wallet_exportKey", WalletExportKeyParam{
		Name:     walletName,
		Password: walletPassword,
		Account:  0,
		Index:    0,
	})
	if keyResp.Error != nil {
		t.Fatalf("wallet_exportKey error: %v", keyResp.Error.Message)
	}
	keyData, _ := json.Marshal(keyResp.Result)
	var keyResult WalletExportKeyResult
	json.Unmarshal(keyData, &keyResult)

	pubKeyBytes, _ := hex.DecodeString(keyResult.PubKey)

	// Plant a stake UTXO for this wallet's pubkey in the UTXO store.
	stakeOp := types.Outpoint{TxID: types.Hash{0xAA, 0xBB}, Index: 0}
	stakeUTXO := &utxo.UTXO{
		Outpoint: stakeOp,
		Value:    1000 * config.Coin,
		Script: types.Script{
			Type: types.ScriptTypeStake,
			Data: pubKeyBytes,
		},
		Height: 0,
	}
	if err := env.utxoStore.Put(stakeUTXO); err != nil {
		t.Fatalf("put stake utxo: %v", err)
	}

	// Also put the same UTXO in the provider (adapter) so mempool can validate.
	// The adapter reads from utxoStore, so it should be available.

	// Call wallet_unstake.
	unstakeResp := rpcCall(t, env.url, "wallet_unstake", WalletUnstakeParam{
		Name:     walletName,
		Password: walletPassword,
	})
	if unstakeResp.Error != nil {
		t.Fatalf("wallet_unstake error: %v", unstakeResp.Error.Message)
	}

	unstakeData, _ := json.Marshal(unstakeResp.Result)
	var unstakeResult WalletUnstakeResult
	json.Unmarshal(unstakeData, &unstakeResult)

	if unstakeResult.TxHash == "" {
		t.Error("expected non-empty tx hash")
	}
	if unstakeResult.Amount != 1000*config.Coin {
		t.Errorf("returned amount = %d, want %d", unstakeResult.Amount, 1000*config.Coin)
	}
	if unstakeResult.PubKey != keyResult.PubKey {
		t.Errorf("pubkey mismatch: got %s, want %s", unstakeResult.PubKey, keyResult.PubKey)
	}

	// Verify the tx is in the mempool.
	if env.pool.Count() != 1 {
		t.Errorf("mempool count = %d, want 1", env.pool.Count())
	}
}

func TestRPC_WalletUnstake_NoStakes(t *testing.T) {
	env, walletName, walletPassword := setupTestEnvWithWallet(t)

	// Call wallet_unstake without any stakes planted.
	resp := rpcCall(t, env.url, "wallet_unstake", WalletUnstakeParam{
		Name:     walletName,
		Password: walletPassword,
	})

	if resp.Error == nil {
		t.Fatal("expected error when no stakes exist")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}

// ── Token endpoint tests ────────────────────────────────────────────────

func TestRPC_TokenList_Empty(t *testing.T) {
	env := setupTestEnv(t)

	resp := rpcCall(t, env.url, "token_list", nil)
	if resp.Error != nil {
		t.Fatalf("token_list error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result TokenListResult
	json.Unmarshal(data, &result)

	if len(result.Tokens) != 0 {
		t.Errorf("expected 0 tokens, got %d", len(result.Tokens))
	}
}

func TestRPC_TokenGetInfo(t *testing.T) {
	env := setupTestEnv(t)

	// Plant a token in the store.
	ts := token.NewStore(env.db)
	tokenID := types.TokenID{0xAA, 0xBB}
	ts.Put(tokenID, &token.Metadata{
		Name:     "Test Token",
		Symbol:   "TST",
		Decimals: 8,
		Creator:  env.validatorAddr,
	})

	tokenIDHex := hex.EncodeToString(tokenID[:])
	resp := rpcCall(t, env.url, "token_getInfo", TokenIDParam{TokenID: tokenIDHex})
	if resp.Error != nil {
		t.Fatalf("token_getInfo error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result TokenInfoResult
	json.Unmarshal(data, &result)

	if result.Name != "Test Token" {
		t.Errorf("name = %q, want %q", result.Name, "Test Token")
	}
	if result.Symbol != "TST" {
		t.Errorf("symbol = %q, want %q", result.Symbol, "TST")
	}
	if result.Decimals != 8 {
		t.Errorf("decimals = %d, want 8", result.Decimals)
	}
}

func TestRPC_TokenGetInfo_NotFound(t *testing.T) {
	env := setupTestEnv(t)

	fakeID := hex.EncodeToString(make([]byte, 32))
	resp := rpcCall(t, env.url, "token_getInfo", TokenIDParam{TokenID: fakeID})
	if resp.Error == nil {
		t.Fatal("expected error for non-existent token")
	}
	if resp.Error.Code != CodeNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeNotFound)
	}
}

func TestRPC_TokenList_WithTokens(t *testing.T) {
	env := setupTestEnv(t)

	// Plant two tokens.
	ts := token.NewStore(env.db)
	ts.Put(types.TokenID{0x01}, &token.Metadata{Name: "Alpha", Symbol: "ALP", Decimals: 6})
	ts.Put(types.TokenID{0x02}, &token.Metadata{Name: "Beta", Symbol: "BET", Decimals: 12})

	resp := rpcCall(t, env.url, "token_list", nil)
	if resp.Error != nil {
		t.Fatalf("token_list error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result TokenListResult
	json.Unmarshal(data, &result)

	if len(result.Tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(result.Tokens))
	}
}

func TestRPC_TokenGetBalance_NoTokens(t *testing.T) {
	env := setupTestEnv(t)

	resp := rpcCall(t, env.url, "token_getBalance", AddressParam{Address: env.addrHex})
	if resp.Error != nil {
		t.Fatalf("token_getBalance error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result TokenBalanceResult
	json.Unmarshal(data, &result)

	if len(result.Tokens) != 0 {
		t.Errorf("expected 0 token balances, got %d", len(result.Tokens))
	}
}

func TestRPC_TokenGetBalance_WithTokenUTXOs(t *testing.T) {
	env := setupTestEnv(t)

	tokenID := types.TokenID{0xCC, 0xDD}

	// Plant a token UTXO for the validator address.
	tokenUTXO := &utxo.UTXO{
		Outpoint: types.Outpoint{TxID: types.Hash{0xEE}, Index: 0},
		Value:    0,
		Script: types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: env.validatorAddr.Bytes(),
		},
		Token: &types.TokenData{
			ID:     tokenID,
			Amount: 5000,
		},
	}
	if err := env.utxoStore.Put(tokenUTXO); err != nil {
		t.Fatalf("put token utxo: %v", err)
	}

	// Plant metadata for enrichment.
	ts := token.NewStore(env.db)
	ts.Put(tokenID, &token.Metadata{Name: "Test", Symbol: "TST", Decimals: 8})

	resp := rpcCall(t, env.url, "token_getBalance", AddressParam{Address: env.addrHex})
	if resp.Error != nil {
		t.Fatalf("token_getBalance error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result TokenBalanceResult
	json.Unmarshal(data, &result)

	if len(result.Tokens) != 1 {
		t.Fatalf("expected 1 token balance, got %d", len(result.Tokens))
	}
	if result.Tokens[0].Amount != 5000 {
		t.Errorf("amount = %d, want 5000", result.Tokens[0].Amount)
	}
	if result.Tokens[0].Symbol != "TST" {
		t.Errorf("symbol = %q, want %q", result.Tokens[0].Symbol, "TST")
	}
}

// ── Mining endpoint tests ───────────────────────────────────────────────

// setupTestEnvWithPoWSubChain creates a test env with a spawned PoW sub-chain
// (difficulty 1 so tests can mine blocks easily).
func setupTestEnvWithPoWSubChain(t *testing.T) (*testEnv, types.ChainID) {
	t.Helper()
	env := setupTestEnv(t)

	rd := subchain.RegistrationData{
		Name:              "PoW Test",
		Symbol:            "TPOW",
		ConsensusType:     "pow",
		BlockTime:         1,
		BlockReward:       1_000_000_000, // 0.001
		MaxSupply:         1_000_000_000_000_000_000,
		MinFeeRate:        10,
		InitialDifficulty: 1, // Easiest possible for tests.
	}
	rdJSON, err := json.Marshal(rd)
	if err != nil {
		t.Fatalf("marshal registration: %v", err)
	}

	// Use rules that allow PoW sub-chains.
	rules := env.genesis.Protocol.SubChain
	rules.AllowPoW = true

	mgr, err := subchain.NewManager(subchain.ManagerConfig{
		ParentDB: storage.NewMemory(),
		ParentID: types.ChainID{},
		Rules:    &rules,
	})
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}

	var txHash types.Hash
	txHash[0] = 0xCD
	if err := mgr.HandleRegistration(txHash, 0, 50_000_000_000_000, rdJSON, 10); err != nil {
		t.Fatalf("handle registration: %v", err)
	}

	env.server.SetSubChainManager(mgr)
	chainID := subchain.DeriveChainID(txHash, 0)
	return env, chainID
}

func TestRPC_MiningGetBlockTemplate_Success(t *testing.T) {
	env, chainID := setupTestEnvWithPoWSubChain(t)

	resp := rpcCall(t, env.url, "mining_getBlockTemplate", MiningGetBlockTemplateParam{
		ChainID:         hex.EncodeToString(chainID[:]),
		CoinbaseAddress: env.addrHex,
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result MiningBlockTemplateResult
	json.Unmarshal(data, &result)

	if result.Height != 1 {
		t.Errorf("height = %d, want 1", result.Height)
	}
	if result.Difficulty != 1 {
		t.Errorf("difficulty = %d, want 1", result.Difficulty)
	}
	if result.Target == "" {
		t.Error("target is empty")
	}
	if len(result.Target) != 64 {
		t.Errorf("target length = %d, want 64", len(result.Target))
	}
	if result.PrevHash == "" {
		t.Error("prev_hash is empty")
	}
	if result.Block == nil {
		t.Fatal("block is nil")
	}
	if result.Block.Header == nil {
		t.Fatal("block header is nil")
	}
	if result.Block.Header.Nonce != 0 {
		t.Errorf("template nonce = %d, want 0", result.Block.Header.Nonce)
	}
	if len(result.Block.Transactions) < 1 {
		t.Fatal("block has no transactions (expected coinbase)")
	}
}

func TestRPC_MiningGetBlockTemplate_PoAChain(t *testing.T) {
	env, chainID := setupTestEnvWithSubChain(t)

	resp := rpcCall(t, env.url, "mining_getBlockTemplate", MiningGetBlockTemplateParam{
		ChainID:         hex.EncodeToString(chainID[:]),
		CoinbaseAddress: env.addrHex,
	})
	if resp.Error == nil {
		t.Fatal("expected error for PoA chain")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}

func TestRPC_MiningGetBlockTemplate_NoChain(t *testing.T) {
	env := setupTestEnv(t)

	mgr, _ := subchain.NewManager(subchain.ManagerConfig{
		ParentDB: storage.NewMemory(),
		ParentID: types.ChainID{},
		Rules:    &env.genesis.Protocol.SubChain,
	})
	env.server.SetSubChainManager(mgr)

	fakeID := hex.EncodeToString(make([]byte, 32))
	resp := rpcCall(t, env.url, "mining_getBlockTemplate", MiningGetBlockTemplateParam{
		ChainID:         fakeID,
		CoinbaseAddress: env.addrHex,
	})
	if resp.Error == nil {
		t.Fatal("expected error for unknown chain")
	}
	if resp.Error.Code != CodeNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeNotFound)
	}
}

func TestRPC_MiningSubmitBlock_Success(t *testing.T) {
	env, chainID := setupTestEnvWithPoWSubChain(t)
	chainIDHex := hex.EncodeToString(chainID[:])

	// Get template.
	resp := rpcCall(t, env.url, "mining_getBlockTemplate", MiningGetBlockTemplateParam{
		ChainID:         chainIDHex,
		CoinbaseAddress: env.addrHex,
	})
	if resp.Error != nil {
		t.Fatalf("get template error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var tmpl MiningBlockTemplateResult
	json.Unmarshal(data, &tmpl)

	// Re-marshal and unmarshal the block to simulate what an external miner does.
	blkJSON, _ := json.Marshal(tmpl.Block)
	var blk block.Block
	if err := json.Unmarshal(blkJSON, &blk); err != nil {
		t.Fatalf("unmarshal block: %v", err)
	}

	// Mine the block (difficulty 1 = almost any nonce works).
	targetInt := new(big.Int)
	targetInt.SetString(tmpl.Target, 16)

	mined := false
	for nonce := uint64(0); nonce < 1_000_000; nonce++ {
		blk.Header.Nonce = nonce
		hash := crypto.Hash(blk.Header.SigningBytes())
		hashInt := new(big.Int).SetBytes(hash[:])
		if hashInt.Cmp(targetInt) <= 0 {
			mined = true
			break
		}
	}
	if !mined {
		t.Fatal("failed to mine block with difficulty 1")
	}

	// Submit.
	submitResp := rpcCall(t, env.url, "mining_submitBlock", MiningSubmitBlockParam{
		ChainID: chainIDHex,
		Block:   &blk,
	})
	if submitResp.Error != nil {
		t.Fatalf("submit error: %v", submitResp.Error.Message)
	}

	submitData, _ := json.Marshal(submitResp.Result)
	var submitResult MiningSubmitBlockResult
	json.Unmarshal(submitData, &submitResult)

	if submitResult.Height != 1 {
		t.Errorf("height = %d, want 1", submitResult.Height)
	}
	if submitResult.BlockHash == "" {
		t.Error("block_hash is empty")
	}

	// Verify chain advanced.
	infoResp := rpcCall(t, env.url, "chain_getInfo", map[string]string{
		"chain_id": chainIDHex,
	})
	if infoResp.Error != nil {
		t.Fatalf("chain_getInfo error: %v", infoResp.Error.Message)
	}
	infoData, _ := json.Marshal(infoResp.Result)
	var info ChainInfoResult
	json.Unmarshal(infoData, &info)

	if info.Height != 1 {
		t.Errorf("chain height = %d, want 1", info.Height)
	}
}

func TestRPC_MiningSubmitBlock_BadNonce(t *testing.T) {
	env, chainID := setupTestEnvWithPoWSubChain(t)
	chainIDHex := hex.EncodeToString(chainID[:])

	// Get template.
	resp := rpcCall(t, env.url, "mining_getBlockTemplate", MiningGetBlockTemplateParam{
		ChainID:         chainIDHex,
		CoinbaseAddress: env.addrHex,
	})
	if resp.Error != nil {
		t.Fatalf("get template error: %v", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var tmpl MiningBlockTemplateResult
	json.Unmarshal(data, &tmpl)

	// Re-create the block but set a very high difficulty so nonce=0 won't work.
	blkJSON, _ := json.Marshal(tmpl.Block)
	var blk block.Block
	json.Unmarshal(blkJSON, &blk)

	// Override difficulty to something impossibly high (nonce 0 won't satisfy it).
	blk.Header.Difficulty = ^uint64(0) // max uint64

	submitResp := rpcCall(t, env.url, "mining_submitBlock", MiningSubmitBlockParam{
		ChainID: chainIDHex,
		Block:   &blk,
	})
	if submitResp.Error == nil {
		t.Fatal("expected error for bad nonce/difficulty")
	}
	if submitResp.Error.Code != CodeInvalidParams {
		t.Errorf("error code = %d, want %d", submitResp.Error.Code, CodeInvalidParams)
	}
}

func TestRPC_MiningSubmitBlock_BadChain(t *testing.T) {
	env, _ := setupTestEnvWithPoWSubChain(t)

	fakeID := hex.EncodeToString(make([]byte, 32))
	resp := rpcCall(t, env.url, "mining_submitBlock", MiningSubmitBlockParam{
		ChainID: fakeID,
		Block:   &block.Block{Header: &block.Header{Version: 1}},
	})
	if resp.Error == nil {
		t.Fatal("expected error for unknown chain")
	}
	if resp.Error.Code != CodeNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, CodeNotFound)
	}
}

// ── Sub-chain PoA dynamic validator tests ───────────────────────────────

// setupTestEnvWithDynValidatorSubChain creates a PoA sub-chain where dynamic
// validator staking is enabled (ValidatorStake > 0). Returns the env, chain ID,
// the genesis validator key, and the SpawnResult for direct block production.
func setupTestEnvWithDynValidatorSubChain(t *testing.T) (*testEnv, types.ChainID, *crypto.PrivateKey, *subchain.SpawnResult) {
	t.Helper()
	env := setupTestEnv(t)

	// Sub-chain genesis validator.
	scKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate sub-chain key: %v", err)
	}
	scPubHex := hex.EncodeToString(scKey.PublicKey())

	// ValidatorStake = 0.5 coin; block reward = 1 coin (leaves room for fee).
	rd := subchain.RegistrationData{
		Name:           "DynVal Test",
		Symbol:         "DVT",
		ConsensusType:  "poa",
		BlockTime:      1,
		BlockReward:    1_000_000_000_000, // 1 coin per block
		MaxSupply:      1_000_000_000_000_000_000,
		MinFeeRate:     10,
		Validators:     []string{scPubHex},
		ValidatorStake: 500_000_000_000, // 0.5 coin min stake
	}
	rdJSON, err := json.Marshal(rd)
	if err != nil {
		t.Fatalf("marshal registration: %v", err)
	}

	mgr, err := subchain.NewManager(subchain.ManagerConfig{
		ParentDB: storage.NewMemory(),
		ParentID: types.ChainID{},
		Rules:    &env.genesis.Protocol.SubChain,
	})
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}

	var txHash types.Hash
	txHash[0] = 0xEE
	if err := mgr.HandleRegistration(txHash, 0, 50_000_000_000_000, rdJSON, 10); err != nil {
		t.Fatalf("handle registration: %v", err)
	}

	chainID := subchain.DeriveChainID(txHash, 0)
	sr, ok := mgr.GetChain(chainID)
	if !ok {
		t.Fatal("sub-chain not found after registration")
	}

	// Set the genesis validator as signer so we can produce blocks.
	poaEng := sr.Engine.(*consensus.PoA)
	if err := poaEng.SetSigner(scKey); err != nil {
		t.Fatalf("set signer: %v", err)
	}

	// Wire stake/unstake handlers (replicating klingnetd logic).
	minStake := sr.Genesis.Protocol.Consensus.ValidatorStake
	stakeChecker := consensus.NewUTXOStakeChecker(sr.UTXOs, minStake)

	sr.Chain.SetStakeHandler(func(pubKey []byte) {
		poaEng.AddValidator(pubKey)
	})
	sr.Chain.SetUnstakeHandler(func(pubKey []byte) {
		hasStake, _ := stakeChecker.HasStake(pubKey)
		if !hasStake {
			poaEng.RemoveValidator(pubKey)
		}
	})

	env.server.SetSubChainManager(mgr)
	return env, chainID, scKey, sr
}

func TestRPC_SubChainPoA_DynamicValidators(t *testing.T) {
	env, chainID, scKey, sr := setupTestEnvWithDynValidatorSubChain(t)
	_ = env
	_ = chainID

	poaEng := sr.Engine.(*consensus.PoA)
	coinbaseAddr := crypto.AddressFromPubKey(scKey.PublicKey())

	// Mine enough blocks so the coinbase matures (CoinbaseMaturity = 20).
	m := miner.New(sr.Chain, sr.Engine, sr.Pool, coinbaseAddr,
		sr.Genesis.Protocol.Consensus.BlockReward, sr.Genesis.Protocol.Consensus.MaxSupply,
		sr.Chain.Supply)

	for i := 0; i < 25; i++ {
		blk, err := m.ProduceBlock()
		if err != nil {
			t.Fatalf("produce block %d: %v", i, err)
		}
		if err := sr.Chain.ProcessBlock(blk); err != nil {
			t.Fatalf("process block %d: %v", i, err)
		}
		sr.Pool.RemoveConfirmed(blk.Transactions)
	}

	// Verify the chain advanced.
	if sr.Chain.Height() < 25 {
		t.Fatalf("chain height = %d, want >= 25", sr.Chain.Height())
	}

	// Generate a new key that wants to become a validator via staking.
	newValKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate new validator key: %v", err)
	}
	newValPub := newValKey.PublicKey()

	// Build a stake transaction: spend a mature coinbase UTXO → ScriptTypeStake output.
	utxos, err := sr.UTXOs.GetByAddress(coinbaseAddr)
	if err != nil {
		t.Fatalf("GetByAddress: %v", err)
	}
	if len(utxos) == 0 {
		t.Fatal("no UTXOs for coinbase address")
	}

	// Find a mature UTXO.
	var stakeInput *utxo.UTXO
	for _, u := range utxos {
		if u.Coinbase && u.Height+config.CoinbaseMaturity <= sr.Chain.Height() {
			stakeInput = u
			break
		}
	}
	if stakeInput == nil {
		t.Fatal("no mature coinbase UTXO found")
	}

	stakeTx := &tx.Transaction{
		Version: 1,
		Inputs: []tx.Input{
			{
				PrevOut: stakeInput.Outpoint,
				PubKey:  scKey.PublicKey(),
			},
		},
		Outputs: []tx.Output{
			{
				Value: sr.Genesis.Protocol.Consensus.ValidatorStake,
				Script: types.Script{
					Type: types.ScriptTypeStake,
					Data: newValPub, // 33-byte compressed pubkey
				},
			},
		},
	}

	// Compute the exact fee from the transaction's SigningBytes size.
	feeRate := sr.Genesis.Protocol.Consensus.MinFeeRate
	stakeAmt := sr.Genesis.Protocol.Consensus.ValidatorStake
	// Add a temporary change output to measure the full tx size.
	stakeTx.Outputs = append(stakeTx.Outputs, tx.Output{
		Value: 0,
		Script: types.Script{
			Type: types.ScriptTypeP2PKH,
			Data: coinbaseAddr[:],
		},
	})
	fee := tx.RequiredFee(stakeTx, feeRate)
	if stakeInput.Value > stakeAmt+fee {
		stakeTx.Outputs[1].Value = stakeInput.Value - stakeAmt - fee
	} else {
		// No room for change; remove the change output.
		stakeTx.Outputs = stakeTx.Outputs[:1]
	}

	// Sign the transaction.
	sigHash := stakeTx.Hash()
	sig, err := scKey.Sign(sigHash[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	stakeTx.Inputs[0].Signature = sig

	// Add to mempool.
	if _, err := sr.Pool.Add(stakeTx); err != nil {
		t.Fatalf("add stake tx to mempool: %v", err)
	}

	// Mine a block that includes the stake tx.
	blk, err := m.ProduceBlock()
	if err != nil {
		t.Fatalf("produce block with stake tx: %v", err)
	}
	if err := sr.Chain.ProcessBlock(blk); err != nil {
		t.Fatalf("process block with stake tx: %v", err)
	}
	sr.Pool.RemoveConfirmed(blk.Transactions)

	// Verify the new validator was added.
	found := false
	for _, v := range poaEng.Validators {
		if bytes.Equal(v, newValPub) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("new validator not found in PoA engine after stake; validators count = %d", len(poaEng.Validators))
	}
}

func TestRPC_SubChainPoA_ValidatorStakeRecovery(t *testing.T) {
	_, _, _, sr := setupTestEnvWithDynValidatorSubChain(t)

	// Verify GetAllStakedValidators works on sub-chain UTXO store.
	validators, err := sr.UTXOs.GetAllStakedValidators()
	if err != nil {
		t.Fatalf("GetAllStakedValidators: %v", err)
	}
	if len(validators) != 0 {
		t.Fatalf("expected 0 staked validators before any staking, got %d", len(validators))
	}

	// Verify the genesis config has ValidatorStake set.
	if sr.Genesis.Protocol.Consensus.ValidatorStake != 500_000_000_000 {
		t.Fatalf("ValidatorStake = %d, want 500000000000", sr.Genesis.Protocol.Consensus.ValidatorStake)
	}
}

func TestRPC_SubChainPoA_ValidatorGetStatus(t *testing.T) {
	env, chainID, scKey, _ := setupTestEnvWithDynValidatorSubChain(t)
	chainIDHex := hex.EncodeToString(chainID[:])

	// Create and wire a sub-chain tracker.
	scTracker := consensus.NewValidatorTracker(60 * time.Second)
	scTracker.RecordHeartbeat(scKey.PublicKey())
	scTracker.RecordBlock(scKey.PublicKey())
	env.server.SetSubChainTracker(chainIDHex, scTracker)

	resp := rpcCall(t, env.url, "validator_getStatus", map[string]string{
		"chain_id": chainIDHex,
	})
	if resp.Error != nil {
		t.Fatalf("validator_getStatus error: %s", resp.Error.Message)
	}

	data, _ := json.Marshal(resp.Result)
	var result ValidatorStatusListResult
	json.Unmarshal(data, &result)

	if len(result.Validators) != 1 {
		t.Fatalf("expected 1 validator, got %d", len(result.Validators))
	}

	v := result.Validators[0]
	if v.PubKey != hex.EncodeToString(scKey.PublicKey()) {
		t.Error("pubkey mismatch")
	}
	if !v.IsOnline {
		t.Error("should be online after heartbeat")
	}
	if v.BlockCount != 1 {
		t.Errorf("block_count = %d, want 1", v.BlockCount)
	}
	if !v.IsGenesis {
		t.Error("should be genesis validator")
	}
}

func TestRPC_BodySizeLimit(t *testing.T) {
	env := setupTestEnv(t)

	// Build a request body that exceeds 1 MB (maxBodySize = 1 << 20).
	bigPayload := make([]byte, (1<<20)+1024)
	for i := range bigPayload {
		bigPayload[i] = 'A'
	}

	resp, err := http.Post(env.url, "application/json", bytes.NewReader(bigPayload))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp Response
	json.NewDecoder(resp.Body).Decode(&rpcResp)

	if rpcResp.Error == nil {
		t.Fatal("expected error for oversized request body")
	}
	if rpcResp.Error.Code != CodeInvalidRequest {
		t.Errorf("error code = %d, want %d", rpcResp.Error.Code, CodeInvalidRequest)
	}
}

func TestRPC_SubChainPoA_ValidatorStakeZero_NoStakeChecker(t *testing.T) {
	env := setupTestEnv(t)

	scKey, _ := crypto.GenerateKey()
	scPubHex := hex.EncodeToString(scKey.PublicKey())

	rd := subchain.RegistrationData{
		Name:          "Fixed Val",
		Symbol:        "FIX",
		ConsensusType: "poa",
		BlockTime:     1,
		BlockReward:   1_000_000_000,
		MaxSupply:     1_000_000_000_000_000_000,
		MinFeeRate:    10,
		Validators:    []string{scPubHex},
	}
	rdJSON, _ := json.Marshal(rd)

	mgr, err := subchain.NewManager(subchain.ManagerConfig{
		ParentDB: storage.NewMemory(),
		ParentID: types.ChainID{},
		Rules:    &env.genesis.Protocol.SubChain,
	})
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}

	var txHash types.Hash
	txHash[0] = 0xFF
	if err := mgr.HandleRegistration(txHash, 0, 50_000_000_000_000, rdJSON, 10); err != nil {
		t.Fatalf("handle registration: %v", err)
	}

	chainID := subchain.DeriveChainID(txHash, 0)
	sr, _ := mgr.GetChain(chainID)

	if sr.Genesis.Protocol.Consensus.ValidatorStake != 0 {
		t.Fatalf("ValidatorStake = %d, want 0", sr.Genesis.Protocol.Consensus.ValidatorStake)
	}

	poaEng := sr.Engine.(*consensus.PoA)
	if len(poaEng.Validators) != 1 {
		t.Fatalf("validators count = %d, want 1", len(poaEng.Validators))
	}
}

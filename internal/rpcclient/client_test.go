package rpcclient

import (
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/chain"
	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	klog "github.com/Klingon-tech/klingnet-chain/internal/log"
	"github.com/Klingon-tech/klingnet-chain/internal/mempool"
	"github.com/Klingon-tech/klingnet-chain/internal/miner"
	"github.com/Klingon-tech/klingnet-chain/internal/rpc"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

type testEnv struct {
	client        *Client
	chain         *chain.Chain
	utxoStore     *utxo.Store
	genesis       *config.Genesis
	validatorAddr types.Address
	addrHex       string
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
		ChainID:   "klingnet-test-client",
		ChainName: "Client Test",
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
	srv := rpc.New("127.0.0.1:0", ch, utxoStore, pool, nil, gen, engine)
	if err := srv.Start(); err != nil {
		t.Fatalf("start rpc: %v", err)
	}
	t.Cleanup(func() { srv.Stop() })

	url := "http://" + srv.Addr() + "/"
	client := New(url)

	return &testEnv{
		client:        client,
		chain:         ch,
		utxoStore:     utxoStore,
		genesis:       gen,
		validatorAddr: validatorAddr,
		addrHex:       addrHex,
	}
}

func TestClient_ChainGetInfo(t *testing.T) {
	env := setupTestEnv(t)

	var result rpc.ChainInfoResult
	if err := env.client.Call("chain_getInfo", nil, &result); err != nil {
		t.Fatalf("Call error: %v", err)
	}

	if result.ChainID != "klingnet-test-client" {
		t.Errorf("chain_id = %q, want %q", result.ChainID, "klingnet-test-client")
	}
	if result.Height != 0 {
		t.Errorf("height = %d, want 0", result.Height)
	}
	if result.TipHash == "" {
		t.Error("tip_hash is empty")
	}
}

func TestClient_GetBlockByHeight(t *testing.T) {
	env := setupTestEnv(t)

	var raw json.RawMessage
	if err := env.client.Call("chain_getBlockByHeight", rpc.HeightParam{Height: 0}, &raw); err != nil {
		t.Fatalf("Call error: %v", err)
	}

	// Verify we got a block with transactions.
	var blk block.Block
	if err := json.Unmarshal(raw, &blk); err != nil {
		t.Fatalf("unmarshal block: %v", err)
	}
	if blk.Header.Height != 0 {
		t.Errorf("height = %d, want 0", blk.Header.Height)
	}
	if len(blk.Transactions) == 0 {
		t.Error("genesis block has no transactions")
	}
}

func TestClient_GetBalance(t *testing.T) {
	env := setupTestEnv(t)

	var result rpc.BalanceResult
	if err := env.client.Call("utxo_getBalance", rpc.AddressParam{Address: env.addrHex}, &result); err != nil {
		t.Fatalf("Call error: %v", err)
	}

	expected := uint64(100_000) * config.Coin
	if result.Balance != expected {
		t.Errorf("balance = %d, want %d", result.Balance, expected)
	}
}

func TestClient_GetBlockByHash_NotFound(t *testing.T) {
	env := setupTestEnv(t)

	fakeHash := hex.EncodeToString(make([]byte, 32))
	var raw json.RawMessage
	err := env.client.Call("chain_getBlockByHash", rpc.HashParam{Hash: fakeHash}, &raw)
	if err == nil {
		t.Fatal("expected error for non-existent block")
	}

	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("expected RPCError, got %T: %v", err, err)
	}
	if rpcErr.Code != -32000 {
		t.Errorf("error code = %d, want -32000", rpcErr.Code)
	}
}

func TestClient_Call_InvalidEndpoint(t *testing.T) {
	client := New("http://127.0.0.1:1/") // port 1 — should refuse

	var result rpc.ChainInfoResult
	err := client.Call("chain_getInfo", nil, &result)
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestClient_Call_MethodNotFound(t *testing.T) {
	env := setupTestEnv(t)

	var raw json.RawMessage
	err := env.client.Call("nonexistent_method", nil, &raw)
	if err == nil {
		t.Fatal("expected error for unknown method")
	}

	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("expected RPCError, got %T: %v", err, err)
	}
	if rpcErr.Code != -32601 {
		t.Errorf("error code = %d, want -32601", rpcErr.Code)
	}
}

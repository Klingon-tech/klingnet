package p2p

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
	"github.com/libp2p/go-libp2p/core/peer"
)

// --- Config ---

func TestConfig_Defaults(t *testing.T) {
	cfg := Config{
		ListenAddr: "0.0.0.0",
		Port:       0,
		MaxPeers:   50,
	}
	if cfg.ListenAddr != "0.0.0.0" {
		t.Error("bad default listen addr")
	}
}

// --- Node Lifecycle ---

func TestNode_New(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	if n == nil {
		t.Fatal("New returned nil")
	}
	if n.host != nil {
		t.Error("host should be nil before Start")
	}
	if n.ID() != "" {
		t.Error("ID should be empty before Start")
	}
	if n.Addrs() != nil {
		t.Error("Addrs should be nil before Start")
	}
}

func TestNode_StartStop(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0, NoDiscover: true})

	if err := n.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if n.host == nil {
		t.Fatal("host should not be nil after Start")
	}
	if n.ID() == "" {
		t.Error("ID should not be empty after Start")
	}
	if len(n.Addrs()) == 0 {
		t.Error("should have at least one address")
	}

	if err := n.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestNode_StopBeforeStart(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	if err := n.Stop(); err != nil {
		t.Fatalf("Stop before Start should not error: %v", err)
	}
}

// --- Peer Management ---

func TestNode_PeerCount_Empty(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	if n.PeerCount() != 0 {
		t.Error("empty node should have 0 peers")
	}
}

func TestNode_AddRemovePeer(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	fakeID := peer.ID("test-peer-1")

	n.addPeer(fakeID)
	if n.PeerCount() != 1 {
		t.Errorf("expected 1 peer, got %d", n.PeerCount())
	}

	// Adding same peer again should not duplicate.
	n.addPeer(fakeID)
	if n.PeerCount() != 1 {
		t.Errorf("expected 1 peer after dup, got %d", n.PeerCount())
	}

	n.removePeer(fakeID)
	if n.PeerCount() != 0 {
		t.Errorf("expected 0 peers after remove, got %d", n.PeerCount())
	}
}

func TestNode_PeerList(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	n.addPeer(peer.ID("a"))
	n.addPeer(peer.ID("b"))

	list := n.PeerList()
	if len(list) != 2 {
		t.Errorf("expected 2 peers, got %d", len(list))
	}
}

// --- Handlers ---

func TestNode_SetTxHandler(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})

	n.SetTxHandler(func(from peer.ID, data []byte) {})

	if n.txHandler == nil {
		t.Error("txHandler should be set")
	}
}

func TestNode_SetBlockHandler(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})

	n.SetBlockHandler(func(from peer.ID, data []byte) {})

	if n.blockHandler == nil {
		t.Error("blockHandler should be set")
	}
}

// --- Rendezvous ---

func TestNode_Rendezvous_WithNetworkID(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0, NetworkID: "klingnet-mainnet-1"})
	want := "klingnet/klingnet-mainnet-1"
	if got := n.rendezvous(); got != want {
		t.Errorf("rendezvous() = %q, want %q", got, want)
	}
}

func TestNode_Rendezvous_Empty(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	want := "klingnet-chain"
	if got := n.rendezvous(); got != want {
		t.Errorf("rendezvous() = %q, want %q", got, want)
	}
}

// --- Protocol Constants ---

func TestTopicNames(t *testing.T) {
	if TopicTransactions == "" {
		t.Error("TopicTransactions should not be empty")
	}
	if TopicBlocks == "" {
		t.Error("TopicBlocks should not be empty")
	}
	if TopicTransactions == TopicBlocks {
		t.Error("topics should be different")
	}
}

func TestMessageTypes(t *testing.T) {
	if MsgTx == 0 {
		t.Error("MsgTx should not be zero")
	}
	if MsgBlock == 0 {
		t.Error("MsgBlock should not be zero")
	}
	if MsgTx == MsgBlock {
		t.Error("message types should differ")
	}
}

// --- BroadcastTx/Block before Start ---

func TestNode_BroadcastTx_NotStarted(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	err := n.BroadcastTx(&tx.Transaction{Version: 1})
	if err == nil {
		t.Error("BroadcastTx should fail before Start")
	}
}

func TestNode_BroadcastBlock_NotStarted(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	err := n.BroadcastBlock(&block.Block{
		Header: &block.Header{Version: 1},
	})
	if err == nil {
		t.Error("BroadcastBlock should fail before Start")
	}
}

// --- Sub-chain topic helpers ---

func TestSubChainBlockTopic(t *testing.T) {
	want := "/klingnet/sc/abcdef/block/1.0.0"
	got := SubChainBlockTopic("abcdef")
	if got != want {
		t.Errorf("SubChainBlockTopic = %q, want %q", got, want)
	}
}

func TestSubChainTxTopic(t *testing.T) {
	want := "/klingnet/sc/abcdef/tx/1.0.0"
	got := SubChainTxTopic("abcdef")
	if got != want {
		t.Errorf("SubChainTxTopic = %q, want %q", got, want)
	}
}

// --- Sub-chain P2P methods (before Start) ---

func TestNode_JoinSubChain_NotStarted(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	err := n.JoinSubChain("deadbeef")
	if err == nil {
		t.Error("JoinSubChain should fail before Start")
	}
}

func TestNode_BroadcastSubChainBlock_NotJoined(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	err := n.BroadcastSubChainBlock("deadbeef", &block.Block{Header: &block.Header{Version: 1}})
	if err == nil {
		t.Error("BroadcastSubChainBlock should fail when not joined")
	}
}

func TestNode_BroadcastSubChainTx_NotJoined(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	err := n.BroadcastSubChainTx("deadbeef", &tx.Transaction{Version: 1})
	if err == nil {
		t.Error("BroadcastSubChainTx should fail when not joined")
	}
}

func TestNode_LeaveSubChain_Noop(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	// LeaveSubChain on a never-joined chain should not panic.
	n.LeaveSubChain("nonexistent")
}

func TestNode_JoinSubChain_Idempotent(t *testing.T) {
	n := startTestNode(t)
	defer n.Stop()

	chainID := "aabbccdd"
	if err := n.JoinSubChain(chainID); err != nil {
		t.Fatalf("first JoinSubChain: %v", err)
	}
	// Second join should be a no-op (idempotent).
	if err := n.JoinSubChain(chainID); err != nil {
		t.Fatalf("second JoinSubChain: %v", err)
	}

	// Clean up.
	n.LeaveSubChain(chainID)
}

func TestNode_JoinAndLeaveSubChain(t *testing.T) {
	n := startTestNode(t)
	defer n.Stop()

	chainID := "11223344"
	if err := n.JoinSubChain(chainID); err != nil {
		t.Fatalf("JoinSubChain: %v", err)
	}

	// Should be able to broadcast after joining.
	err := n.BroadcastSubChainBlock(chainID, &block.Block{
		Header:       &block.Header{Version: 1},
		Transactions: []*tx.Transaction{},
	})
	if err != nil {
		t.Fatalf("BroadcastSubChainBlock after join: %v", err)
	}

	// Leave.
	n.LeaveSubChain(chainID)

	// Broadcast should fail after leaving.
	err = n.BroadcastSubChainBlock(chainID, &block.Block{
		Header:       &block.Header{Version: 1},
		Transactions: []*tx.Transaction{},
	})
	if err == nil {
		t.Error("BroadcastSubChainBlock should fail after LeaveSubChain")
	}
}

// --- Two-Node Gossip Integration Tests ---

// startTestNode creates, starts, and returns a P2P node on a random port.
func startTestNode(t *testing.T) *Node {
	t.Helper()
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0, NoDiscover: true})
	if err := n.Start(); err != nil {
		t.Fatalf("start node: %v", err)
	}
	t.Cleanup(func() { n.Stop() })
	return n
}

// connectNodes connects node B to node A via direct libp2p connect.
func connectNodes(t *testing.T, a, b *Node) {
	t.Helper()
	aInfo := peer.AddrInfo{
		ID:    a.host.ID(),
		Addrs: a.host.Addrs(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.host.Connect(ctx, aInfo); err != nil {
		t.Fatalf("connect nodes: %v", err)
	}
	a.addPeer(b.host.ID())
	b.addPeer(a.host.ID())

	// Give GossipSub time to establish mesh.
	time.Sleep(200 * time.Millisecond)
}

func TestTwoNodes_TxGossip(t *testing.T) {
	nodeA := startTestNode(t)
	nodeB := startTestNode(t)
	connectNodes(t, nodeA, nodeB)

	// Set up handler on B to receive txs.
	var received atomic.Value
	nodeB.SetTxHandler(func(_ peer.ID, data []byte) {
		var txn tx.Transaction
		if err := json.Unmarshal(data, &txn); err == nil {
			received.Store(&txn)
		}
	})

	// Give mesh time to stabilize.
	time.Sleep(300 * time.Millisecond)

	// Broadcast tx from A.
	testTx := &tx.Transaction{
		Version: 1,
		Inputs:  []tx.Input{{PrevOut: types.Outpoint{TxID: types.Hash{0xaa}, Index: 0}}},
		Outputs: []tx.Output{{Value: 5000, Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}}},
	}

	if err := nodeA.BroadcastTx(testTx); err != nil {
		t.Fatalf("BroadcastTx: %v", err)
	}

	// Wait for delivery.
	deadline := time.After(5 * time.Second)
	for {
		if v := received.Load(); v != nil {
			rxTx := v.(*tx.Transaction)
			if rxTx.Version != 1 || len(rxTx.Outputs) != 1 || rxTx.Outputs[0].Value != 5000 {
				t.Errorf("received tx mismatch: %+v", rxTx)
			}
			return // Success!
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for tx gossip")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func TestTwoNodes_BlockGossip(t *testing.T) {
	nodeA := startTestNode(t)
	nodeB := startTestNode(t)
	connectNodes(t, nodeA, nodeB)

	// Set up handler on B to receive blocks.
	var received atomic.Value
	nodeB.SetBlockHandler(func(_ peer.ID, data []byte) {
		var blk block.Block
		if err := json.Unmarshal(data, &blk); err == nil {
			received.Store(&blk)
		}
	})

	time.Sleep(300 * time.Millisecond)

	// Broadcast block from A.
	testBlock := &block.Block{
		Header: &block.Header{
			Version:   1,
			Height:    42,
			Timestamp: uint64(time.Now().Unix()),
		},
		Transactions: []*tx.Transaction{
			{
				Version: 1,
				Outputs: []tx.Output{{Value: 1000, Script: types.Script{Type: types.ScriptTypeP2PKH, Data: make([]byte, 20)}}},
			},
		},
	}

	if err := nodeA.BroadcastBlock(testBlock); err != nil {
		t.Fatalf("BroadcastBlock: %v", err)
	}

	// Wait for delivery.
	deadline := time.After(5 * time.Second)
	for {
		if v := received.Load(); v != nil {
			rxBlock := v.(*block.Block)
			if rxBlock.Header.Height != 42 {
				t.Errorf("expected height 42, got %d", rxBlock.Header.Height)
			}
			if len(rxBlock.Transactions) != 1 {
				t.Errorf("expected 1 tx, got %d", len(rxBlock.Transactions))
			}
			return // Success!
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for block gossip")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// --- Sync Protocol ---

func TestSyncRequest_JSON(t *testing.T) {
	req := SyncRequest{FromHeight: 10, MaxBlocks: 100}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SyncRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.FromHeight != 10 || decoded.MaxBlocks != 100 {
		t.Errorf("roundtrip mismatch: %+v", decoded)
	}
}

func TestSyncResponse_JSON(t *testing.T) {
	resp := SyncResponse{
		Blocks: []*block.Block{
			{Header: &block.Header{Height: 1}},
			{Header: &block.Header{Height: 2}},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SyncResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(decoded.Blocks))
	}
	if decoded.Blocks[0].Header.Height != 1 || decoded.Blocks[1].Header.Height != 2 {
		t.Error("block heights mismatch")
	}
}

// --- Sync Stream Integration ---

func TestTwoNodes_SyncBlocks(t *testing.T) {
	nodeA := startTestNode(t)
	nodeB := startTestNode(t)
	connectNodes(t, nodeA, nodeB)

	// Node A has blocks to serve.
	fakeBlocks := []*block.Block{
		{Header: &block.Header{Height: 0, Version: 1}},
		{Header: &block.Header{Height: 1, Version: 1}},
		{Header: &block.Header{Height: 2, Version: 1}},
	}

	syncerA := NewSyncer(nodeA)
	syncerA.RegisterHandler(func(fromHeight uint64, max uint32) []*block.Block {
		var result []*block.Block
		for _, b := range fakeBlocks {
			if b.Header.Height >= fromHeight {
				result = append(result, b)
				if uint32(len(result)) >= max {
					break
				}
			}
		}
		return result
	})

	// Node B requests blocks from A.
	syncerB := NewSyncer(nodeB)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	blocks, err := syncerB.RequestBlocks(ctx, nodeA.host.ID(), 1, 10)
	if err != nil {
		t.Fatalf("RequestBlocks: %v", err)
	}

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (height 1,2), got %d", len(blocks))
	}
	if blocks[0].Header.Height != 1 || blocks[1].Header.Height != 2 {
		t.Errorf("unexpected block heights: %d, %d", blocks[0].Header.Height, blocks[1].Header.Height)
	}
}

func TestTwoNodes_SyncBlocks_Empty(t *testing.T) {
	nodeA := startTestNode(t)
	nodeB := startTestNode(t)
	connectNodes(t, nodeA, nodeB)

	syncerA := NewSyncer(nodeA)
	syncerA.RegisterHandler(func(fromHeight uint64, max uint32) []*block.Block {
		return nil // No blocks.
	})

	syncerB := NewSyncer(nodeB)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	blocks, err := syncerB.RequestBlocks(ctx, nodeA.host.ID(), 0, 10)
	if err != nil {
		t.Fatalf("RequestBlocks: %v", err)
	}

	if len(blocks) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(blocks))
	}
}

// --- Panic Recovery Tests ---

func TestPanicRecovery_HandleBlock(t *testing.T) {
	nodeA := startTestNode(t)
	nodeB := startTestNode(t)
	connectNodes(t, nodeA, nodeB)

	// Set a handler on B that panics.
	var panicCount atomic.Int32
	nodeB.SetBlockHandler(func(_ peer.ID, data []byte) {
		panicCount.Add(1)
		panic("test panic in block handler")
	})

	time.Sleep(300 * time.Millisecond)

	// Broadcast a block from A. The panicking handler should be recovered.
	testBlock := &block.Block{
		Header: &block.Header{
			Version:   1,
			Height:    1,
			Timestamp: uint64(time.Now().Unix()),
		},
		Transactions: []*tx.Transaction{},
	}
	if err := nodeA.BroadcastBlock(testBlock); err != nil {
		t.Fatalf("BroadcastBlock: %v", err)
	}

	// Wait for the handler to be called.
	deadline := time.After(5 * time.Second)
	for {
		if panicCount.Load() > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for panicking handler to be called")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Node B should still be alive — send another block.
	testBlock2 := &block.Block{
		Header: &block.Header{
			Version:   1,
			Height:    2,
			Timestamp: uint64(time.Now().Unix()),
		},
		Transactions: []*tx.Transaction{},
	}
	if err := nodeA.BroadcastBlock(testBlock2); err != nil {
		t.Fatalf("second BroadcastBlock: %v", err)
	}

	// Wait for second panic (proves goroutine survived).
	deadline2 := time.After(5 * time.Second)
	for {
		if panicCount.Load() >= 2 {
			return // Success: goroutine survived the panic.
		}
		select {
		case <-deadline2:
			t.Fatal("timed out waiting for second block handler call — goroutine may have died")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// --- DHT + Persistence Integration Tests ---

func TestNode_StartStop_WithDHT(t *testing.T) {
	n := New(Config{
		ListenAddr: "127.0.0.1",
		Port:       0,
		NoDiscover: false,
		DB:         storage.NewMemory(),
	})

	if err := n.Start(); err != nil {
		t.Fatalf("Start with DHT: %v", err)
	}

	if n.dht == nil {
		t.Error("DHT should be initialized when NoDiscover is false")
	}
	if n.peerStore == nil {
		t.Error("peerStore should be initialized when DB is provided")
	}
	if n.connNotify == nil {
		t.Error("connNotify should be initialized after Start")
	}

	if err := n.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if n.dht != nil {
		t.Error("DHT should be nil after Stop")
	}
}

func TestNode_PeerPersistence(t *testing.T) {
	db := storage.NewMemory()

	// Create a node with persistence, start it, add a peer, persist.
	nodeA := New(Config{ListenAddr: "127.0.0.1", Port: 0, NoDiscover: true, DB: db})
	if err := nodeA.Start(); err != nil {
		t.Fatalf("Start nodeA: %v", err)
	}

	nodeB := New(Config{ListenAddr: "127.0.0.1", Port: 0, NoDiscover: true})
	if err := nodeB.Start(); err != nil {
		t.Fatalf("Start nodeB: %v", err)
	}

	// Connect B → A.
	aInfo := peer.AddrInfo{ID: nodeA.host.ID(), Addrs: nodeA.host.Addrs()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := nodeB.host.Connect(ctx, aInfo); err != nil {
		t.Fatalf("connect: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	if nodeA.PeerCount() < 1 {
		t.Fatalf("nodeA expected >=1 peer, got %d", nodeA.PeerCount())
	}

	// Persist peers.
	nodeA.persistPeers()

	// Verify persistence by reading from the same DB.
	ps := NewPeerStore(db)
	records, err := ps.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(records) < 1 {
		t.Errorf("expected at least 1 persisted peer, got %d", len(records))
	}

	// Check the persisted peer matches nodeB.
	found := false
	for _, rec := range records {
		if rec.ID == nodeB.host.ID().String() {
			found = true
		}
	}
	if !found {
		t.Error("nodeB not found in persisted peers")
	}

	nodeB.Stop()
	nodeA.Stop()
}

func TestThreeNodes_DHTDiscovery(t *testing.T) {
	// Node A: DHT server (bootstrap node).
	nodeA := New(Config{
		ListenAddr: "127.0.0.1",
		Port:       0,
		NoDiscover: false,
		DHTServer:  true,
	})
	if err := nodeA.Start(); err != nil {
		t.Fatalf("Start nodeA: %v", err)
	}
	t.Cleanup(func() { nodeA.Stop() })

	// Node B: DHT client, connects to A.
	nodeB := New(Config{
		ListenAddr: "127.0.0.1",
		Port:       0,
		NoDiscover: false,
	})
	if err := nodeB.Start(); err != nil {
		t.Fatalf("Start nodeB: %v", err)
	}
	t.Cleanup(func() { nodeB.Stop() })

	// Node C: DHT client, connects to A.
	nodeC := New(Config{
		ListenAddr: "127.0.0.1",
		Port:       0,
		NoDiscover: false,
	})
	if err := nodeC.Start(); err != nil {
		t.Fatalf("Start nodeC: %v", err)
	}
	t.Cleanup(func() { nodeC.Stop() })

	// Connect B → A and C → A.
	aInfo := peer.AddrInfo{ID: nodeA.host.ID(), Addrs: nodeA.host.Addrs()}

	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	if err := nodeB.host.Connect(ctx1, aInfo); err != nil {
		cancel1()
		t.Fatalf("connect B→A: %v", err)
	}
	cancel1()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	if err := nodeC.host.Connect(ctx2, aInfo); err != nil {
		cancel2()
		t.Fatalf("connect C→A: %v", err)
	}
	cancel2()

	// Give DHT time to propagate routing tables.
	time.Sleep(2 * time.Second)

	// A should know both B and C.
	if nodeA.PeerCount() < 2 {
		t.Errorf("nodeA expected >=2 peers, got %d", nodeA.PeerCount())
	}

	// B and C should at least know A (and possibly each other via DHT).
	if nodeB.PeerCount() < 1 {
		t.Errorf("nodeB expected >=1 peer, got %d", nodeB.PeerCount())
	}
	if nodeC.PeerCount() < 1 {
		t.Errorf("nodeC expected >=1 peer, got %d", nodeC.PeerCount())
	}
}

package p2p

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Klingon-tech/klingnet-chain/pkg/types"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestHandshakeMessage_JSON(t *testing.T) {
	msg := HandshakeMessage{
		ProtocolVersion: 1,
		GenesisHash:     types.Hash{0xaa, 0xbb, 0xcc},
		NetworkID:       "klingnet-testnet-1",
		BestHeight:      42,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded HandshakeMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ProtocolVersion != msg.ProtocolVersion {
		t.Errorf("ProtocolVersion: got %d, want %d", decoded.ProtocolVersion, msg.ProtocolVersion)
	}
	if decoded.GenesisHash != msg.GenesisHash {
		t.Errorf("GenesisHash mismatch")
	}
	if decoded.NetworkID != msg.NetworkID {
		t.Errorf("NetworkID: got %q, want %q", decoded.NetworkID, msg.NetworkID)
	}
	if decoded.BestHeight != msg.BestHeight {
		t.Errorf("BestHeight: got %d, want %d", decoded.BestHeight, msg.BestHeight)
	}
}

func TestNode_ValidateHandshake_Success(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	n.genesisHash = types.Hash{0x01, 0x02, 0x03}

	msg := HandshakeMessage{
		ProtocolVersion: ProtocolVersion,
		GenesisHash:     types.Hash{0x01, 0x02, 0x03},
		NetworkID:       "test",
		BestHeight:      100,
	}

	reason := n.validateHandshake(msg)
	if reason != "" {
		t.Errorf("expected success, got reason: %s", reason)
	}
}

func TestNode_ValidateHandshake_GenesisMismatch(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	n.genesisHash = types.Hash{0x01, 0x02, 0x03}

	msg := HandshakeMessage{
		ProtocolVersion: ProtocolVersion,
		GenesisHash:     types.Hash{0xff, 0xfe, 0xfd}, // Different genesis.
		NetworkID:       "test",
	}

	reason := n.validateHandshake(msg)
	if reason == "" {
		t.Error("expected genesis mismatch reason, got empty")
	}
}

func TestNode_ValidateHandshake_VersionTooLow(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	n.genesisHash = types.Hash{0x01}

	msg := HandshakeMessage{
		ProtocolVersion: 0, // Below minimum.
		GenesisHash:     types.Hash{0x01},
		NetworkID:       "test",
	}

	reason := n.validateHandshake(msg)
	if reason == "" {
		t.Error("expected version too low reason, got empty")
	}
}

func TestNode_SetGenesisHash(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})

	if n.handshakeEnabled {
		t.Error("handshake should be disabled by default")
	}

	h := types.Hash{0xaa, 0xbb}
	n.SetGenesisHash(h)

	if !n.handshakeEnabled {
		t.Error("handshake should be enabled after SetGenesisHash with non-zero hash")
	}
	if n.genesisHash != h {
		t.Error("genesis hash not set correctly")
	}

	// Setting zero hash disables it.
	n.SetGenesisHash(types.Hash{})
	if n.handshakeEnabled {
		t.Error("handshake should be disabled after SetGenesisHash with zero hash")
	}
}

func TestNode_BuildHandshakeMessage(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0, NetworkID: "klingnet-testnet-1"})
	n.genesisHash = types.Hash{0x01}
	n.heightFn = func() uint64 { return 99 }

	msg := n.buildHandshakeMessage()

	if msg.ProtocolVersion != ProtocolVersion {
		t.Errorf("ProtocolVersion: got %d, want %d", msg.ProtocolVersion, ProtocolVersion)
	}
	if msg.GenesisHash != n.genesisHash {
		t.Error("GenesisHash mismatch")
	}
	if msg.NetworkID != "klingnet-testnet-1" {
		t.Errorf("NetworkID: got %q, want %q", msg.NetworkID, "klingnet-testnet-1")
	}
	if msg.BestHeight != 99 {
		t.Errorf("BestHeight: got %d, want 99", msg.BestHeight)
	}
}

func TestNode_BuildHandshakeMessage_NoHeightFn(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	n.genesisHash = types.Hash{0x01}

	msg := n.buildHandshakeMessage()
	if msg.BestHeight != 0 {
		t.Errorf("BestHeight should be 0 without heightFn, got %d", msg.BestHeight)
	}
}

func TestNode_DisconnectPeer_NotStarted(t *testing.T) {
	n := New(Config{ListenAddr: "127.0.0.1", Port: 0})
	err := n.DisconnectPeer(peer.ID("fake"))
	if err == nil {
		t.Error("DisconnectPeer should fail before Start")
	}
}

func TestNode_DisconnectPeer(t *testing.T) {
	nodeA := startTestNode(t)
	nodeB := startTestNode(t)
	connectNodes(t, nodeA, nodeB)

	if nodeA.PeerCount() < 1 {
		t.Fatal("nodeA should have at least 1 peer")
	}

	// Disconnect B from A's side.
	if err := nodeA.DisconnectPeer(nodeB.host.ID()); err != nil {
		t.Fatalf("DisconnectPeer: %v", err)
	}

	// Wait for disconnect to propagate.
	time.Sleep(200 * time.Millisecond)

	if nodeA.PeerCount() != 0 {
		t.Errorf("nodeA should have 0 peers after disconnect, got %d", nodeA.PeerCount())
	}
}

func TestTwoNodes_Handshake_Success(t *testing.T) {
	genesis := types.Hash{0x01, 0x02, 0x03}

	nodeA := New(Config{ListenAddr: "127.0.0.1", Port: 0, NoDiscover: true, NetworkID: "test"})
	nodeA.SetGenesisHash(genesis)
	nodeA.SetHeightFn(func() uint64 { return 10 })
	if err := nodeA.Start(); err != nil {
		t.Fatalf("start nodeA: %v", err)
	}
	t.Cleanup(func() { nodeA.Stop() })

	nodeB := New(Config{ListenAddr: "127.0.0.1", Port: 0, NoDiscover: true, NetworkID: "test"})
	nodeB.SetGenesisHash(genesis)
	nodeB.SetHeightFn(func() uint64 { return 10 })
	if err := nodeB.Start(); err != nil {
		t.Fatalf("start nodeB: %v", err)
	}
	t.Cleanup(func() { nodeB.Stop() })

	connectNodes(t, nodeA, nodeB)

	// Both should remain connected (same genesis).
	time.Sleep(500 * time.Millisecond)

	if nodeA.PeerCount() < 1 {
		t.Errorf("nodeA should still have peer, got %d", nodeA.PeerCount())
	}
	if nodeB.PeerCount() < 1 {
		t.Errorf("nodeB should still have peer, got %d", nodeB.PeerCount())
	}
}

func TestTwoNodes_Handshake_GenesisMismatch(t *testing.T) {
	nodeA := New(Config{ListenAddr: "127.0.0.1", Port: 0, NoDiscover: true, NetworkID: "test"})
	nodeA.SetGenesisHash(types.Hash{0x01})
	nodeA.SetHeightFn(func() uint64 { return 10 })
	if err := nodeA.Start(); err != nil {
		t.Fatalf("start nodeA: %v", err)
	}
	t.Cleanup(func() { nodeA.Stop() })

	nodeB := New(Config{ListenAddr: "127.0.0.1", Port: 0, NoDiscover: true, NetworkID: "test"})
	nodeB.SetGenesisHash(types.Hash{0xff}) // Different genesis.
	nodeB.SetHeightFn(func() uint64 { return 10 })
	if err := nodeB.Start(); err != nil {
		t.Fatalf("start nodeB: %v", err)
	}
	t.Cleanup(func() { nodeB.Stop() })

	connectNodes(t, nodeA, nodeB)

	// Wait for handshake to complete and disconnect.
	time.Sleep(1 * time.Second)

	// At least one side should have disconnected. Both sides validate
	// the handshake, so both may disconnect.
	if nodeA.PeerCount() > 0 && nodeB.PeerCount() > 0 {
		t.Errorf("expected at least one side to disconnect: A=%d B=%d",
			nodeA.PeerCount(), nodeB.PeerCount())
	}
}

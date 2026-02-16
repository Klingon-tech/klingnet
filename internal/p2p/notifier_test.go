package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestConnNotifier_Connected(t *testing.T) {
	nodeA := startTestNode(t)
	nodeB := startTestNode(t)

	// Connect B → A. The connNotifier on both sides should track the peer.
	aInfo := peer.AddrInfo{
		ID:    nodeA.host.ID(),
		Addrs: nodeA.host.Addrs(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := nodeB.host.Connect(ctx, aInfo); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Give notifier time to fire.
	time.Sleep(200 * time.Millisecond)

	// Both nodes should see each other in PeerList.
	if nodeA.PeerCount() < 1 {
		t.Errorf("nodeA expected >=1 peer, got %d", nodeA.PeerCount())
	}
	if nodeB.PeerCount() < 1 {
		t.Errorf("nodeB expected >=1 peer, got %d", nodeB.PeerCount())
	}

	// Verify specific peer IDs.
	foundB := false
	for _, p := range nodeA.PeerList() {
		if p.ID == nodeB.host.ID() {
			foundB = true
		}
	}
	if !foundB {
		t.Error("nodeA does not have nodeB in PeerList")
	}

	foundA := false
	for _, p := range nodeB.PeerList() {
		if p.ID == nodeA.host.ID() {
			foundA = true
		}
	}
	if !foundA {
		t.Error("nodeB does not have nodeA in PeerList")
	}
}

func TestConnNotifier_Disconnected(t *testing.T) {
	nodeA := startTestNode(t)
	nodeB := startTestNode(t)

	// Connect.
	aInfo := peer.AddrInfo{
		ID:    nodeA.host.ID(),
		Addrs: nodeA.host.Addrs(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := nodeB.host.Connect(ctx, aInfo); err != nil {
		t.Fatalf("connect: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	if nodeA.PeerCount() < 1 {
		t.Fatalf("nodeA should have at least 1 peer before disconnect, got %d", nodeA.PeerCount())
	}

	// Close all connections from B to A.
	for _, conn := range nodeB.host.Network().ConnsToPeer(nodeA.host.ID()) {
		conn.Close()
	}

	// Wait for disconnection notification to propagate.
	time.Sleep(500 * time.Millisecond)

	// nodeB should no longer have nodeA (all connections closed).
	foundA := false
	for _, p := range nodeB.PeerList() {
		if p.ID == nodeA.host.ID() {
			foundA = true
		}
	}
	if foundA {
		t.Error("nodeB should not have nodeA in PeerList after disconnect")
	}
}

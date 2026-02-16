package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestHeightRequest_RoundTrip(t *testing.T) {
	// Create two minimal libp2p hosts.
	h1, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatalf("create host1: %v", err)
	}
	defer h1.Close()

	h2, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatalf("create host2: %v", err)
	}
	defer h2.Close()

	// Node1 acts as provider (has a chain).
	node1 := &Node{host: h1}
	syncer1 := NewSyncer(node1)
	syncer1.RegisterHeightHandler(func() (uint64, string) {
		return 42, "abcdef1234567890"
	})

	// Connect h2 → h1.
	h2.Peerstore().AddAddrs(h1.ID(), h1.Addrs(), time.Hour)
	if err := h2.Connect(context.Background(), peer.AddrInfo{ID: h1.ID(), Addrs: h1.Addrs()}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Node2 requests height from node1.
	node2 := &Node{host: h2}
	syncer2 := NewSyncer(node2)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := syncer2.RequestHeight(ctx, h1.ID())
	if err != nil {
		t.Fatalf("RequestHeight: %v", err)
	}

	if resp.Height != 42 {
		t.Errorf("Height = %d, want 42", resp.Height)
	}
	if resp.TipHash != "abcdef1234567890" {
		t.Errorf("TipHash = %s, want abcdef1234567890", resp.TipHash)
	}
}

func TestHeightRequest_NoPeer(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	defer h.Close()

	node := &Node{host: h}
	syncer := NewSyncer(node)

	// Use a fake peer ID that is not connected.
	fakePeer, _ := peer.Decode("QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N")

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err = syncer.RequestHeight(ctx, fakePeer)
	if err == nil {
		t.Fatal("expected error for unreachable peer")
	}
}

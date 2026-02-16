package p2p

import (
	"context"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// discoveryNotifee handles mDNS peer discovery notifications.
type discoveryNotifee struct {
	node *Node
}

// HandlePeerFound is called when a peer is discovered via mDNS.
func (d *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == d.node.host.ID() {
		return // Ignore self.
	}

	ctx, cancel := context.WithTimeout(d.node.ctx, 5*time.Second)
	defer cancel()

	if err := d.node.host.Connect(ctx, pi); err == nil {
		d.node.addPeer(pi.ID)
	}
}

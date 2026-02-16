package p2p

import (
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/multiformats/go-multiaddr"
)

// connNotifier tracks connection lifecycle events via the network.Notifiee
// interface. It calls addPeer on connect and removePeer on disconnect.
type connNotifier struct {
	node *Node
}

// Connected is called when a new connection is opened.
func (cn *connNotifier) Connected(_ network.Network, conn network.Conn) {
	remotePeer := conn.RemotePeer()
	if remotePeer == cn.node.host.ID() {
		return // Ignore self-connections.
	}
	cn.node.addPeer(remotePeer)
	if fn := cn.node.onPeerConnected; fn != nil {
		go fn()
	}
	// Initiate handshake for outbound connections only (inbound handled by stream handler).
	if cn.node.handshakeEnabled && conn.Stat().Direction == network.DirOutbound {
		go cn.node.doHandshake(remotePeer)
	}
}

// Disconnected is called when a connection is closed. Only removes the peer
// if there are no remaining connections to it.
func (cn *connNotifier) Disconnected(net network.Network, conn network.Conn) {
	remotePeer := conn.RemotePeer()
	// Check if there are other active connections to this peer.
	if len(net.ConnsToPeer(remotePeer)) == 0 {
		cn.node.removePeer(remotePeer)
	}
}

// Listen is called when the node starts listening on a new address.
func (cn *connNotifier) Listen(network.Network, multiaddr.Multiaddr) {}

// ListenClose is called when the node stops listening on an address.
func (cn *connNotifier) ListenClose(network.Network, multiaddr.Multiaddr) {}

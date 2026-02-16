package p2p

import (
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// banGater implements the libp2p ConnectionGater interface to reject
// connections from banned peers at the transport level.
type banGater struct {
	banMgr *BanManager
}

// InterceptPeerDial rejects outbound dials to banned peers.
func (g *banGater) InterceptPeerDial(p peer.ID) bool {
	return !g.banMgr.IsBanned(p)
}

// InterceptAddrDial allows all address dials (filtering is done per-peer).
func (g *banGater) InterceptAddrDial(_ peer.ID, _ ma.Multiaddr) bool {
	return true
}

// InterceptAccept allows all inbound connections at the transport layer.
// Peer identity is not yet known at this stage.
func (g *banGater) InterceptAccept(_ network.ConnMultiaddrs) bool {
	return true
}

// InterceptSecured rejects connections from banned peers once their
// identity is authenticated.
func (g *banGater) InterceptSecured(_ network.Direction, p peer.ID, _ network.ConnMultiaddrs) bool {
	return !g.banMgr.IsBanned(p)
}

// InterceptUpgraded allows all fully upgraded connections.
func (g *banGater) InterceptUpgraded(_ network.Conn) (bool, control.DisconnectReason) {
	return true, 0
}

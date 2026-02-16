package p2p

import (
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// Peer represents a connected peer.
type Peer struct {
	ID          peer.ID
	ConnectedAt time.Time
	Source      string // "dht", "mdns", "seed", "gossip"
}

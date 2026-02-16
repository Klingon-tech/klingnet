package p2p

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	klog "github.com/Klingon-tech/klingnet-chain/internal/log"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	// handshakeTimeout is the max time for a complete handshake exchange.
	handshakeTimeout = 10 * time.Second

	// maxHandshakeBytes limits handshake message size.
	maxHandshakeBytes = 4096
)

// HandshakeMessage is exchanged between peers to verify compatibility.
type HandshakeMessage struct {
	ProtocolVersion uint32     `json:"protocol_version"`
	GenesisHash     types.Hash `json:"genesis_hash"`
	NetworkID       string     `json:"network_id"`
	BestHeight      uint64     `json:"best_height"`
}

// registerHandshakeHandler sets up the stream handler for incoming handshakes.
func (n *Node) registerHandshakeHandler() {
	logger := klog.WithComponent("p2p")
	n.host.SetStreamHandler(HandshakeProtocol, func(stream network.Stream) {
		defer stream.Close()

		remotePeer := stream.Conn().RemotePeer()

		_ = stream.SetReadDeadline(time.Now().Add(handshakeTimeout))

		// Read peer's handshake message.
		var peerMsg HandshakeMessage
		if err := json.NewDecoder(io.LimitReader(stream, maxHandshakeBytes)).Decode(&peerMsg); err != nil {
			logger.Debug().Err(err).Str("peer", remotePeer.String()[:16]).Msg("Handshake read failed")
			return
		}

		// Respond with our message.
		ourMsg := n.buildHandshakeMessage()
		if err := json.NewEncoder(stream).Encode(&ourMsg); err != nil {
			logger.Debug().Err(err).Str("peer", remotePeer.String()[:16]).Msg("Handshake write failed")
			return
		}

		// Validate peer's message.
		if reason := n.validateHandshake(peerMsg); reason != "" {
			logger.Warn().
				Str("peer", remotePeer.String()[:16]).
				Str("reason", reason).
				Msg("Handshake rejected, banning peer")
			if n.BanManager != nil {
				n.BanManager.RecordOffense(remotePeer, PenaltyHandshakeFail, reason)
			}
			n.DisconnectPeer(remotePeer)
		}
	})
}

// doHandshake initiates a handshake with a remote peer (dialer side).
func (n *Node) doHandshake(peerID peer.ID) {
	logger := klog.WithComponent("p2p")

	ctx, cancel := n.ctx, func() {} // Use node's context.
	_ = cancel

	stream, err := n.host.NewStream(ctx, peerID, HandshakeProtocol)
	if err != nil {
		// Peer doesn't support handshake protocol — tolerate for now.
		logger.Debug().Str("peer", peerID.String()[:16]).Msg("Peer does not support handshake protocol, tolerating")
		return
	}
	defer stream.Close()

	_ = stream.SetDeadline(time.Now().Add(handshakeTimeout))

	// Send our message.
	ourMsg := n.buildHandshakeMessage()
	if err := json.NewEncoder(stream).Encode(&ourMsg); err != nil {
		logger.Debug().Err(err).Str("peer", peerID.String()[:16]).Msg("Handshake send failed")
		return
	}

	// Signal we're done writing.
	stream.CloseWrite()

	// Read peer's response.
	var peerMsg HandshakeMessage
	if err := json.NewDecoder(io.LimitReader(stream, maxHandshakeBytes)).Decode(&peerMsg); err != nil {
		logger.Debug().Err(err).Str("peer", peerID.String()[:16]).Msg("Handshake response read failed")
		return
	}

	// Validate.
	if reason := n.validateHandshake(peerMsg); reason != "" {
		logger.Warn().
			Str("peer", peerID.String()[:16]).
			Str("reason", reason).
			Msg("Handshake rejected, banning peer")
		if n.BanManager != nil {
			n.BanManager.RecordOffense(peerID, PenaltyHandshakeFail, reason)
		}
		n.DisconnectPeer(peerID)
	}
}

// validateHandshake checks a peer's handshake message for compatibility.
// Returns an empty string on success, or a reason string on failure.
func (n *Node) validateHandshake(msg HandshakeMessage) string {
	if msg.GenesisHash != n.genesisHash {
		return fmt.Sprintf("genesis mismatch: peer=%s local=%s",
			msg.GenesisHash.String()[:16], n.genesisHash.String()[:16])
	}
	if msg.ProtocolVersion < MinProtocolVersion {
		return fmt.Sprintf("protocol version too low: peer=%d min=%d",
			msg.ProtocolVersion, MinProtocolVersion)
	}
	return ""
}

// buildHandshakeMessage constructs our handshake message from node state.
func (n *Node) buildHandshakeMessage() HandshakeMessage {
	msg := HandshakeMessage{
		ProtocolVersion: ProtocolVersion,
		GenesisHash:     n.genesisHash,
		NetworkID:       n.config.NetworkID,
	}
	if n.heightFn != nil {
		msg.BestHeight = n.heightFn()
	}
	return msg
}

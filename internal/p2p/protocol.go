package p2p

import (
	"fmt"

	"github.com/libp2p/go-libp2p/core/protocol"
)

// GossipSub topic names.
const (
	TopicTransactions = "/klingnet/tx/1.0.0"
	TopicBlocks       = "/klingnet/block/1.0.0"
	TopicHeartbeat    = "/klingnet/heartbeat/1.0.0"
)

// Handshake protocol constants.
const (
	// HandshakeProtocol is the stream protocol ID for peer compatibility checking.
	HandshakeProtocol = protocol.ID("/klingnet/handshake/1.0.0")

	// ProtocolVersion is the current protocol version advertised during handshake.
	ProtocolVersion uint32 = 1

	// MinProtocolVersion is the minimum protocol version we accept from peers.
	MinProtocolVersion uint32 = 1
)

// SubChainBlockTopic returns the GossipSub topic for a sub-chain's blocks.
func SubChainBlockTopic(chainIDHex string) string {
	return fmt.Sprintf("/klingnet/sc/%s/block/1.0.0", chainIDHex)
}

// SubChainTxTopic returns the GossipSub topic for a sub-chain's transactions.
func SubChainTxTopic(chainIDHex string) string {
	return fmt.Sprintf("/klingnet/sc/%s/tx/1.0.0", chainIDHex)
}

// SubChainHeartbeatTopic returns the GossipSub topic for a sub-chain's validator heartbeats.
func SubChainHeartbeatTopic(chainIDHex string) string {
	return fmt.Sprintf("/klingnet/sc/%s/heartbeat/1.0.0", chainIDHex)
}

// SubChainSyncProtocol returns the stream protocol ID for sub-chain block sync.
func SubChainSyncProtocol(chainIDHex string) protocol.ID {
	return protocol.ID(fmt.Sprintf("/klingnet/sc/%s/sync/1.0.0", chainIDHex))
}

// SubChainHeightProtocol returns the stream protocol ID for sub-chain height queries.
func SubChainHeightProtocol(chainIDHex string) protocol.ID {
	return protocol.ID(fmt.Sprintf("/klingnet/sc/%s/height/1.0.0", chainIDHex))
}

// MessageType identifies the type of P2P message.
type MessageType uint8

const (
	MsgTx    MessageType = iota + 1 // Transaction broadcast.
	MsgBlock                        // Block broadcast.
)

// Message is a P2P protocol message.
type Message struct {
	Type    MessageType `json:"type"`
	Payload []byte      `json:"payload"`
}

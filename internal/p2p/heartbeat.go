package p2p

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	pubsub "github.com/libp2p/go-libp2p-pubsub"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
)

// HeartbeatMessage is a signed validator liveness announcement.
type HeartbeatMessage struct {
	PubKey    []byte `json:"pubkey"`    // 33-byte compressed public key
	Height    uint64 `json:"height"`    // current chain height
	Timestamp int64  `json:"timestamp"` // unix seconds
	Signature []byte `json:"signature"` // Schnorr sig over BLAKE3(pubkey || height_le8 || timestamp_le8)
}

// HeartbeatSigningBytes returns the bytes that are signed/verified for a heartbeat message.
func HeartbeatSigningBytes(pubKey []byte, height uint64, timestamp int64) []byte {
	buf := make([]byte, len(pubKey)+8+8)
	copy(buf, pubKey)
	binary.LittleEndian.PutUint64(buf[len(pubKey):], height)
	binary.LittleEndian.PutUint64(buf[len(pubKey)+8:], uint64(timestamp))
	return buf
}

// VerifyHeartbeat checks that the heartbeat message has a valid Schnorr signature.
func VerifyHeartbeat(msg *HeartbeatMessage) bool {
	if len(msg.PubKey) != 33 || len(msg.Signature) == 0 {
		return false
	}
	data := HeartbeatSigningBytes(msg.PubKey, msg.Height, msg.Timestamp)
	hash := crypto.Hash(data)
	return crypto.VerifySignature(hash[:], msg.Signature, msg.PubKey)
}

// SetHeartbeatHandler registers a callback for verified incoming heartbeats.
func (n *Node) SetHeartbeatHandler(fn func(msg *HeartbeatMessage)) {
	n.heartbeatHandler = fn
}

// JoinHeartbeat joins the heartbeat GossipSub topic and starts reading.
func (n *Node) JoinHeartbeat() error {
	if n.pubsub == nil {
		return fmt.Errorf("p2p node not started")
	}
	if n.topicHeartbeat != nil {
		return nil // Already joined.
	}

	topic, err := n.pubsub.Join(TopicHeartbeat)
	if err != nil {
		return fmt.Errorf("join heartbeat topic: %w", err)
	}
	sub, err := topic.Subscribe()
	if err != nil {
		topic.Close()
		return fmt.Errorf("subscribe heartbeat topic: %w", err)
	}
	n.topicHeartbeat = topic
	n.subHeartbeat = sub

	go n.heartbeatReadLoop()
	return nil
}

// LeaveHeartbeat unsubscribes from the heartbeat topic.
func (n *Node) LeaveHeartbeat() {
	if n.subHeartbeat != nil {
		n.subHeartbeat.Cancel()
		n.subHeartbeat = nil
	}
	if n.topicHeartbeat != nil {
		n.topicHeartbeat.Close()
		n.topicHeartbeat = nil
	}
}

// BroadcastHeartbeat publishes a heartbeat message to the GossipSub topic.
func (n *Node) BroadcastHeartbeat(msg *HeartbeatMessage) error {
	if n.topicHeartbeat == nil {
		return fmt.Errorf("heartbeat topic not joined")
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal heartbeat: %w", err)
	}
	return n.topicHeartbeat.Publish(n.ctx, data)
}

func (n *Node) heartbeatReadLoop() {
	for {
		msg, err := n.subHeartbeat.Next(n.ctx)
		if err != nil {
			return // Context cancelled or subscription closed.
		}
		if msg.ReceivedFrom == n.host.ID() {
			continue // Skip own messages.
		}

		var hb HeartbeatMessage
		if err := json.Unmarshal(msg.Data, &hb); err != nil {
			continue // Malformed message.
		}

		// Verify signature before forwarding.
		if !VerifyHeartbeat(&hb) {
			continue // Invalid signature.
		}

		if n.heartbeatHandler != nil {
			func() {
				defer func() { recover() }()
				n.heartbeatHandler(&hb)
			}()
		}
	}
}

// ── Sub-chain heartbeat methods ─────────────────────────────────────────

// SetSubChainHeartbeatHandler sets the handler for incoming sub-chain heartbeats.
func (n *Node) SetSubChainHeartbeatHandler(chainIDHex string, fn func(*HeartbeatMessage)) {
	n.scMu.Lock()
	defer n.scMu.Unlock()
	n.scHBHandlers[chainIDHex] = fn
}

// JoinSubChainHeartbeat joins a sub-chain's heartbeat GossipSub topic.
func (n *Node) JoinSubChainHeartbeat(chainIDHex string) error {
	if n.pubsub == nil {
		return fmt.Errorf("p2p node not started")
	}

	n.scMu.Lock()
	defer n.scMu.Unlock()

	if _, ok := n.scHBTopics[chainIDHex]; ok {
		return nil // Already joined.
	}

	topic, err := n.pubsub.Join(SubChainHeartbeatTopic(chainIDHex))
	if err != nil {
		return fmt.Errorf("join sub-chain heartbeat topic %s: %w", chainIDHex, err)
	}
	sub, err := topic.Subscribe()
	if err != nil {
		topic.Close()
		return fmt.Errorf("subscribe sub-chain heartbeat topic %s: %w", chainIDHex, err)
	}

	n.scHBTopics[chainIDHex] = topic
	n.scHBSubs[chainIDHex] = sub

	go n.scHeartbeatReadLoop(chainIDHex, sub)
	return nil
}

// LeaveSubChainHeartbeat unsubscribes from a sub-chain's heartbeat topic.
func (n *Node) LeaveSubChainHeartbeat(chainIDHex string) {
	n.scMu.Lock()
	defer n.scMu.Unlock()

	if sub, ok := n.scHBSubs[chainIDHex]; ok {
		sub.Cancel()
		delete(n.scHBSubs, chainIDHex)
	}
	if t, ok := n.scHBTopics[chainIDHex]; ok {
		t.Close()
		delete(n.scHBTopics, chainIDHex)
	}
	delete(n.scHBHandlers, chainIDHex)
}

// BroadcastSubChainHeartbeat publishes a heartbeat to a sub-chain's topic.
func (n *Node) BroadcastSubChainHeartbeat(chainIDHex string, msg *HeartbeatMessage) error {
	n.scMu.RLock()
	topic, ok := n.scHBTopics[chainIDHex]
	n.scMu.RUnlock()
	if !ok {
		return fmt.Errorf("not joined to sub-chain %s heartbeat topic", chainIDHex)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal sub-chain heartbeat: %w", err)
	}
	return topic.Publish(n.ctx, data)
}

func (n *Node) scHeartbeatReadLoop(chainIDHex string, sub *pubsub.Subscription) {
	for {
		msg, err := sub.Next(n.ctx)
		if err != nil {
			return
		}
		if msg.ReceivedFrom == n.host.ID() {
			continue
		}

		var hb HeartbeatMessage
		if err := json.Unmarshal(msg.Data, &hb); err != nil {
			continue
		}

		if !VerifyHeartbeat(&hb) {
			continue
		}

		n.scMu.RLock()
		handler := n.scHBHandlers[chainIDHex]
		n.scMu.RUnlock()

		if handler != nil {
			func() {
				defer func() { recover() }()
				handler(&hb)
			}()
		}
	}
}

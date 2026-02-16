package p2p

import (
	"encoding/json"
	"fmt"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
)

// JoinSubChain joins block and tx GossipSub topics for a sub-chain.
// It starts read-loop goroutines for each topic that route messages
// to the registered handlers.
func (n *Node) JoinSubChain(chainIDHex string) error {
	if n.pubsub == nil {
		return fmt.Errorf("p2p node not started")
	}

	n.scMu.Lock()
	defer n.scMu.Unlock()

	// Already joined?
	if _, ok := n.scTopics[chainIDHex]; ok {
		return nil
	}

	// Join block topic.
	blockTopic, err := n.pubsub.Join(SubChainBlockTopic(chainIDHex))
	if err != nil {
		return fmt.Errorf("join sub-chain block topic %s: %w", chainIDHex, err)
	}
	blockSub, err := blockTopic.Subscribe()
	if err != nil {
		blockTopic.Close()
		return fmt.Errorf("subscribe sub-chain block topic %s: %w", chainIDHex, err)
	}

	// Join tx topic.
	txTopic, err := n.pubsub.Join(SubChainTxTopic(chainIDHex))
	if err != nil {
		blockSub.Cancel()
		blockTopic.Close()
		return fmt.Errorf("join sub-chain tx topic %s: %w", chainIDHex, err)
	}
	txSub, err := txTopic.Subscribe()
	if err != nil {
		txTopic.Close()
		blockSub.Cancel()
		blockTopic.Close()
		return fmt.Errorf("subscribe sub-chain tx topic %s: %w", chainIDHex, err)
	}

	n.scTopics[chainIDHex] = blockTopic
	n.scSubs[chainIDHex] = blockSub
	n.scTxTopics[chainIDHex] = txTopic
	n.scTxSubs[chainIDHex] = txSub

	// Start read loops.
	go n.scReadLoop(chainIDHex, blockSub, true)
	go n.scReadLoop(chainIDHex, txSub, false)

	return nil
}

// LeaveSubChain unsubscribes and closes topics for a sub-chain.
func (n *Node) LeaveSubChain(chainIDHex string) {
	n.scMu.Lock()
	defer n.scMu.Unlock()

	if sub, ok := n.scSubs[chainIDHex]; ok {
		sub.Cancel()
		delete(n.scSubs, chainIDHex)
	}
	if sub, ok := n.scTxSubs[chainIDHex]; ok {
		sub.Cancel()
		delete(n.scTxSubs, chainIDHex)
	}
	if t, ok := n.scTopics[chainIDHex]; ok {
		t.Close()
		delete(n.scTopics, chainIDHex)
	}
	if t, ok := n.scTxTopics[chainIDHex]; ok {
		t.Close()
		delete(n.scTxTopics, chainIDHex)
	}
	delete(n.scBlockHandlers, chainIDHex)
	delete(n.scTxHandlers, chainIDHex)
}

// SetSubChainBlockHandler sets the handler for incoming sub-chain blocks.
// The callback receives the sender peer ID and the raw message bytes.
func (n *Node) SetSubChainBlockHandler(chainIDHex string, fn func(peer.ID, []byte)) {
	n.scMu.Lock()
	defer n.scMu.Unlock()
	n.scBlockHandlers[chainIDHex] = fn
}

// SetSubChainTxHandler sets the handler for incoming sub-chain transactions.
// The callback receives the sender peer ID and the raw message bytes.
func (n *Node) SetSubChainTxHandler(chainIDHex string, fn func(peer.ID, []byte)) {
	n.scMu.Lock()
	defer n.scMu.Unlock()
	n.scTxHandlers[chainIDHex] = fn
}

// BroadcastSubChainBlock publishes a block to a sub-chain's GossipSub topic.
func (n *Node) BroadcastSubChainBlock(chainIDHex string, b *block.Block) error {
	n.scMu.RLock()
	topic, ok := n.scTopics[chainIDHex]
	n.scMu.RUnlock()
	if !ok {
		return fmt.Errorf("not joined to sub-chain %s block topic", chainIDHex)
	}

	data, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("marshal sub-chain block: %w", err)
	}

	return topic.Publish(n.ctx, data)
}

// BroadcastSubChainTx publishes a transaction to a sub-chain's GossipSub topic.
func (n *Node) BroadcastSubChainTx(chainIDHex string, t *tx.Transaction) error {
	n.scMu.RLock()
	topic, ok := n.scTxTopics[chainIDHex]
	n.scMu.RUnlock()
	if !ok {
		return fmt.Errorf("not joined to sub-chain %s tx topic", chainIDHex)
	}

	data, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("marshal sub-chain tx: %w", err)
	}

	return topic.Publish(n.ctx, data)
}

// scReadLoop reads messages from a sub-chain subscription and routes them
// to the appropriate handler. isBlock indicates block vs tx topic.
func (n *Node) scReadLoop(chainIDHex string, sub *pubsub.Subscription, isBlock bool) {
	for {
		msg, err := sub.Next(n.ctx)
		if err != nil {
			return // Context cancelled or subscription closed.
		}
		if msg.ReceivedFrom == n.host.ID() {
			continue // Skip own messages.
		}

		n.scMu.RLock()
		var handler func(peer.ID, []byte)
		if isBlock {
			handler = n.scBlockHandlers[chainIDHex]
		} else {
			handler = n.scTxHandlers[chainIDHex]
		}
		n.scMu.RUnlock()

		if handler != nil {
			func() {
				defer func() { recover() }()
				handler(msg.ReceivedFrom, msg.Data)
			}()
		}
	}
}

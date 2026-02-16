package p2p

import (
	"encoding/json"
	"fmt"

	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
)

// BroadcastTx publishes a transaction to the gossip network.
func (n *Node) BroadcastTx(t *tx.Transaction) error {
	if n.topicTx == nil {
		return fmt.Errorf("p2p node not started")
	}

	data, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("marshal tx: %w", err)
	}

	return n.topicTx.Publish(n.ctx, data)
}

// BroadcastBlock publishes a block to the gossip network.
func (n *Node) BroadcastBlock(b *block.Block) error {
	if n.topicBlock == nil {
		return fmt.Errorf("p2p node not started")
	}

	data, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("marshal block: %w", err)
	}

	return n.topicBlock.Publish(n.ctx, data)
}

// Package miner implements block production for Klingnet chain.
package miner

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"time"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// ChainState provides read-only access to the current chain state.
type ChainState interface {
	Height() uint64
	TipHash() types.Hash
	TipTimestamp() uint64
}

// MempoolSelector selects transactions for block inclusion.
type MempoolSelector interface {
	SelectForBlock(limit int) []*tx.Transaction
	GetFee(txHash types.Hash) uint64
}

// SupplyFunc returns the current total coin supply.
type SupplyFunc func() uint64

// Miner produces new blocks.
type Miner struct {
	chain        ChainState
	engine       consensus.Engine
	pool         MempoolSelector
	coinbaseAddr types.Address
	blockReward  uint64
	maxSupply    uint64     // 0 = unlimited
	supplyFn     SupplyFunc // nil = no cap check
	maxBlockTxs  int
}

// New creates a new block producer.
func New(chain ChainState, engine consensus.Engine, pool MempoolSelector,
	coinbaseAddr types.Address, blockReward, maxSupply uint64, supplyFn SupplyFunc) *Miner {
	return &Miner{
		chain:        chain,
		engine:       engine,
		pool:         pool,
		coinbaseAddr: coinbaseAddr,
		blockReward:  blockReward,
		maxSupply:    maxSupply,
		supplyFn:     supplyFn,
		maxBlockTxs:  config.MaxBlockTxs,
	}
}

// ProduceBlock builds, seals, and returns a new block using the current time.
// The coinbase output value = block reward + sum of all tx fees.
// The block is NOT applied to the chain — the caller must call ProcessBlock.
func (m *Miner) ProduceBlock() (*block.Block, error) {
	return m.produceBlock(context.Background(), uint64(time.Now().Unix()))
}

// ProduceBlockAt builds, seals, and returns a new block with the given timestamp.
// Use this instead of ProduceBlock when the caller needs the block timestamp to
// match a previously computed value (e.g. the same timestamp used for slot election).
// The timestamp is bumped to at least parentTimestamp+1 to guarantee monotonicity.
func (m *Miner) ProduceBlockAt(timestamp uint64) (*block.Block, error) {
	return m.produceBlock(context.Background(), timestamp)
}

// ProduceBlockCtx builds and seals a block with cancellation support.
// When the context is cancelled, PoW sealing stops immediately.
func (m *Miner) ProduceBlockCtx(ctx context.Context) (*block.Block, error) {
	return m.produceBlock(ctx, uint64(time.Now().Unix()))
}

func (m *Miner) produceBlock(ctx context.Context, timestamp uint64) (*block.Block, error) {
	// Ensure monotonic: block timestamp must be strictly after parent.
	if parentTS := m.chain.TipTimestamp(); timestamp <= parentTS {
		timestamp = parentTS + 1
	}
	// Select mempool transactions first to compute total fees.
	var selected []*tx.Transaction
	var totalFees uint64
	if m.pool != nil {
		selected = m.pool.SelectForBlock(m.maxBlockTxs - 1) // Reserve slot for coinbase.
		for _, t := range selected {
			totalFees += m.pool.GetFee(t.Hash())
		}
	}

	// Cap block reward to not exceed max supply.
	reward := m.blockReward
	if m.maxSupply > 0 && m.supplyFn != nil {
		currentSupply := m.supplyFn()
		if currentSupply >= m.maxSupply {
			reward = 0
		} else if currentSupply+reward > m.maxSupply {
			reward = m.maxSupply - currentSupply
		}
	}

	// Sort non-coinbase transactions by hash ascending (canonical order).
	sort.Slice(selected, func(i, j int) bool {
		hi, hj := selected[i].Hash(), selected[j].Hash()
		return bytes.Compare(hi[:], hj[:]) < 0
	})

	coinbase := BuildCoinbase(m.coinbaseAddr, reward+totalFees, m.chain.Height()+1)
	txs := make([]*tx.Transaction, 0, 1+len(selected))
	txs = append(txs, coinbase)
	txs = append(txs, selected...)

	// Compute merkle root.
	txHashes := make([]types.Hash, len(txs))
	for i, t := range txs {
		txHashes[i] = t.Hash()
	}
	merkle := block.ComputeMerkleRoot(txHashes)

	header := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   m.chain.TipHash(),
		MerkleRoot: merkle,
		Timestamp:  timestamp,
		Height:     m.chain.Height() + 1,
	}

	if err := m.engine.Prepare(header); err != nil {
		return nil, fmt.Errorf("prepare header: %w", err)
	}

	blk := block.NewBlock(header, txs)

	// Use cancellable sealing if the engine supports it (PoW).
	if pow, ok := m.engine.(*consensus.PoW); ok {
		if err := pow.SealWithCancel(ctx, blk); err != nil {
			return nil, fmt.Errorf("seal block: %w", err)
		}
	} else {
		if err := m.engine.Seal(blk); err != nil {
			return nil, fmt.Errorf("seal block: %w", err)
		}
	}

	return blk, nil
}

// BuildCoinbase creates a coinbase transaction with the given reward.
// The block height is encoded in the coinbase input's signature field
// to ensure each coinbase tx has a unique hash (similar to Bitcoin's BIP34).
func BuildCoinbase(addr types.Address, reward, height uint64) *tx.Transaction {
	// Encode height as little-endian uint64 in the coinbase "signature".
	heightBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(heightBytes, height)

	return &tx.Transaction{
		Version: 1,
		Inputs: []tx.Input{{
			PrevOut:   types.Outpoint{}, // Zero outpoint marks coinbase.
			Signature: heightBytes,
		}},
		Outputs: []tx.Output{{
			Value: reward,
			Script: types.Script{
				Type: types.ScriptTypeP2PKH,
				Data: addr[:],
			},
		}},
	}
}

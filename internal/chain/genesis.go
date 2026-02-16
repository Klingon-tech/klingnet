package chain

import (
	"fmt"
	"sort"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// CreateGenesisBlock builds the genesis block from the genesis configuration.
// The genesis block has height 0, a zero PrevHash, and a single coinbase
// transaction that distributes the initial allocations.
func CreateGenesisBlock(gen *config.Genesis) (*block.Block, error) {
	if gen == nil {
		return nil, fmt.Errorf("genesis config is nil")
	}

	coinbase, err := buildCoinbaseTx(gen.Alloc)
	if err != nil {
		return nil, fmt.Errorf("build coinbase: %w", err)
	}

	txs := []*tx.Transaction{coinbase}
	txHashes := []types.Hash{coinbase.Hash()}
	merkle := block.ComputeMerkleRoot(txHashes)

	header := &block.Header{
		Version:    block.CurrentVersion,
		PrevHash:   types.Hash{}, // Zero for genesis.
		MerkleRoot: merkle,
		Timestamp:  gen.Timestamp,
		Height:     0,
	}

	return block.NewBlock(header, txs), nil
}

// buildCoinbaseTx creates a coinbase transaction with the initial allocations.
// The coinbase has no inputs (it creates coins from nothing).
// Each allocation becomes a P2PKH output. Addresses may be bech32 or raw hex.
func buildCoinbaseTx(alloc map[string]uint64) (*tx.Transaction, error) {
	// Sort addresses for deterministic ordering.
	addrs := make([]string, 0, len(alloc))
	for addr := range alloc {
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs)

	var outputs []tx.Output
	for _, addrStr := range addrs {
		addr, err := types.ParseAddress(addrStr)
		if err != nil {
			return nil, fmt.Errorf("invalid alloc address %q: %w", addrStr, err)
		}

		outputs = append(outputs, tx.Output{
			Value: alloc[addrStr],
			Script: types.Script{
				Type: types.ScriptTypeP2PKH,
				Data: addr.Bytes(),
			},
		})
	}

	// If no allocations, create a single zero-value output so the block has a valid tx.
	if len(outputs) == 0 {
		outputs = []tx.Output{{
			Value: 0,
			Script: types.Script{
				Type: types.ScriptTypeP2PKH,
				Data: make([]byte, types.AddressSize),
			},
		}}
	}

	coinbase := &tx.Transaction{
		Version: 1,
		Inputs: []tx.Input{{
			PrevOut: types.Outpoint{}, // Zero outpoint marks a coinbase.
		}},
		Outputs: outputs,
	}

	return coinbase, nil
}

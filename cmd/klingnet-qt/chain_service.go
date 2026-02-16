package main

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Klingon-tech/klingnet-chain/internal/rpc"
)

// ChainService exposes chain/block/tx queries to the frontend.
type ChainService struct {
	app *App
}

// ChainInfo describes the current chain state.
type ChainInfo struct {
	ChainID string `json:"chain_id"`
	Symbol  string `json:"symbol"`
	Height  uint64 `json:"height"`
	TipHash string `json:"tip_hash"`
}

// BlockInfo describes a block with its transactions.
type BlockInfo struct {
	Hash         string    `json:"hash"`
	PrevHash     string    `json:"prev_hash"`
	MerkleRoot   string    `json:"merkle_root"`
	Timestamp    uint64    `json:"timestamp"`
	Height       uint64    `json:"height"`
	ValidatorSig string    `json:"validator_sig,omitempty"`
	TxCount      int       `json:"tx_count"`
	Transactions []TxBrief `json:"transactions"`
}

// TxBrief summarizes a transaction in a block.
type TxBrief struct {
	Hash       string     `json:"hash"`
	Version    uint32     `json:"version"`
	InputCount int        `json:"input_count"`
	Outputs    []TxOutput `json:"outputs"`
	IsCoinbase bool       `json:"is_coinbase"`
}

// TxOutput describes a transaction output.
type TxOutput struct {
	Value      string `json:"value"`
	ScriptType uint8  `json:"script_type"`
	ScriptData string `json:"script_data"`
}

// TxInfo describes a full transaction.
type TxInfo struct {
	Hash     string     `json:"hash"`
	Version  uint32     `json:"version"`
	Inputs   []TxInput  `json:"inputs"`
	Outputs  []TxOutput `json:"outputs"`
	LockTime uint64     `json:"locktime"`
}

// TxInput describes a transaction input.
type TxInput struct {
	TxID  string `json:"tx_id"`
	Index uint32 `json:"index"`
}

// BlockSummary is a brief block listing.
type BlockSummary struct {
	Height    uint64 `json:"height"`
	Hash      string `json:"hash"`
	Timestamp uint64 `json:"timestamp"`
	TxCount   int    `json:"tx_count"`
}

// GetChainInfo returns the current chain status.
func (c *ChainService) GetChainInfo() (*ChainInfo, error) {
	var result rpc.ChainInfoResult
	if err := c.app.rpcClient().Call("chain_getInfo", nil, &result); err != nil {
		return nil, err
	}
	return &ChainInfo{
		ChainID: result.ChainID,
		Symbol:  result.Symbol,
		Height:  result.Height,
		TipHash: result.TipHash,
	}, nil
}

// GetBlockByHeight returns a block at the given height.
func (c *ChainService) GetBlockByHeight(height uint64) (*BlockInfo, error) {
	var raw json.RawMessage
	if err := c.app.rpcClient().Call("chain_getBlockByHeight", rpc.HeightParam{Height: height}, &raw); err != nil {
		return nil, err
	}
	return c.parseBlock(raw)
}

// GetBlockByHash returns a block with the given hash.
func (c *ChainService) GetBlockByHash(hash string) (*BlockInfo, error) {
	var raw json.RawMessage
	if err := c.app.rpcClient().Call("chain_getBlockByHash", rpc.HashParam{Hash: hash}, &raw); err != nil {
		return nil, err
	}
	return c.parseBlock(raw)
}

// GetTransaction returns details for a transaction hash.
func (c *ChainService) GetTransaction(hash string) (*TxInfo, error) {
	var raw json.RawMessage
	if err := c.app.rpcClient().Call("chain_getTransaction", rpc.HashParam{Hash: hash}, &raw); err != nil {
		return nil, err
	}

	var txn struct {
		Version uint32 `json:"version"`
		Inputs  []struct {
			PrevOut struct {
				TxID  string `json:"tx_id"`
				Index uint32 `json:"index"`
			} `json:"prevout"`
		} `json:"inputs"`
		Outputs []struct {
			Value  uint64 `json:"value"`
			Script struct {
				Type uint8  `json:"type"`
				Data string `json:"data"`
			} `json:"script"`
		} `json:"outputs"`
		LockTime uint64 `json:"locktime"`
	}
	if err := json.Unmarshal(raw, &txn); err != nil {
		return nil, fmt.Errorf("decode tx: %w", err)
	}

	inputs := make([]TxInput, len(txn.Inputs))
	for i, in := range txn.Inputs {
		inputs[i] = TxInput{TxID: in.PrevOut.TxID, Index: in.PrevOut.Index}
	}

	outputs := make([]TxOutput, len(txn.Outputs))
	for i, out := range txn.Outputs {
		outputs[i] = TxOutput{
			Value:      formatAmount(out.Value),
			ScriptType: out.Script.Type,
			ScriptData: out.Script.Data,
		}
	}

	return &TxInfo{
		Hash:     hash,
		Version:  txn.Version,
		Inputs:   inputs,
		Outputs:  outputs,
		LockTime: txn.LockTime,
	}, nil
}

// GetRecentBlocks returns the most recent blocks (up to count).
func (c *ChainService) GetRecentBlocks(count int) ([]BlockSummary, error) {
	info, err := c.GetChainInfo()
	if err != nil {
		return nil, err
	}

	if count <= 0 {
		count = 5
	}

	var blocks []BlockSummary
	for h := int64(info.Height); h >= 0 && len(blocks) < count; h-- {
		blk, err := c.GetBlockByHeight(uint64(h))
		if err != nil {
			break
		}
		blocks = append(blocks, BlockSummary{
			Height:    blk.Height,
			Hash:      blk.Hash,
			Timestamp: blk.Timestamp,
			TxCount:   blk.TxCount,
		})
	}
	return blocks, nil
}

// GetBlockRange returns blocks in a height range (for pagination).
func (c *ChainService) GetBlockRange(from, to uint64) ([]BlockSummary, error) {
	var blocks []BlockSummary
	for h := to; h >= from && h <= to; h-- {
		blk, err := c.GetBlockByHeight(h)
		if err != nil {
			break
		}
		blocks = append(blocks, BlockSummary{
			Height:    blk.Height,
			Hash:      blk.Hash,
			Timestamp: blk.Timestamp,
			TxCount:   blk.TxCount,
		})
	}
	return blocks, nil
}

func (c *ChainService) parseBlock(raw json.RawMessage) (*BlockInfo, error) {
	var blk struct {
		Hash   string `json:"hash"`
		Header struct {
			PrevHash     string `json:"prev_hash"`
			MerkleRoot   string `json:"merkle_root"`
			Timestamp    uint64 `json:"timestamp"`
			Height       uint64 `json:"height"`
			ValidatorSig string `json:"validator_sig,omitempty"`
		} `json:"header"`
		Transactions []json.RawMessage `json:"transactions"`
	}
	if err := json.Unmarshal(raw, &blk); err != nil {
		return nil, fmt.Errorf("decode block: %w", err)
	}

	txs := make([]TxBrief, len(blk.Transactions))
	for i, rawTx := range blk.Transactions {
		var txn struct {
			Version uint32 `json:"version"`
			Inputs  []struct {
				PrevOut struct {
					TxID  string `json:"tx_id"`
					Index uint32 `json:"index"`
				} `json:"prevout"`
			} `json:"inputs"`
			Outputs []struct {
				Value  uint64 `json:"value"`
				Script struct {
					Type uint8  `json:"type"`
					Data string `json:"data"`
				} `json:"script"`
			} `json:"outputs"`
		}
		if err := json.Unmarshal(rawTx, &txn); err != nil {
			continue
		}

		isCoinbase := len(txn.Inputs) > 0 && txn.Inputs[0].PrevOut.TxID == "" || (len(txn.Inputs) > 0 && isZeroHash(txn.Inputs[0].PrevOut.TxID))

		outputs := make([]TxOutput, len(txn.Outputs))
		for j, out := range txn.Outputs {
			outputs[j] = TxOutput{
				Value:      formatAmount(out.Value),
				ScriptType: out.Script.Type,
				ScriptData: out.Script.Data,
			}
		}

		txs[i] = TxBrief{
			Hash:       hashFromIndex(i),
			Version:    txn.Version,
			InputCount: len(txn.Inputs),
			Outputs:    outputs,
			IsCoinbase: isCoinbase,
		}
	}

	return &BlockInfo{
		Hash:         blk.Hash,
		PrevHash:     blk.Header.PrevHash,
		MerkleRoot:   blk.Header.MerkleRoot,
		Timestamp:    blk.Header.Timestamp,
		Height:       blk.Header.Height,
		ValidatorSig: blk.Header.ValidatorSig,
		TxCount:      len(blk.Transactions),
		Transactions: txs,
	}, nil
}

func isZeroHash(s string) bool {
	for _, c := range s {
		if c != '0' {
			return false
		}
	}
	return len(s) > 0
}

func hashFromIndex(i int) string {
	return "tx-" + strconv.Itoa(i)
}

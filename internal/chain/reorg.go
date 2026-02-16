package chain

import (
	"encoding/json"
	"fmt"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// UndoData stores the information needed to revert a block's UTXO changes.
type UndoData struct {
	SpentUTXOs       []utxo.UTXO      `json:"spent_utxos"`
	CreatedOutpoints []types.Outpoint `json:"created_outpoints"`
	TxHashes         []types.Hash     `json:"tx_hashes"`
	BlockReward      uint64           `json:"block_reward"`
}

// ErrForkDetected indicates a valid block whose parent is known but is not the
// current tip. The caller should decide whether to reorg.
var ErrForkDetected = fmt.Errorf("fork detected")

// ErrReorgTooDeep is returned when a reorg exceeds MaxReorgDepth.
var ErrReorgTooDeep = fmt.Errorf("reorg too deep")

// ErrGenesisReorg is returned when a reorg would replace the genesis block.
var ErrGenesisReorg = fmt.Errorf("reorg would replace genesis block")

// MaxReorgDepth is the maximum number of blocks that can be reverted in a reorg.
const MaxReorgDepth = 1000

// applyBlockWithUndo applies a block to the UTXO set and returns undo data.
func (c *Chain) applyBlockWithUndo(blk *block.Block) (*UndoData, error) {
	undo := &UndoData{}

	for txIdx, transaction := range blk.Transactions {
		txHash := transaction.Hash()
		undo.TxHashes = append(undo.TxHashes, txHash)
		isCoinbase := txIdx == 0 && blk.Header.Height > 0

		// Detect if this tx spends any stake UTXOs → lock return outputs.
		var lockedUntil uint64
		for _, in := range transaction.Inputs {
			if in.PrevOut.IsZero() {
				continue
			}
			u, err := c.utxos.Get(in.PrevOut)
			if err == nil && u.Script.Type == types.ScriptTypeStake {
				lockedUntil = blk.Header.Height + config.UnstakeCooldown
				break
			}
		}

		// Spend inputs — save UTXO before deleting for undo.
		for _, in := range transaction.Inputs {
			if in.PrevOut.IsZero() {
				continue
			}
			u, err := c.utxos.Get(in.PrevOut)
			if err != nil {
				return nil, fmt.Errorf("get utxo for undo %s: %w", in.PrevOut, err)
			}
			undo.SpentUTXOs = append(undo.SpentUTXOs, *u)

			if err := c.utxos.Delete(in.PrevOut); err != nil {
				return nil, fmt.Errorf("spend %s: %w", in.PrevOut, err)
			}
		}

		// Create outputs.
		for i, out := range transaction.Outputs {
			op := types.Outpoint{TxID: txHash, Index: uint32(i)}
			undo.CreatedOutpoints = append(undo.CreatedOutpoints, op)

			u := &utxo.UTXO{
				Outpoint:    op,
				Value:       out.Value,
				Script:      out.Script,
				Token:       out.Token,
				Height:      blk.Header.Height,
				Coinbase:    isCoinbase,
				LockedUntil: lockedUntil,
			}
			if err := c.utxos.Put(u); err != nil {
				return nil, fmt.Errorf("create output %s:%d: %w", txHash, i, err)
			}
		}
	}

	return undo, nil
}

// revertBlock undoes a block's UTXO changes using stored undo data.
func (c *Chain) revertBlock(undo *UndoData) error {
	// Delete created outputs (reverse order for safety).
	for i := len(undo.CreatedOutpoints) - 1; i >= 0; i-- {
		if err := c.utxos.Delete(undo.CreatedOutpoints[i]); err != nil {
			return fmt.Errorf("delete created output %s: %w", undo.CreatedOutpoints[i], err)
		}
	}

	// Restore spent UTXOs.
	for i := range undo.SpentUTXOs {
		if err := c.utxos.Put(&undo.SpentUTXOs[i]); err != nil {
			return fmt.Errorf("restore utxo %s: %w", undo.SpentUTXOs[i].Outpoint, err)
		}
	}

	// Remove tx index entries.
	for _, txHash := range undo.TxHashes {
		if err := c.blocks.DeleteTxIndex(txHash); err != nil {
			return fmt.Errorf("delete tx index %s: %w", txHash, err)
		}
	}

	return nil
}

// Reorg switches the chain from the current tip to the new tip.
// It finds the common ancestor, reverts old blocks, and replays new blocks.
// For PoW chains, the reorg only proceeds if the new branch has more
// cumulative work than the old branch.
func (c *Chain) Reorg(newTipHash types.Hash) error {
	// Collect the new branch (from newTip back to common ancestor).
	newBranch, err := c.collectBranch(newTipHash)
	if err != nil {
		return fmt.Errorf("collect new branch: %w", err)
	}
	if len(newBranch) == 0 {
		return fmt.Errorf("empty new branch")
	}

	// The fork height is one below the first block in the new branch.
	forkHeight := newBranch[0].Header.Height - 1
	oldHeight := c.state.Height

	// Compare cumulative work (applies to both PoA and PoW).
	// For PoA: in-turn blocks have Difficulty=2, out-of-turn have Difficulty=1,
	// so the in-turn chain always wins. Equal work → keep current (no flip-flopping).
	var newBranchWork, oldBranchWork uint64
	for _, blk := range newBranch {
		newBranchWork += blk.Header.Difficulty
	}
	for h := forkHeight + 1; h <= oldHeight; h++ {
		blk, err := c.blocks.GetBlockByHeight(h)
		if err != nil {
			return fmt.Errorf("load old block for work comparison at height %d: %w", h, err)
		}
		oldBranchWork += blk.Header.Difficulty
	}
	if newBranchWork <= oldBranchWork {
		return nil // New branch doesn't have more work — keep current chain.
	}

	// Write reorg checkpoint so we can recover if the node crashes mid-reorg.
	if err := c.blocks.PutReorgCheckpoint(forkHeight); err != nil {
		return fmt.Errorf("write reorg checkpoint: %w", err)
	}

	// Collect reverted non-coinbase transactions for mempool re-insertion.
	var revertedTxs []*tx.Transaction

	// Revert old blocks from current tip down to fork point.
	for h := oldHeight; h > forkHeight; h-- {
		blk, err := c.blocks.GetBlockByHeight(h)
		if err != nil {
			return fmt.Errorf("load old block at height %d: %w", h, err)
		}
		bHash := blk.Hash()
		undoBytes, err := c.blocks.GetUndo(bHash)
		if err != nil {
			return fmt.Errorf("load undo for block %s: %w", bHash, err)
		}
		var undo UndoData
		if err := json.Unmarshal(undoBytes, &undo); err != nil {
			return fmt.Errorf("unmarshal undo for block %s: %w", bHash, err)
		}

		if err := c.revertBlock(&undo); err != nil {
			return fmt.Errorf("revert block %s: %w", bHash, err)
		}

		// Notify sub-chain manager about reverted registrations.
		if c.deregistrationHandler != nil {
			for _, transaction := range blk.Transactions {
				txHash := transaction.Hash()
				for i, out := range transaction.Outputs {
					if out.Script.Type == types.ScriptTypeRegister {
						c.deregistrationHandler(txHash, uint32(i))
					}
				}
			}
		}

		// Undo stake creations: created stake outputs are being deleted → unstake.
		if c.unstakeHandler != nil {
			for _, transaction := range blk.Transactions {
				for _, out := range transaction.Outputs {
					if out.Script.Type == types.ScriptTypeStake && len(out.Script.Data) == 33 {
						c.unstakeHandler(out.Script.Data)
					}
				}
			}
		}

		// Undo stake spends: spent stake UTXOs are being restored → re-stake.
		if c.stakeHandler != nil {
			for i := range undo.SpentUTXOs {
				su := &undo.SpentUTXOs[i]
				if su.Script.Type == types.ScriptTypeStake && len(su.Script.Data) == 33 {
					c.stakeHandler(su.Script.Data)
				}
			}
		}

		// Collect non-coinbase transactions for mempool re-insertion.
		if c.revertedTxHandler != nil && len(blk.Transactions) > 1 {
			revertedTxs = append(revertedTxs, blk.Transactions[1:]...)
		}

		if undo.BlockReward > c.state.Supply {
			return fmt.Errorf("supply underflow at height %d: reward %d > supply %d", h, undo.BlockReward, c.state.Supply)
		}
		c.state.Supply -= undo.BlockReward
		c.state.CumulativeDifficulty -= blk.Header.Difficulty

		if err := c.blocks.DeleteUndo(bHash); err != nil {
			return fmt.Errorf("delete undo for block %s: %w", bHash, err)
		}
	}

	// Replay new branch blocks with full validation.
	for _, blk := range newBranch {
		// Validate structure + consensus (signatures, merkle, header sig).
		if err := c.validator.ValidateBlock(blk); err != nil {
			return fmt.Errorf("validate replay block at height %d: %w", blk.Header.Height, err)
		}

		// Verify PoW difficulty if applicable.
		if err := c.verifyDifficulty(blk); err != nil {
			return fmt.Errorf("difficulty check replay block at height %d: %w", blk.Header.Height, err)
		}

		// Validate UTXO-dependent rules (tx signatures, maturity, tokens, stakes).
		if err := c.validateBlockState(blk); err != nil {
			return fmt.Errorf("state validation replay block at height %d: %w", blk.Header.Height, err)
		}

		blockReward := c.computeBlockReward(blk)

		undo, err := c.applyBlockWithUndo(blk)
		if err != nil {
			return fmt.Errorf("apply new block at height %d: %w", blk.Header.Height, err)
		}
		undo.BlockReward = blockReward

		// PutBlock indexes txs and height; safe to call even if block was stored.
		if err := c.blocks.PutBlock(blk); err != nil {
			return fmt.Errorf("store new block: %w", err)
		}

		undoBytes, err := json.Marshal(undo)
		if err != nil {
			return fmt.Errorf("marshal undo: %w", err)
		}
		if err := c.blocks.PutUndo(blk.Hash(), undoBytes); err != nil {
			return fmt.Errorf("store undo: %w", err)
		}

		// Cap block reward to respect max supply and prevent overflow.
		if c.maxSupply > 0 && c.state.Supply+blockReward > c.maxSupply {
			blockReward = c.maxSupply - c.state.Supply
		}
		if c.state.Supply > ^uint64(0)-blockReward {
			return fmt.Errorf("supply overflow at height %d: supply %d + reward %d", blk.Header.Height, c.state.Supply, blockReward)
		}

		c.state.Supply += blockReward
		c.state.CumulativeDifficulty += blk.Header.Difficulty

		// Fire registration handler for any registrations in the new branch.
		if c.registrationHandler != nil {
			for _, transaction := range blk.Transactions {
				txHash := transaction.Hash()
				for i, out := range transaction.Outputs {
					if out.Script.Type == types.ScriptTypeRegister {
						c.registrationHandler(txHash, uint32(i), out.Value, out.Script.Data, blk.Header.Height)
					}
				}
			}
		}

		// Fire stake handler for any stakes in the new branch.
		if c.stakeHandler != nil {
			for _, transaction := range blk.Transactions {
				for _, out := range transaction.Outputs {
					if out.Script.Type == types.ScriptTypeStake && len(out.Script.Data) == 33 {
						c.stakeHandler(out.Script.Data)
					}
				}
			}
		}

		// Fire unstake handler for spent stakes in the new branch.
		if c.unstakeHandler != nil {
			for i := range undo.SpentUTXOs {
				su := &undo.SpentUTXOs[i]
				if su.Script.Type == types.ScriptTypeStake && len(su.Script.Data) == 33 {
					c.unstakeHandler(su.Script.Data)
				}
			}
		}
	}

	// Update tip to new branch head.
	tip := newBranch[len(newBranch)-1]
	tipHash := tip.Hash()
	c.state.TipHash = tipHash
	c.state.Height = tip.Header.Height
	c.state.TipTimestamp = tip.Header.Timestamp
	if err := c.blocks.SetTip(tipHash, tip.Header.Height, c.state.Supply); err != nil {
		return fmt.Errorf("set new tip: %w", err)
	}
	if err := c.blocks.SetCumulativeDifficulty(c.state.CumulativeDifficulty); err != nil {
		return fmt.Errorf("set cumulative difficulty: %w", err)
	}

	// Reorg complete — remove the crash-recovery checkpoint.
	if err := c.blocks.DeleteReorgCheckpoint(); err != nil {
		return fmt.Errorf("delete reorg checkpoint: %w", err)
	}

	// Return reverted transactions to mempool (excluding any that appear in the new branch).
	if c.revertedTxHandler != nil && len(revertedTxs) > 0 {
		// Build a set of tx hashes in the new branch to filter conflicts.
		newBranchTxs := make(map[types.Hash]bool)
		for _, blk := range newBranch {
			for _, t := range blk.Transactions {
				newBranchTxs[t.Hash()] = true
			}
		}
		var toReturn []*tx.Transaction
		for _, t := range revertedTxs {
			if !newBranchTxs[t.Hash()] {
				toReturn = append(toReturn, t)
			}
		}
		if len(toReturn) > 0 {
			c.revertedTxHandler(toReturn)
		}
	}

	return nil
}

// collectBranch collects blocks from the given hash back to the fork point
// (common ancestor with the current main chain).
// Returns blocks in ascending height order (fork+1 ... newTip).
func (c *Chain) collectBranch(tipHash types.Hash) ([]*block.Block, error) {
	var branch []*block.Block
	hash := tipHash

	for {
		blk, err := c.blocks.GetBlock(hash)
		if err != nil {
			return nil, fmt.Errorf("load block %s: %w", hash, err)
		}
		branch = append(branch, blk)

		if len(branch) > MaxReorgDepth {
			return nil, fmt.Errorf("%w: branch exceeds %d blocks", ErrReorgTooDeep, MaxReorgDepth)
		}

		// If this block's parent is on the main chain at (height-1), we found the fork.
		if blk.Header.Height == 0 {
			// Reject reorgs that would replace the genesis block.
			if !c.genesisHash.IsZero() && blk.Hash() != c.genesisHash {
				return nil, ErrGenesisReorg
			}
			break
		}
		parentHeight := blk.Header.Height - 1
		mainBlock, err := c.blocks.GetBlockByHeight(parentHeight)
		if err != nil {
			break // No block at this height on main chain.
		}
		if mainBlock.Hash() == blk.Header.PrevHash {
			break // Common ancestor found.
		}

		hash = blk.Header.PrevHash
	}

	// Reverse to ascending order.
	for i, j := 0, len(branch)-1; i < j; i, j = i+1, j-1 {
		branch[i], branch[j] = branch[j], branch[i]
	}

	return branch, nil
}

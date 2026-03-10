// Package chain implements the blockchain state machine.
package chain

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// RegistrationHandler is called when a ScriptTypeRegister output is found in a confirmed block.
// The value parameter is the output's KGX value (burn amount).
type RegistrationHandler func(txHash types.Hash, outputIndex uint32, value uint64, scriptData []byte, height uint64)

// RegistrationValidator is called during block validation for each
// ScriptTypeRegister output before the block is committed.
// existingRegistrations is the active-chain count before this block.
// pendingRegistrations is the number of earlier register outputs already
// validated in the same block.
type RegistrationValidator func(output tx.Output, existingRegistrations, pendingRegistrations uint64) error

// DeregistrationHandler is called when a ScriptTypeRegister output is reverted during a reorg.
type DeregistrationHandler func(txHash types.Hash, outputIndex uint32)

// StakeHandler is called when a ScriptTypeStake output is found in a confirmed block.
type StakeHandler func(pubKey []byte)

// UnstakeHandler is called when a ScriptTypeStake output is spent (stake withdrawn).
type UnstakeHandler func(pubKey []byte)

// RevertedTxHandler is called after a reorg with transactions from reverted blocks
// that are not present in the new branch (for mempool re-insertion).
type RevertedTxHandler func(txs []*tx.Transaction)

// ReorgHandler is called after a reorg completes. Used to trigger
// suspension state reconstruction from on-chain blocks.
type ReorgHandler func()

// Chain represents a blockchain instance with state, storage, and consensus.
type Chain struct {
	mu        sync.Mutex // Protects all state mutations (ProcessBlock, Reorg).
	ID        types.ChainID
	state     *State
	blocks    *BlockStore
	utxos     utxo.Set
	engine    consensus.Engine
	validator *consensus.Validator

	maxSupply           uint64     // Max coin supply (0 = unlimited).
	blockReward         uint64     // Base block subsidy in base units.
	halvingInterval     uint64     // Blocks between reward halvings (0 = disabled).
	validatorStake      uint64     // Exact stake amount required (0 = disabled).
	allowMinting        bool       // Whether new token issuance is allowed.
	registeredSubChains uint64     // Active ScriptTypeRegister outputs on the current chain.
	genesisHash         types.Hash // Hash of the genesis block (immutable).

	registrationValidator RegistrationValidator
	registrationHandler   RegistrationHandler
	deregistrationHandler DeregistrationHandler
	stakeHandler          StakeHandler
	unstakeHandler        UnstakeHandler
	revertedTxHandler     RevertedTxHandler
	reorgHandler          ReorgHandler
}

// New creates a new chain with the given components.
func New(id types.ChainID, db storage.DB, utxoSet utxo.Set, engine consensus.Engine) (*Chain, error) {
	if db == nil {
		return nil, fmt.Errorf("storage db is nil")
	}
	if utxoSet == nil {
		return nil, fmt.Errorf("utxo set is nil")
	}
	if engine == nil {
		return nil, fmt.Errorf("consensus engine is nil")
	}

	blocks := NewBlockStore(db)

	// Recover state from the block store.
	tipHash, height, supply, err := blocks.GetTip()
	if err != nil {
		return nil, fmt.Errorf("recover tip: %w", err)
	}

	cumDiff := blocks.GetCumulativeDifficulty()

	// Recover genesis hash and tip timestamp for reorg protection.
	var genesisHash types.Hash
	var tipTimestamp uint64
	genBlk, err := blocks.GetBlockByHeight(0)
	if err == nil {
		genesisHash = genBlk.Hash()
	}
	if tipBlk, err := blocks.GetBlock(tipHash); err == nil {
		tipTimestamp = tipBlk.Header.Timestamp
	}
	registeredSubChains, err := countRegisteredSubChains(utxoSet)
	if err != nil {
		return nil, fmt.Errorf("recover sub-chain registrations: %w", err)
	}

	ch := &Chain{
		ID:                  id,
		state:               &State{TipHash: tipHash, Height: height, Supply: supply, CumulativeDifficulty: cumDiff, TipTimestamp: tipTimestamp},
		blocks:              blocks,
		utxos:               utxoSet,
		engine:              engine,
		validator:           consensus.NewValidator(engine),
		registeredSubChains: registeredSubChains,
		genesisHash:         genesisHash,
	}

	// Check for incomplete reorg — if the node crashed mid-reorg, the UTXO
	// set may be inconsistent. Rebuild from blocks.
	if _, found := blocks.GetReorgCheckpoint(); found {
		if err := ch.RebuildUTXOs(); err != nil {
			return nil, fmt.Errorf("recover from interrupted reorg: %w", err)
		}
	}

	return ch, nil
}

// InitFromGenesis initializes a fresh chain from genesis configuration.
// Returns an error if the chain already has blocks.
func (c *Chain) InitFromGenesis(gen *config.Genesis) error {
	if !c.state.IsGenesis() {
		return fmt.Errorf("chain already initialized at height %d", c.state.Height)
	}

	// Compute initial supply from genesis allocations with overflow protection.
	var supply uint64
	for _, v := range gen.Alloc {
		if supply > ^uint64(0)-v {
			return fmt.Errorf("genesis allocation overflow: supply %d + alloc %d exceeds uint64", supply, v)
		}
		supply += v
	}

	blk, err := CreateGenesisBlock(gen)
	if err != nil {
		return fmt.Errorf("create genesis: %w", err)
	}

	// Genesis block bypasses consensus validation (no validator sig needed).
	// Apply directly: store block, apply UTXOs, set tip.
	if err := c.applyBlock(blk); err != nil {
		return fmt.Errorf("apply genesis: %w", err)
	}

	if err := c.blocks.PutBlock(blk); err != nil {
		return fmt.Errorf("store genesis: %w", err)
	}

	hash := blk.Hash()
	c.state.TipHash = hash
	c.state.Height = 0
	c.state.Supply = supply
	c.state.TipTimestamp = blk.Header.Timestamp
	c.genesisHash = hash

	// Store protocol limits from genesis.
	c.maxSupply = gen.Protocol.Consensus.MaxSupply
	c.blockReward = gen.Protocol.Consensus.BlockReward
	c.halvingInterval = gen.Protocol.Consensus.HalvingInterval
	c.validatorStake = gen.Protocol.Consensus.ValidatorStake
	c.allowMinting = gen.Protocol.Token.AllowMinting
	c.registeredSubChains = 0

	if err := c.blocks.SetTip(hash, 0, supply); err != nil {
		return fmt.Errorf("set genesis tip: %w", err)
	}

	return nil
}

// SetConsensusRules configures consensus economic limits for runtime validation.
// Call this on startup for both fresh and resumed chains.
func (c *Chain) SetConsensusRules(r config.ConsensusRules) {
	c.maxSupply = r.MaxSupply
	c.blockReward = r.BlockReward
	c.halvingInterval = r.HalvingInterval
	c.validatorStake = r.ValidatorStake
}

// SetTokenRules configures token minting rules for runtime validation.
func (c *Chain) SetTokenRules(r config.TokenRules) {
	c.allowMinting = r.AllowMinting
}

// SetRegistrationValidator configures consensus validation for registration outputs.
func (c *Chain) SetRegistrationValidator(fn RegistrationValidator) {
	c.registrationValidator = fn
}

// State returns a copy of the current chain state.
func (c *Chain) State() State {
	return *c.state
}

// GetBlock retrieves a block by its hash.
func (c *Chain) GetBlock(hash types.Hash) (*block.Block, error) {
	return c.blocks.GetBlock(hash)
}

// GetBlockByHeight retrieves a block by its height.
func (c *Chain) GetBlockByHeight(height uint64) (*block.Block, error) {
	return c.blocks.GetBlockByHeight(height)
}

// Height returns the current chain height.
func (c *Chain) Height() uint64 {
	return c.state.Height
}

// TipHash returns the hash of the current chain tip.
func (c *Chain) TipHash() types.Hash {
	return c.state.TipHash
}

// TipTimestamp returns the timestamp of the current tip block.
func (c *Chain) TipTimestamp() uint64 {
	return c.state.TipTimestamp
}

// Supply returns the total coins in circulation.
func (c *Chain) Supply() uint64 {
	return c.state.Supply
}

// RebuildIndexes delegates to BlockStore.RebuildIndexes to rebuild
// all height and transaction indexes by walking the chain from tip.
func (c *Chain) RebuildIndexes() (int, error) {
	return c.blocks.RebuildIndexes()
}

// SetRegistrationHandler sets the callback for ScriptTypeRegister outputs in confirmed blocks.
func (c *Chain) SetRegistrationHandler(fn RegistrationHandler) {
	c.registrationHandler = fn
}

// SetDeregistrationHandler sets the callback for ScriptTypeRegister outputs reverted during a reorg.
func (c *Chain) SetDeregistrationHandler(fn DeregistrationHandler) {
	c.deregistrationHandler = fn
}

// SetStakeHandler sets the callback for ScriptTypeStake outputs in confirmed blocks.
func (c *Chain) SetStakeHandler(fn StakeHandler) {
	c.stakeHandler = fn
}

// SetUnstakeHandler sets the callback for ScriptTypeStake outputs being spent (stake withdrawn).
func (c *Chain) SetUnstakeHandler(fn UnstakeHandler) {
	c.unstakeHandler = fn
}

// SetRevertedTxHandler sets the callback for transactions reverted during a reorg.
// These transactions should be re-added to the mempool if they are still valid.
func (c *Chain) SetRevertedTxHandler(fn RevertedTxHandler) {
	c.revertedTxHandler = fn
}

// SetReorgHandler sets the callback invoked after a reorg completes.
func (c *Chain) SetReorgHandler(fn ReorgHandler) {
	c.reorgHandler = fn
}

// getBlockTimestamp returns the timestamp of a block at the given height.
// Used for PoW difficulty verification.
func (c *Chain) getBlockTimestamp(height uint64) (uint64, error) {
	blk, err := c.blocks.GetBlockByHeight(height)
	if err != nil {
		return 0, err
	}
	return blk.Header.Timestamp, nil
}

// verifyDifficulty checks that a PoW block's stated difficulty matches
// the expected value computed from chain history. No-op for non-PoW engines.
func (c *Chain) verifyDifficulty(blk *block.Block) error {
	pow, ok := c.engine.(*consensus.PoW)
	if !ok {
		return nil // Not PoW — no difficulty to verify.
	}

	var prevDifficulty uint64
	if blk.Header.Height > 1 {
		prevBlk, err := c.blocks.GetBlockByHeight(blk.Header.Height - 1)
		if err != nil {
			return fmt.Errorf("get prev block for difficulty: %w", err)
		}
		prevDifficulty = prevBlk.Header.Difficulty
	}

	return pow.VerifyDifficulty(blk.Header, prevDifficulty, c.getBlockTimestamp)
}

// RebuildUTXOs clears the UTXO set and replays all blocks from genesis to the
// current tip, reconstructing the UTXO state. Used to recover from a crash
// during reorg where the UTXO set may be inconsistent.
func (c *Chain) RebuildUTXOs() error {
	store, ok := c.utxos.(*utxo.Store)
	if !ok {
		return fmt.Errorf("UTXO set does not support ClearAll (not *utxo.Store)")
	}

	if err := store.ClearAll(); err != nil {
		return fmt.Errorf("clear utxo set: %w", err)
	}

	// Replay all blocks from genesis to current tip, storing undo data
	// so that future reorgs can revert blocks properly.
	var supply uint64
	var cumDiff uint64
	c.registeredSubChains = 0
	for h := uint64(0); h <= c.state.Height; h++ {
		blk, err := c.blocks.GetBlockByHeight(h)
		if err != nil {
			return fmt.Errorf("load block at height %d: %w", h, err)
		}

		blockReward := c.computeBlockReward(blk)

		undo, err := c.applyBlockWithUndo(blk)
		if err != nil {
			return fmt.Errorf("replay block at height %d: %w", h, err)
		}
		undo.BlockReward = blockReward

		undoBytes, err := json.Marshal(undo)
		if err != nil {
			return fmt.Errorf("marshal undo at height %d: %w", h, err)
		}
		if err := c.blocks.PutUndo(blk.Hash(), undoBytes); err != nil {
			return fmt.Errorf("store undo at height %d: %w", h, err)
		}

		if c.maxSupply > 0 {
			if supply >= c.maxSupply {
				blockReward = 0
			} else if remaining := c.maxSupply - supply; blockReward > remaining {
				blockReward = remaining
			}
		}
		if blockReward > ^uint64(0)-supply {
			return fmt.Errorf("rebuild utxos: supply overflow at height %d: supply %d + reward %d", h, supply, blockReward)
		}
		if blk.Header.Difficulty > ^uint64(0)-cumDiff {
			return fmt.Errorf("rebuild utxos: cumulative difficulty overflow at height %d: cumdiff %d + difficulty %d",
				h, cumDiff, blk.Header.Difficulty)
		}

		supply += blockReward
		cumDiff += blk.Header.Difficulty
		if err := c.addRegisteredSubChains(countRegisterOutputs(blk)); err != nil {
			return fmt.Errorf("rebuild utxos: %w", err)
		}
	}

	c.state.Supply = supply
	c.state.CumulativeDifficulty = cumDiff

	// Persist recovered state.
	if err := c.blocks.SetTip(c.state.TipHash, c.state.Height, supply); err != nil {
		return fmt.Errorf("set tip after rebuild: %w", err)
	}
	if err := c.blocks.SetCumulativeDifficulty(cumDiff); err != nil {
		return fmt.Errorf("set cumulative difficulty after rebuild: %w", err)
	}

	// Clear the checkpoint — recovery complete.
	if err := c.blocks.DeleteReorgCheckpoint(); err != nil {
		return fmt.Errorf("delete reorg checkpoint: %w", err)
	}

	return nil
}

type utxoIterator interface {
	ForEach(func(*utxo.UTXO) error) error
}

func countRegisteredSubChains(set utxo.Set) (uint64, error) {
	iter, ok := set.(utxoIterator)
	if !ok {
		return 0, fmt.Errorf("utxo set does not support iteration")
	}

	var count uint64
	if err := iter.ForEach(func(u *utxo.UTXO) error {
		if u.Script.Type != types.ScriptTypeRegister {
			return nil
		}
		if count == ^uint64(0) {
			return fmt.Errorf("registration count overflow")
		}
		count++
		return nil
	}); err != nil {
		return 0, err
	}
	return count, nil
}

// isPoWEngine returns true if the chain uses proof-of-work consensus.
func (c *Chain) isPoWEngine() bool {
	_, ok := c.engine.(*consensus.PoW)
	return ok
}

// GetTransaction looks up a confirmed transaction by hash via the tx index.
func (c *Chain) GetTransaction(hash types.Hash) (*tx.Transaction, error) {
	_, blockHash, err := c.blocks.GetTxLocation(hash)
	if err != nil {
		return nil, err
	}
	blk, err := c.blocks.GetBlock(blockHash)
	if err != nil {
		return nil, fmt.Errorf("load block for tx: %w", err)
	}
	for _, t := range blk.Transactions {
		if t.Hash() == hash {
			return t, nil
		}
	}
	return nil, fmt.Errorf("tx %s not found in block %s (index corrupt)", hash, blockHash)
}

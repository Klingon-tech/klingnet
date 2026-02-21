package chain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/consensus"
	"github.com/Klingon-tech/klingnet-chain/internal/token"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// Block processing errors.
var (
	ErrBlockKnown             = errors.New("block already known")
	ErrPrevNotFound           = errors.New("previous block not found")
	ErrBadHeight              = errors.New("block height does not follow parent")
	ErrBadPrevHash            = errors.New("prev_hash does not match current tip")
	ErrApplyUTXO              = errors.New("failed to apply UTXO changes")
	ErrCoinbaseNotMature      = errors.New("coinbase output not mature")
	ErrTimestampTooFuture     = errors.New("block timestamp too far in the future")
	ErrTimestampBeforeParent  = errors.New("block timestamp before parent")
	ErrInvalidStakeAmount     = errors.New("invalid stake amount")
	ErrBadCoinbaseTx          = errors.New("invalid coinbase transaction")
	ErrCoinbaseRewardExceeded = errors.New("coinbase reward exceeds consensus limit")
	ErrSigningLimitExceeded   = errors.New("validator exceeded signing limit")
)

// ProcessBlock validates a block and applies it to the chain.
// It checks structural validity, consensus rules, UTXO state, then
// updates the UTXO set, block store, and chain tip.
// If the block extends a fork that is longer than the current chain, a
// reorg is triggered automatically.
func (c *Chain) ProcessBlock(blk *block.Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if blk == nil || blk.Header == nil {
		return fmt.Errorf("nil block or header")
	}

	hash := blk.Hash()

	// Reject duplicates.
	known, err := c.blocks.HasBlock(hash)
	if err != nil {
		return fmt.Errorf("check block: %w", err)
	}
	if known {
		return ErrBlockKnown
	}

	// Check parent linkage first — we need the correct height before
	// verifying difficulty and running consensus validation.
	parentErr := c.checkParentLink(blk)
	if parentErr != nil && !errors.Is(parentErr, ErrForkDetected) {
		return parentErr
	}

	// Verify PoW difficulty matches expected (from chain history).
	// Only on fast path — fork blocks are verified during reorg replay.
	if !errors.Is(parentErr, ErrForkDetected) {
		if err := c.verifyDifficulty(blk); err != nil {
			return err
		}
	}

	// Structural + consensus validation (VerifyHeader checks hash vs header.Difficulty).
	if err := c.validator.ValidateBlock(blk); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	// Note: Signing limit is NOT checked here on the fast path.
	// Blocks received from peers during sync were already accepted by the network.
	// The signing limit is enforced in:
	//   - Miner pre-check (IsSigningLimitReached) — prevents local violations
	//   - Reorg replay — prevents rogue validators from forcing reorgs

	// Block timestamp bounds: reject blocks too far in the future.
	maxTime := uint64(time.Now().Add(2 * time.Minute).Unix())
	if blk.Header.Timestamp > maxTime {
		return fmt.Errorf("%w: block timestamp %d exceeds max %d", ErrTimestampTooFuture, blk.Header.Timestamp, maxTime)
	}

	// Block timestamp must not be before its parent (monotonic).
	if blk.Header.Height > 0 {
		parentBlk, err := c.blocks.GetBlock(blk.Header.PrevHash)
		if err == nil && blk.Header.Timestamp < parentBlk.Header.Timestamp {
			return fmt.Errorf("%w: block timestamp %d < parent timestamp %d",
				ErrTimestampBeforeParent, blk.Header.Timestamp, parentBlk.Header.Timestamp)
		}
	}

	// Fork detected: store the block and decide whether to reorg.
	if errors.Is(parentErr, ErrForkDetected) {
		// Store block data only (no height/tx indexes yet).
		if err := c.blocks.StoreBlock(blk); err != nil {
			return fmt.Errorf("store fork block: %w", err)
		}

		// Decide whether to attempt reorg.
		// Same-height or longer forks are candidates — Reorg itself compares
		// cumulative difficulty to decide (works for both PoA and PoW).
		shouldAttempt := blk.Header.Height >= c.state.Height
		if c.isPoWEngine() {
			shouldAttempt = true // PoW: difficulty variations can make shorter chains heavier.
		}
		if shouldAttempt {
			if err := c.Reorg(hash); err != nil {
				return fmt.Errorf("reorg: %w", err)
			}
		}
		// If the reorg didn't proceed, the block is stored but not active.
		return nil
	}

	// Fast path: block extends current tip.

	// Validate UTXO-dependent rules (signatures, maturity, tokens, stakes).
	if err := c.validateBlockState(blk); err != nil {
		return err
	}

	// Compute block reward (new coins) before applying, while inputs are
	// still in the UTXO set. reward = coinbase_value - total_fees.
	blockReward := c.computeBlockReward(blk)

	// Apply UTXO changes and collect undo data.
	undo, err := c.applyBlockWithUndo(blk)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrApplyUTXO, err)
	}
	undo.BlockReward = blockReward

	// Persist the block.
	if err := c.blocks.PutBlock(blk); err != nil {
		return fmt.Errorf("store block: %w", err)
	}

	// Persist undo data.
	undoBytes, err := json.Marshal(undo)
	if err != nil {
		return fmt.Errorf("marshal undo: %w", err)
	}
	if err := c.blocks.PutUndo(hash, undoBytes); err != nil {
		return fmt.Errorf("store undo: %w", err)
	}

	// Cap block reward to respect max supply.
	if c.maxSupply > 0 && c.state.Supply+blockReward > c.maxSupply {
		blockReward = c.maxSupply - c.state.Supply
	}

	// Track newly minted coins (block reward only; fees are recycled).
	c.state.Supply += blockReward
	c.state.CumulativeDifficulty += blk.Header.Difficulty

	// Update chain tip.
	c.state.TipHash = hash
	c.state.Height = blk.Header.Height
	c.state.TipTimestamp = blk.Header.Timestamp
	if err := c.blocks.SetTip(hash, blk.Header.Height, c.state.Supply); err != nil {
		return fmt.Errorf("set tip: %w", err)
	}
	if err := c.blocks.SetCumulativeDifficulty(c.state.CumulativeDifficulty); err != nil {
		return fmt.Errorf("set cumulative difficulty: %w", err)
	}

	// Scan for sub-chain registration outputs.
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

	// Scan for stake outputs → register new validators.
	if c.stakeHandler != nil {
		for _, transaction := range blk.Transactions {
			for _, out := range transaction.Outputs {
				if out.Script.Type == types.ScriptTypeStake && len(out.Script.Data) == 33 {
					c.stakeHandler(out.Script.Data)
				}
			}
		}
	}

	// Scan for spent stake UTXOs → fire unstake handler.
	if c.unstakeHandler != nil {
		for i := range undo.SpentUTXOs {
			su := &undo.SpentUTXOs[i]
			if su.Script.Type == types.ScriptTypeStake && len(su.Script.Data) == 33 {
				c.unstakeHandler(su.Script.Data)
			}
		}
	}

	return nil
}

// validateBlockState checks UTXO-dependent rules: transaction signatures,
// coinbase maturity, token conservation, and stake amounts.
// Used by both the fast path and reorg replay to ensure consistent validation.
func (c *Chain) validateBlockState(blk *block.Block) error {
	coinbaseTx := blk.Transactions[0]

	// Coinbase must be a dedicated transaction:
	// exactly one input and that input must be the zero outpoint marker.
	if len(coinbaseTx.Inputs) != 1 || !coinbaseTx.Inputs[0].PrevOut.IsZero() {
		return ErrBadCoinbaseTx
	}

	// Reject coinbase with token outputs — tokens must go through normal
	// transactions so that mint fee and conservation rules are enforced.
	for i, out := range coinbaseTx.Outputs {
		if out.Token != nil {
			return fmt.Errorf("coinbase output %d: must not contain token data", i)
		}
		if out.Script.Type == types.ScriptTypeMint {
			return fmt.Errorf("coinbase output %d: must not use mint script type", i)
		}
	}

	// Full UTXO-aware transaction validation (skip coinbase):
	// ownership checks, input existence/unspent checks, signatures, and fee sanity.
	utxoProvider := &chainUTXOProvider{set: c.utxos}
	fees := make([]uint64, len(blk.Transactions))
	var totalFees uint64
	for i, transaction := range blk.Transactions {
		if i == 0 {
			continue // Coinbase.
		}
		fee, err := transaction.ValidateWithUTXOs(utxoProvider)
		if err != nil {
			return fmt.Errorf("tx %d validation: %w", i, err)
		}
		if totalFees > math.MaxUint64-fee {
			return fmt.Errorf("tx %d fee overflow", i)
		}
		fees[i] = fee
		totalFees += fee
	}

	// Enforce coinbase mint limit:
	// minted = coinbase_total - total_fees (fees are recycled, not newly minted).
	coinbaseTotal, err := coinbaseTx.TotalOutputValue()
	if err != nil {
		return fmt.Errorf("coinbase output overflow: %w", err)
	}
	var minted uint64
	if coinbaseTotal > totalFees {
		minted = coinbaseTotal - totalFees
	}
	allowedMint := c.blockReward
	if c.maxSupply > 0 {
		if c.state.Supply >= c.maxSupply {
			allowedMint = 0
		} else if remaining := c.maxSupply - c.state.Supply; allowedMint > remaining {
			allowedMint = remaining
		}
	}
	if minted > allowedMint {
		return fmt.Errorf("%w: minted=%d allowed=%d", ErrCoinbaseRewardExceeded, minted, allowedMint)
	}

	// Defensive rule: only transaction 0 may carry a coinbase marker input.
	for i, transaction := range blk.Transactions[1:] {
		for _, in := range transaction.Inputs {
			if in.PrevOut.IsZero() {
				return fmt.Errorf("%w: tx %d contains coinbase input", ErrBadCoinbaseTx, i+1)
			}
		}
	}

	// Coinbase maturity: reject blocks that spend immature coinbase outputs.
	if err := c.checkCoinbaseMaturity(blk); err != nil {
		return err
	}

	// Token validation: verify token conservation, minting, and burning rules.
	tokenInputs := &token.UTXOTokenAdapter{Set: c.utxos}
	for i, transaction := range blk.Transactions[1:] {
		if err := token.ValidateTokens(transaction, tokenInputs); err != nil {
			return fmt.Errorf("token validation: %w", err)
		}
		if config.TokenCreationFee > 0 && token.HasMintOutput(transaction) {
			txFee := fees[i+1]
			if err := token.ValidateMintFee(transaction, txFee, config.TokenCreationFee); err != nil {
				return fmt.Errorf("token creation fee: %w", err)
			}
		}
	}

	// Enforce exact stake amount at chain level.
	if c.validatorStake > 0 {
		for _, transaction := range blk.Transactions[1:] {
			for _, out := range transaction.Outputs {
				if out.Script.Type == types.ScriptTypeStake && out.Value != c.validatorStake {
					return fmt.Errorf("%w: must be exactly %d, got %d", ErrInvalidStakeAmount, c.validatorStake, out.Value)
				}
			}
		}
	}

	return nil
}

// checkParentLink verifies that the block's PrevHash and Height are consistent
// with the current chain tip.
func (c *Chain) checkParentLink(blk *block.Block) error {
	// Genesis block: PrevHash must be zero, height must be 0.
	if c.state.IsGenesis() {
		if blk.Header.Height != 0 {
			return fmt.Errorf("%w: genesis must be height 0, got %d", ErrBadHeight, blk.Header.Height)
		}
		if !blk.Header.PrevHash.IsZero() {
			return fmt.Errorf("%w: genesis must have zero prev_hash", ErrBadPrevHash)
		}
		return nil
	}

	// Non-genesis: check if block extends current tip.
	if blk.Header.PrevHash == c.state.TipHash {
		expectedHeight := c.state.Height + 1
		if blk.Header.Height != expectedHeight {
			return fmt.Errorf("%w: want %d, got %d", ErrBadHeight, expectedHeight, blk.Header.Height)
		}
		return nil
	}

	// PrevHash != tip. Check if the parent exists (fork) or is truly unknown.
	parentKnown, err := c.blocks.HasBlock(blk.Header.PrevHash)
	if err != nil {
		return fmt.Errorf("check parent: %w", err)
	}
	if parentKnown {
		parentBlk, err := c.blocks.GetBlock(blk.Header.PrevHash)
		if err != nil {
			return fmt.Errorf("load parent block: %w", err)
		}
		expectedHeight := parentBlk.Header.Height + 1
		if blk.Header.Height != expectedHeight {
			return fmt.Errorf("%w: parent height %d implies %d, got %d",
				ErrBadHeight, parentBlk.Header.Height, expectedHeight, blk.Header.Height)
		}
		return fmt.Errorf("%w: block %d forks from %s", ErrForkDetected, blk.Header.Height, blk.Header.PrevHash)
	}
	return ErrPrevNotFound
}

// computeBlockReward calculates the new coins minted in this block.
// Block reward = coinbase output value - total fees from non-coinbase txs.
// Must be called BEFORE applyBlock (needs UTXO set for input values).
func (c *Chain) computeBlockReward(blk *block.Block) uint64 {
	if len(blk.Transactions) == 0 || len(blk.Transactions[0].Outputs) == 0 {
		return 0
	}

	coinbaseValue, err := blk.Transactions[0].TotalOutputValue()
	if err != nil {
		return 0
	}

	// Sum fees from non-coinbase transactions.
	var totalFees uint64
	for _, transaction := range blk.Transactions[1:] {
		var inputSum, outputSum uint64
		for _, in := range transaction.Inputs {
			if in.PrevOut.IsZero() {
				continue
			}
			u, err := c.utxos.Get(in.PrevOut)
			if err != nil {
				continue // Input not found (shouldn't happen after validation).
			}
			if inputSum > math.MaxUint64-u.Value {
				continue // Overflow guard.
			}
			inputSum += u.Value
		}
		for _, out := range transaction.Outputs {
			if outputSum > math.MaxUint64-out.Value {
				continue // Overflow guard.
			}
			outputSum += out.Value
		}
		if inputSum > outputSum {
			fee := inputSum - outputSum
			if totalFees > math.MaxUint64-fee {
				continue // Overflow guard.
			}
			totalFees += fee
		}
	}

	// Reward = coinbase value minus recycled fees.
	if coinbaseValue > totalFees {
		return coinbaseValue - totalFees
	}
	return 0
}

// computeTxFee calculates the fee for a single transaction.
// fee = sum(input values) - sum(output values).
// Must be called BEFORE applyBlock (needs UTXO set for input values).
func (c *Chain) computeTxFee(transaction *tx.Transaction) uint64 {
	var inputSum, outputSum uint64
	for _, in := range transaction.Inputs {
		if in.PrevOut.IsZero() {
			continue
		}
		u, err := c.utxos.Get(in.PrevOut)
		if err != nil {
			continue
		}
		if inputSum > math.MaxUint64-u.Value {
			continue // Overflow guard.
		}
		inputSum += u.Value
	}
	for _, out := range transaction.Outputs {
		if outputSum > math.MaxUint64-out.Value {
			continue // Overflow guard.
		}
		outputSum += out.Value
	}
	if inputSum > outputSum {
		return inputSum - outputSum
	}
	return 0
}

type chainUTXOProvider struct {
	set utxo.Set
}

func (p *chainUTXOProvider) GetUTXO(outpoint types.Outpoint) (uint64, types.Script, error) {
	u, err := p.set.Get(outpoint)
	if err != nil {
		return 0, types.Script{}, err
	}
	return u.Value, u.Script, nil
}

func (p *chainUTXOProvider) HasUTXO(outpoint types.Outpoint) bool {
	has, err := p.set.Has(outpoint)
	return err == nil && has
}

// applyBlock updates the UTXO set: spends inputs and creates outputs.
// Coinbase inputs (zero outpoint) are skipped during spending.
func (c *Chain) applyBlock(blk *block.Block) error {
	for txIdx, transaction := range blk.Transactions {
		txHash := transaction.Hash()
		isCoinbase := txIdx == 0 && blk.Header.Height > 0

		// Spend inputs (skip coinbase zero-outpoint).
		for _, in := range transaction.Inputs {
			if in.PrevOut.IsZero() {
				continue // Coinbase input.
			}
			if err := c.utxos.Delete(in.PrevOut); err != nil {
				return fmt.Errorf("spend %s: %w", in.PrevOut, err)
			}
		}

		// Create outputs.
		for i, out := range transaction.Outputs {
			u := &utxo.UTXO{
				Outpoint: types.Outpoint{TxID: txHash, Index: uint32(i)},
				Value:    out.Value,
				Script:   out.Script,
				Token:    out.Token,
				Height:   blk.Header.Height,
				Coinbase: isCoinbase,
			}
			if err := c.utxos.Put(u); err != nil {
				return fmt.Errorf("create output %s:%d: %w", txHash, i, err)
			}
		}
	}
	return nil
}

// checkCoinbaseMaturity verifies that no transaction in the block spends
// an immature coinbase output.
func (c *Chain) checkCoinbaseMaturity(blk *block.Block) error {
	for _, transaction := range blk.Transactions {
		for _, in := range transaction.Inputs {
			if in.PrevOut.IsZero() {
				continue
			}
			u, err := c.utxos.Get(in.PrevOut)
			if err != nil {
				continue // Will be caught by UTXO validation.
			}
			if u.Coinbase && blk.Header.Height-u.Height < config.CoinbaseMaturity {
				return fmt.Errorf("%w: need %d confirmations, have %d",
					ErrCoinbaseNotMature, config.CoinbaseMaturity, blk.Header.Height-u.Height)
			}
			if u.LockedUntil > 0 && blk.Header.Height < u.LockedUntil {
				return fmt.Errorf("output locked until block %d, current %d", u.LockedUntil, blk.Header.Height)
			}
		}
	}
	return nil
}

// checkSigningLimit enforces the PoA signing frequency rule: a validator
// may sign at most 1 block in any consecutive window of N/2+1 blocks,
// where N is the number of active validators. Returns nil for non-PoA chains
// or single-validator setups.
func (c *Chain) checkSigningLimit(blk *block.Block) error {
	poa, ok := c.engine.(*consensus.PoA)
	if !ok {
		return nil
	}
	limit := poa.SigningLimit()
	if limit == 0 {
		return nil
	}
	signer := poa.IdentifySigner(blk.Header)
	if signer == nil {
		return nil
	}

	// Check the last (limit - 1) blocks for the same signer.
	h := blk.Header.Height
	for i := 1; i < limit; i++ {
		if h < uint64(i) {
			break
		}
		prev, err := c.blocks.GetBlockByHeight(h - uint64(i))
		if err != nil {
			continue
		}
		prevSigner := poa.IdentifySigner(prev.Header)
		if prevSigner != nil && bytes.Equal(signer, prevSigner) {
			return fmt.Errorf("%w: signer appeared at height %d and %d (window=%d)",
				ErrSigningLimitExceeded, h-uint64(i), h, limit)
		}
	}
	return nil
}

// IsSigningLimitReached checks whether the given validator pubkey has signed
// a block recently enough that producing another block would violate the
// signing limit. Used by the miner to skip slots proactively.
func (c *Chain) IsSigningLimitReached(pubkey []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	poa, ok := c.engine.(*consensus.PoA)
	if !ok {
		return false
	}
	limit := poa.SigningLimit()
	if limit == 0 {
		return false
	}

	h := c.state.Height
	for i := 0; i < limit-1; i++ {
		if h < uint64(i+1) {
			break
		}
		prev, err := c.blocks.GetBlockByHeight(h - uint64(i))
		if err != nil {
			continue
		}
		prevSigner := poa.IdentifySigner(prev.Header)
		if prevSigner != nil && bytes.Equal(pubkey, prevSigner) {
			return true
		}
	}
	return false
}

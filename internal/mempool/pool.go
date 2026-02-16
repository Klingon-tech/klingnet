// Package mempool manages pending transactions waiting for block inclusion.
package mempool

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/Klingon-tech/klingnet-chain/internal/token"
	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// Mempool errors.
var (
	ErrAlreadyExists     = errors.New("transaction already in mempool")
	ErrConflict          = errors.New("transaction conflicts with existing mempool entry")
	ErrPoolFull          = errors.New("mempool is full")
	ErrValidation        = errors.New("transaction failed validation")
	ErrFeeTooLow         = errors.New("transaction fee below minimum")
	ErrCoinbaseNotMature = errors.New("coinbase output not mature")
)

// entry wraps a transaction with its fee and metadata.
type entry struct {
	tx      *tx.Transaction
	txHash  types.Hash
	fee     uint64
	feeRate float64 // fee per byte of SigningBytes.
}

// Pool holds unconfirmed transactions.
type Pool struct {
	mu      sync.RWMutex
	txs     map[types.Hash]*entry         // txHash -> entry
	spends  map[types.Outpoint]types.Hash // outpoint -> txHash (conflict index)
	maxSize int
	minFeeRate uint64 // Minimum fee rate in base units per byte (0 = no minimum).
	utxos   tx.UTXOProvider

	// Coinbase maturity checking.
	utxoSet          utxo.Set      // For maturity checks (nil = disabled).
	heightFn         func() uint64 // Current chain height.
	coinbaseMaturity uint64        // Required confirmations (0 = disabled).

	// Token validation.
	tokenInputs token.InputTokens // For token conservation checks (nil = disabled).
	mintFee     uint64            // Minimum fee for mint transactions (0 = no extra requirement).

	// Stake validation.
	stakeAmount uint64 // Exact amount required for stake outputs (0 = disabled).
}

// New creates a new mempool with the given UTXO provider and max size.
func New(utxos tx.UTXOProvider, maxSize int) *Pool {
	if maxSize <= 0 {
		maxSize = 5000
	}
	return &Pool{
		txs:     make(map[types.Hash]*entry),
		spends:  make(map[types.Outpoint]types.Hash),
		maxSize: maxSize,
		utxos:   utxos,
	}
}

// SetMinFeeRate sets the minimum fee rate (base units per byte) for transaction acceptance.
func (p *Pool) SetMinFeeRate(rate uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.minFeeRate = rate
}

// MinFeeRate returns the current minimum fee rate (base units per byte).
func (p *Pool) MinFeeRate() uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.minFeeRate
}

// SetTokenValidator enables token validation in the mempool.
func (p *Pool) SetTokenValidator(inputs token.InputTokens) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tokenInputs = inputs
}

// SetMintFee sets the minimum fee required for mint transactions.
func (p *Pool) SetMintFee(fee uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mintFee = fee
}

// SetStakeAmount sets the exact amount required for stake outputs.
// Transactions with ScriptTypeStake outputs whose value != stakeAmount are rejected.
func (p *Pool) SetStakeAmount(amount uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stakeAmount = amount
}

// SetCoinbaseMaturity enables coinbase maturity checking.
func (p *Pool) SetCoinbaseMaturity(maturity uint64, heightFn func() uint64, set utxo.Set) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.coinbaseMaturity = maturity
	p.heightFn = heightFn
	p.utxoSet = set
}

// Add validates and adds a transaction to the mempool.
// Returns the computed fee. Rejects duplicates and double-spend conflicts.
func (p *Pool) Add(transaction *tx.Transaction) (uint64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	txHash := transaction.Hash()

	// Reject duplicates.
	if _, exists := p.txs[txHash]; exists {
		return 0, ErrAlreadyExists
	}

	// Check for double-spend conflicts.
	for _, in := range transaction.Inputs {
		if in.PrevOut.IsZero() {
			continue
		}
		if conflictHash, exists := p.spends[in.PrevOut]; exists {
			return 0, fmt.Errorf("%w: input %s already spent by %s", ErrConflict, in.PrevOut, conflictHash)
		}
	}

	// Coinbase maturity check.
	if p.coinbaseMaturity > 0 && p.utxoSet != nil {
		currentHeight := p.heightFn()
		for _, in := range transaction.Inputs {
			if in.PrevOut.IsZero() {
				continue
			}
			u, uErr := p.utxoSet.Get(in.PrevOut)
			if uErr == nil && u.Coinbase && currentHeight-u.Height < p.coinbaseMaturity {
				return 0, fmt.Errorf("%w: need %d confirmations, have %d",
					ErrCoinbaseNotMature, p.coinbaseMaturity, currentHeight-u.Height)
			}
			if uErr == nil && u.LockedUntil > 0 && currentHeight < u.LockedUntil {
				return 0, fmt.Errorf("output locked until block %d, current %d", u.LockedUntil, currentHeight)
			}
		}
	}

	// UTXO-aware validation.
	fee, err := transaction.ValidateWithUTXOs(p.utxos)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrValidation, err)
	}

	// Token validation.
	if p.tokenInputs != nil {
		if err := token.ValidateTokens(transaction, p.tokenInputs); err != nil {
			return 0, fmt.Errorf("%w: %v", ErrValidation, err)
		}
	}

	// Mint fee: require higher fee for transactions that create tokens.
	if p.mintFee > 0 && fee < p.mintFee {
		if token.HasMintOutput(transaction) {
			return 0, fmt.Errorf("%w: mint tx needs %d, got %d", ErrFeeTooLow, p.mintFee, fee)
		}
	}

	// Stake amount: enforce exact value on ScriptTypeStake outputs.
	if p.stakeAmount > 0 {
		for _, out := range transaction.Outputs {
			if out.Script.Type == types.ScriptTypeStake && out.Value != p.stakeAmount {
				return 0, fmt.Errorf("%w: stake output must be exactly %d, got %d", ErrValidation, p.stakeAmount, out.Value)
			}
		}
	}

	// Compute fee rate for minimum check and eviction comparison.
	sigBytes := len(transaction.SigningBytes())
	var feeRate float64
	if sigBytes > 0 {
		feeRate = float64(fee) / float64(sigBytes)
	}

	// Enforce minimum fee rate (fee per byte of SigningBytes).
	if p.minFeeRate > 0 {
		requiredFee := p.minFeeRate * uint64(sigBytes)
		if fee < requiredFee {
			return 0, fmt.Errorf("%w: got %d, need %d (%d bytes × %d rate)", ErrFeeTooLow, fee, requiredFee, sigBytes, p.minFeeRate)
		}
	}

	// Check pool capacity — evict lowest fee-rate if new tx pays more.
	if len(p.txs) >= p.maxSize {
		lowestHash, lowestRate := p.findLowestFeeRate()
		if feeRate <= lowestRate {
			return 0, ErrPoolFull
		}
		p.removeLocked(lowestHash)
	}

	e := &entry{
		tx:      transaction,
		txHash:  txHash,
		fee:     fee,
		feeRate: feeRate,
	}

	// Add to pool and conflict index.
	p.txs[txHash] = e
	for _, in := range transaction.Inputs {
		if !in.PrevOut.IsZero() {
			p.spends[in.PrevOut] = txHash
		}
	}

	return fee, nil
}

// Remove removes a transaction from the mempool by hash.
func (p *Pool) Remove(txHash types.Hash) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.removeLocked(txHash)
}

func (p *Pool) removeLocked(txHash types.Hash) {
	e, exists := p.txs[txHash]
	if !exists {
		return
	}
	// Clean up spend index.
	for _, in := range e.tx.Inputs {
		if !in.PrevOut.IsZero() {
			delete(p.spends, in.PrevOut)
		}
	}
	delete(p.txs, txHash)
}

// RemoveConfirmed removes all transactions that were included in a block.
func (p *Pool) RemoveConfirmed(transactions []*tx.Transaction) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range transactions {
		p.removeLocked(t.Hash())
	}
}

// Has checks if a transaction exists in the mempool.
func (p *Pool) Has(txHash types.Hash) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, exists := p.txs[txHash]
	return exists
}

// Get retrieves a transaction from the mempool.
func (p *Pool) Get(txHash types.Hash) *tx.Transaction {
	p.mu.RLock()
	defer p.mu.RUnlock()
	e, exists := p.txs[txHash]
	if !exists {
		return nil
	}
	return e.tx
}

// GetFee returns the fee for a transaction in the mempool (0 if not found).
func (p *Pool) GetFee(txHash types.Hash) uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	e, exists := p.txs[txHash]
	if !exists {
		return 0
	}
	return e.fee
}

// Count returns the number of transactions in the mempool.
func (p *Pool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.txs)
}

// Hashes returns the hashes of all transactions in the mempool.
func (p *Pool) Hashes() []types.Hash {
	p.mu.RLock()
	defer p.mu.RUnlock()
	hashes := make([]types.Hash, 0, len(p.txs))
	for h := range p.txs {
		hashes = append(hashes, h)
	}
	return hashes
}

// findLowestFeeRate returns the hash and fee rate of the lowest fee-rate entry.
// Must be called with p.mu held.
func (p *Pool) findLowestFeeRate() (types.Hash, float64) {
	var lowestHash types.Hash
	lowestRate := math.MaxFloat64
	for h, e := range p.txs {
		if e.feeRate < lowestRate {
			lowestRate = e.feeRate
			lowestHash = h
		}
	}
	return lowestHash, lowestRate
}

// SelectForBlock returns transactions ordered by fee rate (highest first),
// up to the given limit.
func (p *Pool) SelectForBlock(limit int) []*tx.Transaction {
	p.mu.RLock()
	defer p.mu.RUnlock()

	entries := make([]*entry, 0, len(p.txs))
	for _, e := range p.txs {
		entries = append(entries, e)
	}

	// Sort by fee rate descending.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].feeRate > entries[j].feeRate
	})

	if limit > len(entries) {
		limit = len(entries)
	}

	result := make([]*tx.Transaction, limit)
	for i := 0; i < limit; i++ {
		result[i] = entries[i].tx
	}
	return result
}

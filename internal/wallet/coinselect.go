package wallet

import (
	"errors"
	"fmt"
	"sort"

	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// Coin selection errors.
var (
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrNoUTXOs           = errors.New("no UTXOs available")
)

// UTXO represents an unspent output owned by the wallet.
type UTXO struct {
	Outpoint types.Outpoint
	Value    uint64
	Script   types.Script
	Token    *types.TokenData // Non-nil for token-carrying UTXOs.
}

// CoinSelection holds the result of coin selection.
type CoinSelection struct {
	Inputs []UTXO // Selected UTXOs to spend.
	Total  uint64 // Sum of selected input values.
	Change uint64 // Change = Total - target.
}

// SelectCoins chooses UTXOs to fund a transaction of the given target amount.
// It tries two strategies:
//  1. Single UTXO: finds the smallest single UTXO that covers the target (minimizes inputs).
//  2. Largest-first accumulation: greedily adds the largest UTXOs until the target is met.
//
// Returns the strategy that produces the least change (waste).
func SelectCoins(utxos []UTXO, target uint64) (*CoinSelection, error) {
	if len(utxos) == 0 {
		return nil, ErrNoUTXOs
	}
	if target == 0 {
		return nil, fmt.Errorf("target must be positive")
	}

	// Filter out zero-value UTXOs and sort by value ascending.
	candidates := make([]UTXO, 0, len(utxos))
	for _, u := range utxos {
		if u.Value > 0 {
			candidates = append(candidates, u)
		}
	}
	if len(candidates) == 0 {
		return nil, ErrNoUTXOs
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Value < candidates[j].Value
	})

	// Strategy 1: Single UTXO — smallest one that covers the target.
	var single *CoinSelection
	for _, u := range candidates {
		if u.Value >= target {
			single = &CoinSelection{
				Inputs: []UTXO{u},
				Total:  u.Value,
				Change: u.Value - target,
			}
			break // Already sorted ascending, first match is smallest.
		}
	}

	// Strategy 2: Largest-first accumulation.
	var accum *CoinSelection
	var selected []UTXO
	var total uint64
	// Iterate from largest to smallest.
	for i := len(candidates) - 1; i >= 0; i-- {
		selected = append(selected, candidates[i])
		total += candidates[i].Value
		if total >= target {
			accum = &CoinSelection{
				Inputs: selected,
				Total:  total,
				Change: total - target,
			}
			break
		}
	}

	// Pick the best result.
	switch {
	case single != nil && accum != nil:
		// Prefer whichever produces less change (less waste).
		if single.Change <= accum.Change {
			return single, nil
		}
		return accum, nil
	case single != nil:
		return single, nil
	case accum != nil:
		return accum, nil
	default:
		return nil, fmt.Errorf("%w: have %d, need %d", ErrInsufficientFunds, totalValue(candidates), target)
	}
}

func totalValue(utxos []UTXO) uint64 {
	var total uint64
	for _, u := range utxos {
		total += u.Value
	}
	return total
}

package consensus

import (
	"math"

	"github.com/Klingon-tech/klingnet-chain/internal/utxo"
)

// UTXOStakeChecker checks that a validator has sufficient stake by querying the
// UTXO store's stake index. It satisfies the StakeChecker interface.
type UTXOStakeChecker struct {
	utxos    *utxo.Store
	minStake uint64
}

// NewUTXOStakeChecker creates a stake checker that requires at least minStake
// base units locked in ScriptTypeStake UTXOs for the given public key.
func NewUTXOStakeChecker(utxos *utxo.Store, minStake uint64) *UTXOStakeChecker {
	return &UTXOStakeChecker{utxos: utxos, minStake: minStake}
}

// HasStake returns true if the validator identified by pubKey has >= minStake
// locked in ScriptTypeStake UTXOs.
func (c *UTXOStakeChecker) HasStake(pubKey []byte) (bool, error) {
	stakes, err := c.utxos.GetStakes(pubKey)
	if err != nil {
		return false, err
	}

	var total uint64
	for _, s := range stakes {
		if total > math.MaxUint64-s.Value {
			// Overflow means total exceeds any possible minStake.
			return true, nil
		}
		total += s.Value
	}
	return total >= c.minStake, nil
}

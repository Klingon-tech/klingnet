package tx

import (
	"errors"
	"fmt"
	"math"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// Validation errors.
var (
	ErrNoInputs           = errors.New("transaction has no inputs")
	ErrNoOutputs          = errors.New("transaction has no outputs")
	ErrDuplicateInput     = errors.New("duplicate input")
	ErrOutputOverflow     = errors.New("output values overflow")
	ErrNegativeOutput     = errors.New("output value is zero")
	ErrInvalidScript      = errors.New("invalid script type")
	ErrMissingPubKey      = errors.New("input missing public key")
	ErrMissingSig         = errors.New("input missing signature")
	ErrInvalidSig         = errors.New("invalid signature")
	ErrTooManyInputs      = errors.New("too many inputs")
	ErrTooManyOutputs     = errors.New("too many outputs")
	ErrScriptDataTooLarge = errors.New("script data too large")
)

// Validate checks transaction structure and basic rules.
// This does NOT check UTXO existence (that requires the UTXO set).
func (tx *Transaction) Validate() error {
	if len(tx.Inputs) == 0 {
		return ErrNoInputs
	}
	if len(tx.Outputs) == 0 {
		return ErrNoOutputs
	}
	if len(tx.Inputs) > config.MaxTxInputs {
		return fmt.Errorf("%w: %d inputs, max %d", ErrTooManyInputs, len(tx.Inputs), config.MaxTxInputs)
	}
	if len(tx.Outputs) > config.MaxTxOutputs {
		return fmt.Errorf("%w: %d outputs, max %d", ErrTooManyOutputs, len(tx.Outputs), config.MaxTxOutputs)
	}

	// Check for duplicate inputs.
	seen := make(map[types.Outpoint]bool, len(tx.Inputs))
	for i, in := range tx.Inputs {
		if seen[in.PrevOut] {
			return fmt.Errorf("input %d: %w", i, ErrDuplicateInput)
		}
		seen[in.PrevOut] = true
	}

	// Validate inputs have signatures and public keys.
	// Coinbase inputs (zero outpoint) are exempt — they create coins.
	for i, in := range tx.Inputs {
		if in.PrevOut.IsZero() {
			continue // Coinbase input.
		}
		if len(in.PubKey) == 0 {
			return fmt.Errorf("input %d: %w", i, ErrMissingPubKey)
		}
		if len(in.Signature) == 0 {
			return fmt.Errorf("input %d: %w", i, ErrMissingSig)
		}
	}

	// Validate outputs.
	var totalOutput uint64
	for i, out := range tx.Outputs {
		if out.Value == 0 && out.Token == nil {
			return fmt.Errorf("output %d: %w", i, ErrNegativeOutput)
		}
		if len(out.Script.Data) > config.MaxScriptData {
			return fmt.Errorf("output %d: %w: %d bytes, max %d", i, ErrScriptDataTooLarge, len(out.Script.Data), config.MaxScriptData)
		}
		if totalOutput > math.MaxUint64-out.Value {
			return fmt.Errorf("output %d: %w", i, ErrOutputOverflow)
		}
		totalOutput += out.Value
	}

	return nil
}

// VerifySignatures checks that all input signatures are valid for this transaction.
func (tx *Transaction) VerifySignatures() error {
	hash := tx.Hash()
	for i, in := range tx.Inputs {
		if in.PrevOut.IsZero() {
			continue // Coinbase input.
		}
		if !crypto.VerifySignature(hash[:], in.Signature, in.PubKey) {
			return fmt.Errorf("input %d: %w", i, ErrInvalidSig)
		}
	}
	return nil
}

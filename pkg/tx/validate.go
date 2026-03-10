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
	ErrMintMissingToken   = errors.New("mint output missing token data")
	ErrMintNonZeroValue   = errors.New("mint output must have zero native value")
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
		if err := validateOutputScript(out); err != nil {
			return fmt.Errorf("output %d: %w", i, err)
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

func validateOutputScript(out Output) error {
	switch out.Script.Type {
	case types.ScriptTypeP2PKH, types.ScriptTypeBurn, types.ScriptTypeAnchor, types.ScriptTypeRegister:
		return nil
	case types.ScriptTypeMint:
		if out.Token == nil {
			return ErrMintMissingToken
		}
		if out.Value != 0 {
			return ErrMintNonZeroValue
		}
		if len(out.Script.Data) < types.AddressSize {
			return fmt.Errorf("%w: mint script data must include at least a %d-byte address", ErrInvalidScript, types.AddressSize)
		}
		return nil
	case types.ScriptTypeStake:
		if len(out.Script.Data) != 33 {
			return fmt.Errorf("%w: stake script data length %d, want 33", ErrInvalidScript, len(out.Script.Data))
		}
		return nil
	case types.ScriptTypeP2SH, types.ScriptTypeBridge:
		return fmt.Errorf("%w: unsupported script type %s", ErrInvalidScript, out.Script.Type)
	default:
		return fmt.Errorf("%w: unknown script type %#x", ErrInvalidScript, uint8(out.Script.Type))
	}
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

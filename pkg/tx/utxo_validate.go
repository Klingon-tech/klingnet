package tx

import (
	"bytes"
	"errors"
	"fmt"
	"math"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// UTXO-aware validation errors.
var (
	ErrInputNotFound     = errors.New("input UTXO not found")
	ErrInputSpent        = errors.New("input UTXO already spent")
	ErrInsufficientFee   = errors.New("insufficient fee")
	ErrInputOverflow     = errors.New("input values overflow")
	ErrScriptMismatch    = errors.New("pubkey does not match UTXO script")
	ErrUnspendableOutput = errors.New("output is unspendable")
)

// UTXOProvider provides read-only access to the UTXO set for validation.
type UTXOProvider interface {
	GetUTXO(outpoint types.Outpoint) (value uint64, script types.Script, err error)
	HasUTXO(outpoint types.Outpoint) bool
}

// ValidateWithUTXOs performs full validation of a transaction against the UTXO set.
// It checks that all inputs exist, are unspent, that the pubkey matches the
// UTXO script, that signatures are valid, and that inputs >= outputs.
// Returns the fee (inputs - outputs).
func (tx *Transaction) ValidateWithUTXOs(provider UTXOProvider) (uint64, error) {
	// Basic structural validation first.
	if err := tx.ValidateStructure(); err != nil {
		return 0, err
	}

	// Check each input against the UTXO set.
	var totalInput uint64
	for i, in := range tx.Inputs {
		// Coinbase inputs skip UTXO checks.
		if in.PrevOut.IsZero() {
			continue
		}

		if !provider.HasUTXO(in.PrevOut) {
			return 0, fmt.Errorf("input %d (%s): %w", i, in.PrevOut, ErrInputNotFound)
		}

		value, script, err := provider.GetUTXO(in.PrevOut)
		if err != nil {
			return 0, fmt.Errorf("input %d: %w", i, err)
		}

		// Reject spending unspendable outputs (register, anchor, burn).
		if script.Type == types.ScriptTypeRegister || script.Type == types.ScriptTypeAnchor || script.Type == types.ScriptTypeBurn {
			return 0, fmt.Errorf("input %d (%s): %w: %s output cannot be spent",
				i, in.PrevOut, ErrUnspendableOutput, script.Type)
		}

		// Verify the pubkey matches the UTXO script for P2PKH.
		if script.Type == types.ScriptTypeP2PKH {
			if err := verifyP2PKH(in.PubKey, script.Data); err != nil {
				return 0, fmt.Errorf("input %d: %w", i, err)
			}
		}

		// Verify the pubkey matches the stake's pubkey for ScriptTypeStake.
		if script.Type == types.ScriptTypeStake {
			if len(script.Data) != 33 {
				return 0, fmt.Errorf("input %d: %w: stake script data length %d, want 33", i, ErrScriptMismatch, len(script.Data))
			}
			if !bytes.Equal(in.PubKey, script.Data) {
				return 0, fmt.Errorf("input %d: %w: pubkey does not match stake", i, ErrScriptMismatch)
			}
		}

		if totalInput > math.MaxUint64-value {
			return 0, fmt.Errorf("input %d: %w", i, ErrInputOverflow)
		}
		totalInput += value
	}

	// Verify signatures.
	if err := tx.VerifySignatures(); err != nil {
		return 0, err
	}

	totalOutput, ovfErr := tx.TotalOutputValue()
	if ovfErr != nil {
		return 0, fmt.Errorf("output overflow: %w", ovfErr)
	}
	if totalInput < totalOutput {
		return 0, fmt.Errorf("%w: inputs=%d outputs=%d", ErrInsufficientFee, totalInput, totalOutput)
	}

	fee := totalInput - totalOutput
	return fee, nil
}

// ValidateStructure checks transaction structure without requiring UTXO access.
// Same as Validate() but renamed for clarity when used alongside ValidateWithUTXOs.
func (tx *Transaction) ValidateStructure() error {
	return tx.Validate()
}

// verifyP2PKH checks that a public key hashes to the expected address in the script.
func verifyP2PKH(pubKey []byte, scriptData []byte) error {
	if len(scriptData) != types.AddressSize {
		return fmt.Errorf("%w: script data length %d", ErrScriptMismatch, len(scriptData))
	}
	if len(pubKey) == 0 {
		return ErrMissingPubKey
	}

	// Address = BLAKE3(compressed_pubkey)[:20].
	hash := crypto.Hash(pubKey)
	var expected types.Address
	copy(expected[:], scriptData)
	var derived types.Address
	copy(derived[:], hash[:types.AddressSize])

	if expected != derived {
		return fmt.Errorf("%w: expected %s, got %s", ErrScriptMismatch, expected, derived)
	}
	return nil
}

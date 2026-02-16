package token

import (
	"errors"
	"fmt"
	"math"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// Token validation errors.
var (
	ErrTokenConservation   = errors.New("token conservation violated")
	ErrMintNoScript        = errors.New("mint output missing mint script")
	ErrMintZeroAmount      = errors.New("mint amount must be positive")
	ErrBurnZeroAmount      = errors.New("burn amount must be positive")
	ErrTokenIDMismatch     = errors.New("output token ID does not match mint-derived ID")
	ErrMixedMintTransfer   = errors.New("cannot mint and transfer same token in one tx")
	ErrMintFeeTooLow       = errors.New("mint transaction fee too low")
	ErrTokenAmountTooLarge = errors.New("token amount exceeds maximum")
)

// InputTokens provides token data for transaction inputs from the UTXO set.
type InputTokens interface {
	// GetTokenData returns the token data for a given outpoint, or nil if none.
	GetTokenData(outpoint types.Outpoint) *types.TokenData
}

// ValidateTokens checks token rules for a transaction.
// It verifies:
//   - Mint outputs have correct TokenID derivation and mint scripts
//   - Burn outputs have burn scripts
//   - Conservation: for each non-minted, non-burned TokenID, input amount == output amount
func ValidateTokens(transaction *tx.Transaction, inputs InputTokens) error {
	// Derive the expected TokenID for any mints from the first input outpoint.
	var mintTokenID types.TokenID
	if len(transaction.Inputs) > 0 {
		first := transaction.Inputs[0].PrevOut
		mintTokenID = DeriveTokenID(first.TxID, first.Index)
	}

	// Gather token inputs (from UTXO set).
	inputTotals := make(map[types.TokenID]uint64)
	for _, in := range transaction.Inputs {
		if in.PrevOut.IsZero() {
			continue // Coinbase.
		}
		td := inputs.GetTokenData(in.PrevOut)
		if td != nil {
			current := inputTotals[td.ID]
			if current > math.MaxUint64-td.Amount {
				return fmt.Errorf("token %s: input amount overflow", td.ID)
			}
			inputTotals[td.ID] = current + td.Amount
		}
	}

	// Gather token outputs, separating mints, burns, and transfers.
	outputTotals := make(map[types.TokenID]uint64)
	mintedIDs := make(map[types.TokenID]bool)
	burnedAmounts := make(map[types.TokenID]uint64)

	for i, out := range transaction.Outputs {
		if out.Token == nil {
			continue
		}

		switch out.Script.Type {
		case types.ScriptTypeMint:
			if err := validateMintOutput(mintTokenID, out); err != nil {
				return fmt.Errorf("output %d: %w", i, err)
			}
			mintedIDs[out.Token.ID] = true
			current := outputTotals[out.Token.ID]
			if current > math.MaxUint64-out.Token.Amount {
				return fmt.Errorf("token %s: output amount overflow", out.Token.ID)
			}
			outputTotals[out.Token.ID] = current + out.Token.Amount

		case types.ScriptTypeBurn:
			if out.Token.Amount == 0 {
				return fmt.Errorf("output %d: %w", i, ErrBurnZeroAmount)
			}
			if out.Token.Amount > config.MaxTokenAmount {
				return fmt.Errorf("output %d: %w", i, ErrTokenAmountTooLarge)
			}
			current := burnedAmounts[out.Token.ID]
			if current > math.MaxUint64-out.Token.Amount {
				return fmt.Errorf("token %s: burn amount overflow", out.Token.ID)
			}
			burnedAmounts[out.Token.ID] = current + out.Token.Amount

		default:
			// Regular transfer output.
			if out.Token.Amount > config.MaxTokenAmount {
				return fmt.Errorf("output %d: %w", i, ErrTokenAmountTooLarge)
			}
			current := outputTotals[out.Token.ID]
			if current > math.MaxUint64-out.Token.Amount {
				return fmt.Errorf("token %s: output amount overflow", out.Token.ID)
			}
			outputTotals[out.Token.ID] = current + out.Token.Amount
		}
	}

	// Check: cannot mint and transfer the same token ID in one transaction.
	for id := range mintedIDs {
		if _, hasInput := inputTotals[id]; hasInput {
			return fmt.Errorf("token %s: %w", id, ErrMixedMintTransfer)
		}
	}

	// Conservation check for non-minted tokens.
	// For each TokenID that appears in inputs:
	// input_amount = output_amount + burned_amount
	allTokenIDs := make(map[types.TokenID]bool)
	for id := range inputTotals {
		allTokenIDs[id] = true
	}
	for id := range outputTotals {
		if !mintedIDs[id] {
			allTokenIDs[id] = true
		}
	}

	for id := range allTokenIDs {
		if mintedIDs[id] {
			continue // Minted tokens have no input requirement.
		}

		inAmount := inputTotals[id]
		outAmount := outputTotals[id] + burnedAmounts[id]

		if inAmount != outAmount {
			return fmt.Errorf("token %s: %w: input=%d output+burn=%d",
				id, ErrTokenConservation, inAmount, outAmount)
		}
	}

	return nil
}

// HasMintOutput returns true if the transaction contains any ScriptTypeMint outputs.
func HasMintOutput(transaction *tx.Transaction) bool {
	for _, out := range transaction.Outputs {
		if out.Script.Type == types.ScriptTypeMint {
			return true
		}
	}
	return false
}

// ValidateMintFee checks that a mint transaction pays the required creation fee.
// Returns nil if the transaction has no mint outputs or if the fee is sufficient.
func ValidateMintFee(transaction *tx.Transaction, fee, creationFee uint64) error {
	if !HasMintOutput(transaction) {
		return nil
	}
	if fee < creationFee {
		return fmt.Errorf("%w: need %d, got %d", ErrMintFeeTooLow, creationFee, fee)
	}
	return nil
}

// validateMintOutput checks that a mint output has a valid TokenID and amount.
func validateMintOutput(expectedID types.TokenID, out tx.Output) error {
	if out.Token.Amount == 0 {
		return ErrMintZeroAmount
	}
	if out.Token.Amount > config.MaxTokenAmount {
		return ErrTokenAmountTooLarge
	}

	// All mint outputs in a tx must use the same TokenID derived from the first input.
	if out.Token.ID != expectedID {
		return fmt.Errorf("%w: expected %s, got %s", ErrTokenIDMismatch, expectedID, out.Token.ID)
	}

	return nil
}

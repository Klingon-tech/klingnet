package consensus

import (
	"fmt"

	"github.com/Klingon-tech/klingnet-chain/pkg/block"
)

// Validator validates blocks against consensus rules.
type Validator struct {
	engine Engine
}

// NewValidator creates a block validator with the given consensus engine.
func NewValidator(engine Engine) *Validator {
	return &Validator{engine: engine}
}

// ValidateBlock checks a block against both structural and consensus rules.
func (v *Validator) ValidateBlock(blk *block.Block) error {
	// Structural validation.
	if err := blk.Validate(); err != nil {
		return fmt.Errorf("block structure: %w", err)
	}

	// Consensus-specific header verification.
	if err := v.engine.VerifyHeader(blk.Header); err != nil {
		return fmt.Errorf("consensus: %w", err)
	}

	return nil
}

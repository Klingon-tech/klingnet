package subchain

import (
	"fmt"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// ValidateRegistrationTx checks that a ScriptTypeRegister output is valid
// in the context of protocol rules and the current active-chain count.
// pendingRegistrations is the number of earlier registration outputs already
// validated in the same block.
func ValidateRegistrationTx(output tx.Output, rules *config.SubChainRules, existingRegistrations, pendingRegistrations uint64) error {
	if rules == nil {
		return fmt.Errorf("sub-chain rules are nil")
	}
	if output.Script.Type != types.ScriptTypeRegister {
		return fmt.Errorf("output is not a registration (type=%s)", output.Script.Type)
	}
	if !rules.Enabled {
		return fmt.Errorf("sub-chains are disabled")
	}

	// Check burn amount meets minimum deposit.
	if output.Value < rules.MinDeposit {
		return fmt.Errorf("registration burn %d < min deposit %d", output.Value, rules.MinDeposit)
	}

	// Parse and validate the registration data.
	rd, err := ParseRegistrationData(output.Script.Data)
	if err != nil {
		return fmt.Errorf("invalid registration data: %w", err)
	}

	if err := ValidateRegistrationData(rd, rules); err != nil {
		return fmt.Errorf("invalid registration params: %w", err)
	}

	// Check max sub-chains per parent.
	if rules.MaxPerParent > 0 {
		max := uint64(rules.MaxPerParent)
		if existingRegistrations > ^uint64(0)-pendingRegistrations {
			return fmt.Errorf("registration count overflow")
		}
		current := existingRegistrations + pendingRegistrations
		if current >= max {
			return fmt.Errorf("max sub-chains per parent reached (%d)", rules.MaxPerParent)
		}
	}

	return nil
}

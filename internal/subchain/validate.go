package subchain

import (
	"fmt"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// ValidateRegistrationTx checks that a ScriptTypeRegister output is valid
// in the context of protocol rules and the current registry state.
func ValidateRegistrationTx(output tx.Output, rules *config.SubChainRules, registry *Registry) error {
	if output.Script.Type != types.ScriptTypeRegister {
		return fmt.Errorf("output is not a registration (type=%s)", output.Script.Type)
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
	if registry != nil && rules.MaxPerParent > 0 && registry.Count() >= rules.MaxPerParent {
		return fmt.Errorf("max sub-chains per parent reached (%d)", rules.MaxPerParent)
	}

	return nil
}

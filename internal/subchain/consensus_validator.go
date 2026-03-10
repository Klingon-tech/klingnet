package subchain

import (
	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/chain"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
)

// NewRegistrationValidator adapts registration rule checks for chain consensus validation.
func NewRegistrationValidator(rules *config.SubChainRules) chain.RegistrationValidator {
	return func(output tx.Output, existingRegistrations, pendingRegistrations uint64) error {
		return ValidateRegistrationTx(output, rules, existingRegistrations, pendingRegistrations)
	}
}

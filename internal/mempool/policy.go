package mempool

import (
	"fmt"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/pkg/tx"
)

// DefaultMaxTxSize is the maximum transaction size in bytes (signing bytes).
const DefaultMaxTxSize = 100_000

// Policy defines transaction acceptance rules.
type Policy struct {
	MaxTxSize int // Maximum transaction size in signing bytes.
}

// DefaultPolicy returns a policy with sensible defaults.
func DefaultPolicy() *Policy {
	return &Policy{
		MaxTxSize: DefaultMaxTxSize,
	}
}

// Check validates a transaction against policy rules.
// This is separate from consensus validation — policy rules can vary per node.
// Also enforces consensus limits as defense-in-depth (reject early before full validation).
func (p *Policy) Check(transaction *tx.Transaction) error {
	size := len(transaction.SigningBytes())
	if p.MaxTxSize > 0 && size > p.MaxTxSize {
		return fmt.Errorf("transaction too large: %d bytes, max %d", size, p.MaxTxSize)
	}
	if len(transaction.Inputs) > config.MaxTxInputs {
		return fmt.Errorf("too many inputs: %d, max %d", len(transaction.Inputs), config.MaxTxInputs)
	}
	if len(transaction.Outputs) > config.MaxTxOutputs {
		return fmt.Errorf("too many outputs: %d, max %d", len(transaction.Outputs), config.MaxTxOutputs)
	}
	for i, out := range transaction.Outputs {
		if len(out.Script.Data) > config.MaxScriptData {
			return fmt.Errorf("output %d script data too large: %d bytes, max %d", i, len(out.Script.Data), config.MaxScriptData)
		}
	}
	return nil
}

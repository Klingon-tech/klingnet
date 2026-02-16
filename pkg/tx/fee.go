package tx

// EstimateTxFee returns the minimum fee for a transaction with the given
// number of inputs and outputs at the given fee rate (base units per byte).
//
// The estimate is based on the SigningBytes layout (which excludes signatures):
//
//	version(4) + inputCount(4) + inputs(36*n) + outputCount(4) + outputs(perOut*n) + locktime(8)
//
// By default, perOutput = 33 (8 value + 1 type + 4 len + 20 P2PKH addr).
// Pass an optional extraOutputBytes to add extra bytes per output (e.g., 13
// for stake outputs with 33-byte pubkeys, or 40 for token data per output).
func EstimateTxFee(numInputs, numOutputs int, feeRate uint64, extraOutputBytes ...int) uint64 {
	const overhead = 4 + 4 + 4 + 8   // version + inputCount + outputCount + locktime
	const perInput = 32 + 4           // txID + index
	const perOutput = 8 + 1 + 4 + 20 // value + scriptType + scriptDataLen + P2PKH addr

	extra := 0
	if len(extraOutputBytes) > 0 {
		extra = extraOutputBytes[0]
	}

	size := overhead + perInput*numInputs + (perOutput+extra)*numOutputs
	return uint64(size) * feeRate
}

// RequiredFee returns the exact minimum fee for a fully built transaction
// at the given fee rate (base units per byte of SigningBytes). This is more
// accurate than EstimateTxFee for transactions with non-standard outputs
// (stake, registration, token).
func RequiredFee(transaction *Transaction, feeRate uint64) uint64 {
	return uint64(len(transaction.SigningBytes())) * feeRate
}

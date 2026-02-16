package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// formatAmount converts raw base units to a human-readable decimal string.
func formatAmount(units uint64) string {
	whole := units / config.Coin
	frac := units % config.Coin
	return fmt.Sprintf("%d.%012d", whole, frac)
}

// parseAmount converts a decimal string to raw base units.
func parseAmount(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty amount")
	}
	if strings.HasPrefix(s, "-") {
		return 0, fmt.Errorf("negative amount")
	}

	parts := strings.SplitN(s, ".", 2)

	whole, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid whole part: %w", err)
	}

	var frac uint64
	if len(parts) == 2 {
		fracStr := parts[1]
		if len(fracStr) > config.Decimals {
			return 0, fmt.Errorf("too many decimal places (max %d)", config.Decimals)
		}
		fracStr = fracStr + strings.Repeat("0", config.Decimals-len(fracStr))
		frac, err = strconv.ParseUint(fracStr, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid fractional part: %w", err)
		}
	}

	if whole > math.MaxUint64/config.Coin {
		return 0, fmt.Errorf("amount too large")
	}
	result := whole * config.Coin
	if result > math.MaxUint64-frac {
		return 0, fmt.Errorf("amount too large")
	}

	return result + frac, nil
}

// validateAddress parses an address string (bech32 or hex) and returns a types.Address.
func validateAddress(s string) (types.Address, error) {
	return types.ParseAddress(s)
}

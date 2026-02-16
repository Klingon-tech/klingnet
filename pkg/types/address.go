package types

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// AddressSize is the length of an address in bytes.
const AddressSize = 20

// Address HRP (human-readable part) constants for bech32 encoding.
const (
	MainnetHRP = "kgx"
	TestnetHRP = "tkgx"
)

// Deprecated: Use MainnetHRP/TestnetHRP instead.
const (
	MainnetPrefix = "kgx:"
	TestnetPrefix = "tkgx:"
)

// activeHRP is the address HRP used by String() and MarshalJSON().
// Set once at startup via SetAddressHRP(). Default is mainnet.
var activeHRP = MainnetHRP

// SetAddressHRP sets the active address HRP (call once at startup).
func SetAddressHRP(hrp string) {
	activeHRP = hrp
}

// GetAddressHRP returns the currently active address HRP.
func GetAddressHRP() string {
	return activeHRP
}

// Deprecated: Use SetAddressHRP instead.
func SetAddressPrefix(prefix string) {
	switch prefix {
	case TestnetPrefix:
		activeHRP = TestnetHRP
	default:
		activeHRP = MainnetHRP
	}
}

// Deprecated: Use GetAddressHRP instead.
func GetAddressPrefix() string {
	return activeHRP
}

// Address represents a 160-bit address (public key hash).
type Address [AddressSize]byte

// IsZero returns true if the address is all zeros.
func (a Address) IsZero() bool {
	return a == Address{}
}

// String returns the bech32-encoded address (e.g. "kgx1...").
func (a Address) String() string {
	s, err := Bech32Encode(activeHRP, a[:])
	if err != nil {
		// Fallback to hex if encoding fails (should never happen).
		return activeHRP + ":" + hex.EncodeToString(a[:])
	}
	return s
}

// Hex returns the raw hex-encoded address without prefix.
func (a Address) Hex() string {
	return hex.EncodeToString(a[:])
}

// Bytes returns a copy of the address as a byte slice.
func (a Address) Bytes() []byte {
	b := make([]byte, AddressSize)
	copy(b, a[:])
	return b
}

// MarshalJSON encodes the address as a bech32 string.
func (a Address) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.String())
}

// UnmarshalJSON decodes a bech32, prefixed hex, or raw hex string into an address.
func (a *Address) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		*a = Address{}
		return nil
	}
	parsed, err := ParseAddress(s)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}

// ParseAddress parses a bech32 or raw hex address string.
// Accepts: bech32 ("kgx1...", "tkgx1..."), legacy prefixed hex ("kgx:<hex>", "tkgx:<hex>"),
// or raw 40-char hex (for genesis/internal use).
func ParseAddress(s string) (Address, error) {
	if s == "" {
		return Address{}, fmt.Errorf("empty address")
	}

	// Try bech32 first: contains "1" separator, no ":" colon, and not pure hex.
	if strings.Contains(s, "1") && !strings.Contains(s, ":") && !isHex40(s) {
		_, data, err := Bech32Decode(s)
		if err != nil {
			return Address{}, fmt.Errorf("invalid bech32 address: %w", err)
		}
		if len(data) != AddressSize {
			return Address{}, fmt.Errorf("address must be %d bytes, got %d", AddressSize, len(data))
		}
		var a Address
		copy(a[:], data)
		return a, nil
	}

	// Legacy prefixed hex: strip "kgx:" or "tkgx:" prefix.
	hexStr := s
	if strings.HasPrefix(s, "kgx:") {
		hexStr = s[4:]
	} else if strings.HasPrefix(s, "tkgx:") {
		hexStr = s[5:]
	}

	decoded, err := hex.DecodeString(hexStr)
	if err != nil {
		return Address{}, fmt.Errorf("invalid address: %w", err)
	}
	if len(decoded) != AddressSize {
		return Address{}, fmt.Errorf("address must be %d bytes, got %d", AddressSize, len(decoded))
	}
	var a Address
	copy(a[:], decoded)
	return a, nil
}

// HexToAddress converts a raw hex string to an Address.
// Returns an error if the string is not exactly 40 hex characters.
// For user-facing input that may have a prefix, use ParseAddress instead.
func HexToAddress(s string) (Address, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return Address{}, fmt.Errorf("invalid hex: %w", err)
	}
	if len(b) != AddressSize {
		return Address{}, fmt.Errorf("address must be %d bytes, got %d", AddressSize, len(b))
	}
	var a Address
	copy(a[:], b)
	return a, nil
}

// isHex40 returns true if s is exactly 40 hex characters.
func isHex40(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

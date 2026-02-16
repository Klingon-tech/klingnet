package types

import (
	"fmt"
	"strings"
)

// Bech32 charset used for encoding (BIP-173).
const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

// bech32CharsetRev maps bech32 characters to their 5-bit values. -1 = invalid.
var bech32CharsetRev [128]int8

func init() {
	for i := range bech32CharsetRev {
		bech32CharsetRev[i] = -1
	}
	for i, c := range bech32Charset {
		bech32CharsetRev[c] = int8(i)
	}
}

// Bech32Encode encodes a human-readable part and data bytes into a bech32 string.
func Bech32Encode(hrp string, data []byte) (string, error) {
	if len(hrp) == 0 {
		return "", fmt.Errorf("bech32: empty HRP")
	}
	for _, c := range hrp {
		if c < 33 || c > 126 {
			return "", fmt.Errorf("bech32: invalid HRP character %q", c)
		}
	}

	// Convert 8-bit data to 5-bit groups.
	conv, err := convertBits(data, 8, 5, true)
	if err != nil {
		return "", fmt.Errorf("bech32: convert bits: %w", err)
	}

	// Compute checksum.
	chk := bech32CreateChecksum(hrp, conv)

	// Build result: hrp + "1" + data + checksum
	var sb strings.Builder
	sb.Grow(len(hrp) + 1 + len(conv) + 6)
	sb.WriteString(hrp)
	sb.WriteByte('1')
	for _, b := range conv {
		sb.WriteByte(bech32Charset[b])
	}
	for _, b := range chk {
		sb.WriteByte(bech32Charset[b])
	}
	return sb.String(), nil
}

// Bech32Decode decodes a bech32 string into the human-readable part and data bytes.
func Bech32Decode(s string) (string, []byte, error) {
	if len(s) == 0 {
		return "", nil, fmt.Errorf("bech32: empty string")
	}

	// Reject mixed case.
	hasUpper := false
	hasLower := false
	for _, c := range s {
		if c >= 'A' && c <= 'Z' {
			hasUpper = true
		}
		if c >= 'a' && c <= 'z' {
			hasLower = true
		}
	}
	if hasUpper && hasLower {
		return "", nil, fmt.Errorf("bech32: mixed case")
	}

	// Work in lowercase.
	s = strings.ToLower(s)

	// Find the last '1' separator.
	sepIdx := strings.LastIndex(s, "1")
	if sepIdx < 1 {
		return "", nil, fmt.Errorf("bech32: missing separator")
	}
	if sepIdx+7 > len(s) {
		return "", nil, fmt.Errorf("bech32: too short")
	}

	hrp := s[:sepIdx]
	dataStr := s[sepIdx+1:]

	// Decode data characters.
	data5 := make([]byte, len(dataStr))
	for i, c := range dataStr {
		if c > 127 {
			return "", nil, fmt.Errorf("bech32: invalid character %q", c)
		}
		val := bech32CharsetRev[c]
		if val < 0 {
			return "", nil, fmt.Errorf("bech32: invalid character %q", c)
		}
		data5[i] = byte(val)
	}

	// Verify checksum (last 6 characters).
	if !bech32VerifyChecksum(hrp, data5) {
		return "", nil, fmt.Errorf("bech32: invalid checksum")
	}

	// Strip checksum from data.
	data5 = data5[:len(data5)-6]

	// Convert 5-bit groups back to 8-bit.
	data8, err := convertBits(data5, 5, 8, false)
	if err != nil {
		return "", nil, fmt.Errorf("bech32: convert bits: %w", err)
	}

	return hrp, data8, nil
}

// bech32Polymod computes the bech32 polynomial modulus.
func bech32Polymod(values []byte) uint32 {
	gen := [5]uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	chk := uint32(1)
	for _, v := range values {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ uint32(v)
		for i := 0; i < 5; i++ {
			if (top>>uint(i))&1 == 1 {
				chk ^= gen[i]
			}
		}
	}
	return chk
}

// bech32HRPExpand expands the HRP for checksum computation.
func bech32HRPExpand(hrp string) []byte {
	ret := make([]byte, 0, len(hrp)*2+1)
	for _, c := range hrp {
		ret = append(ret, byte(c>>5))
	}
	ret = append(ret, 0)
	for _, c := range hrp {
		ret = append(ret, byte(c&31))
	}
	return ret
}

// bech32CreateChecksum creates a 6-byte checksum for the given HRP and data.
func bech32CreateChecksum(hrp string, data []byte) []byte {
	values := append(bech32HRPExpand(hrp), data...)
	values = append(values, 0, 0, 0, 0, 0, 0)
	polymod := bech32Polymod(values) ^ 1
	ret := make([]byte, 6)
	for i := 0; i < 6; i++ {
		ret[i] = byte((polymod >> uint(5*(5-i))) & 31)
	}
	return ret
}

// bech32VerifyChecksum verifies the checksum of the given HRP and data (including checksum).
func bech32VerifyChecksum(hrp string, data []byte) bool {
	return bech32Polymod(append(bech32HRPExpand(hrp), data...)) == 1
}

// convertBits converts between bit groups.
// fromBits/toBits are the source/destination group sizes (e.g. 8 and 5).
// pad controls whether incomplete groups are zero-padded.
func convertBits(data []byte, fromBits, toBits uint, pad bool) ([]byte, error) {
	acc := uint32(0)
	bits := uint(0)
	maxv := uint32((1 << toBits) - 1)
	var ret []byte

	for _, b := range data {
		if uint32(b)>>fromBits != 0 {
			return nil, fmt.Errorf("invalid data byte: %d", b)
		}
		acc = acc<<fromBits | uint32(b)
		bits += fromBits
		for bits >= toBits {
			bits -= toBits
			ret = append(ret, byte((acc>>bits)&maxv))
		}
	}

	if pad {
		if bits > 0 {
			ret = append(ret, byte((acc<<(toBits-bits))&maxv))
		}
	} else {
		if bits >= fromBits {
			return nil, fmt.Errorf("non-zero padding")
		}
		if (acc<<(toBits-bits))&maxv != 0 {
			return nil, fmt.Errorf("non-zero padding")
		}
	}

	return ret, nil
}

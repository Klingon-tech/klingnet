package subchain

import (
	"encoding/binary"
	"fmt"

	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// AnchorDataSize is the byte size of encoded anchor data: ChainID(32) + StateRoot(32) + Height(8).
const AnchorDataSize = 72

// Anchor represents a sub-chain state commitment to the root chain.
type Anchor struct {
	ChainID     types.ChainID // Which sub-chain
	StateRoot   types.Hash    // Sub-chain tip hash at the time of anchoring
	BlockHeight uint64        // Sub-chain block height being anchored
}

// EncodeAnchorData serializes an Anchor to its binary format (72 bytes).
func EncodeAnchorData(anchor *Anchor) []byte {
	buf := make([]byte, AnchorDataSize)
	copy(buf[0:32], anchor.ChainID[:])
	copy(buf[32:64], anchor.StateRoot[:])
	binary.BigEndian.PutUint64(buf[64:72], anchor.BlockHeight)
	return buf
}

// DecodeAnchorData deserializes binary data into an Anchor.
func DecodeAnchorData(data []byte) (*Anchor, error) {
	if len(data) != AnchorDataSize {
		return nil, fmt.Errorf("anchor data must be %d bytes, got %d", AnchorDataSize, len(data))
	}
	var a Anchor
	copy(a.ChainID[:], data[0:32])
	copy(a.StateRoot[:], data[32:64])
	a.BlockHeight = binary.BigEndian.Uint64(data[64:72])
	return &a, nil
}

// ValidateAnchor checks that an anchor references a registered sub-chain.
func ValidateAnchor(anchor *Anchor, registry *Registry) error {
	if !registry.Has(anchor.ChainID) {
		return fmt.Errorf("anchor references unknown sub-chain %s", anchor.ChainID)
	}
	return nil
}

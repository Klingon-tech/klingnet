package subchain

import (
	"testing"

	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func TestAnchorData_Roundtrip(t *testing.T) {
	anchor := &Anchor{
		ChainID:     types.ChainID{0xAA, 0xBB},
		StateRoot:   types.Hash{0xCC, 0xDD},
		BlockHeight: 12345,
	}

	data := EncodeAnchorData(anchor)
	if len(data) != AnchorDataSize {
		t.Fatalf("encoded size = %d, want %d", len(data), AnchorDataSize)
	}

	decoded, err := DecodeAnchorData(data)
	if err != nil {
		t.Fatalf("DecodeAnchorData: %v", err)
	}

	if decoded.ChainID != anchor.ChainID {
		t.Fatalf("ChainID = %s, want %s", decoded.ChainID, anchor.ChainID)
	}
	if decoded.StateRoot != anchor.StateRoot {
		t.Fatalf("StateRoot = %s, want %s", decoded.StateRoot, anchor.StateRoot)
	}
	if decoded.BlockHeight != anchor.BlockHeight {
		t.Fatalf("BlockHeight = %d, want %d", decoded.BlockHeight, anchor.BlockHeight)
	}
}

func TestDecodeAnchorData_WrongSize(t *testing.T) {
	_, err := DecodeAnchorData([]byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for wrong size")
	}
}

func TestValidateAnchor_RegisteredChain(t *testing.T) {
	reg := NewRegistry()
	chainID := types.ChainID{1}
	reg.Register(&SubChain{ID: chainID})

	anchor := &Anchor{ChainID: chainID, StateRoot: types.Hash{2}, BlockHeight: 5}
	if err := ValidateAnchor(anchor, reg); err != nil {
		t.Fatalf("ValidateAnchor for registered chain: %v", err)
	}
}

func TestValidateAnchor_UnknownChain(t *testing.T) {
	reg := NewRegistry()
	anchor := &Anchor{ChainID: types.ChainID{99}, StateRoot: types.Hash{2}, BlockHeight: 5}
	if err := ValidateAnchor(anchor, reg); err == nil {
		t.Fatal("expected error for unknown chain")
	}
}

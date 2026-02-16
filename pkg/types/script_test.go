package types

import "testing"

func TestScriptType_String(t *testing.T) {
	tests := []struct {
		st   ScriptType
		want string
	}{
		{ScriptTypeP2PKH, "P2PKH"},
		{ScriptTypeP2SH, "P2SH"},
		{ScriptTypeMint, "Mint"},
		{ScriptTypeBurn, "Burn"},
		{ScriptTypeAnchor, "Anchor"},
		{ScriptTypeRegister, "Register"},
		{ScriptTypeBridge, "Bridge"},
		{ScriptType(0xFF), "Unknown"},
		{ScriptType(0x00), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.st.String(); got != tt.want {
				t.Errorf("ScriptType(%#x).String() = %q, want %q", uint8(tt.st), got, tt.want)
			}
		})
	}
}

func TestScriptType_Values(t *testing.T) {
	// Verify the actual byte values are correct (these are protocol constants)
	if ScriptTypeP2PKH != 0x01 {
		t.Errorf("P2PKH = %#x, want 0x01", uint8(ScriptTypeP2PKH))
	}
	if ScriptTypeP2SH != 0x02 {
		t.Errorf("P2SH = %#x, want 0x02", uint8(ScriptTypeP2SH))
	}
	if ScriptTypeMint != 0x10 {
		t.Errorf("Mint = %#x, want 0x10", uint8(ScriptTypeMint))
	}
	if ScriptTypeBurn != 0x11 {
		t.Errorf("Burn = %#x, want 0x11", uint8(ScriptTypeBurn))
	}
	if ScriptTypeAnchor != 0x20 {
		t.Errorf("Anchor = %#x, want 0x20", uint8(ScriptTypeAnchor))
	}
	if ScriptTypeRegister != 0x21 {
		t.Errorf("Register = %#x, want 0x21", uint8(ScriptTypeRegister))
	}
	if ScriptTypeBridge != 0x30 {
		t.Errorf("Bridge = %#x, want 0x30", uint8(ScriptTypeBridge))
	}
}

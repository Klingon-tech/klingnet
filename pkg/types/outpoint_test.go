package types

import (
	"strings"
	"testing"
)

func TestOutpoint_IsZero(t *testing.T) {
	var zero Outpoint
	if !zero.IsZero() {
		t.Error("zero-value Outpoint should be zero")
	}

	// Non-zero TxID
	nonZero := Outpoint{TxID: Hash{0x01}, Index: 0}
	if nonZero.IsZero() {
		t.Error("Outpoint with non-zero TxID should not be zero")
	}

	// Non-zero index
	nonZero2 := Outpoint{TxID: Hash{}, Index: 1}
	if nonZero2.IsZero() {
		t.Error("Outpoint with non-zero Index should not be zero")
	}
}

func TestOutpoint_String(t *testing.T) {
	o := Outpoint{
		TxID:  Hash{0xab},
		Index: 3,
	}
	s := o.String()

	// Should contain the txid hex and :index
	if !strings.HasPrefix(s, "ab") {
		t.Errorf("String() should start with txid hex, got %s", s)
	}
	if !strings.HasSuffix(s, ":3") {
		t.Errorf("String() should end with ':3', got %s", s)
	}

	// Zero outpoint
	var zero Outpoint
	zs := zero.String()
	if !strings.HasSuffix(zs, ":0") {
		t.Errorf("zero Outpoint String() should end with ':0', got %s", zs)
	}
}

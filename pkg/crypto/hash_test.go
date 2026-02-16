package crypto

import (
	"encoding/hex"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func hexToHash(t *testing.T, s string) types.Hash {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex: %v", err)
	}
	var h types.Hash
	copy(h[:], b)
	return h
}

func TestHash(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "empty input",
			input: []byte{},
			want:  "af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262",
		},
		{
			name:  "hello",
			input: []byte("hello"),
			want:  "ea8f163db38682925e4491c5e58d4bb3506ef8c14eb78a86e908c5624a67200f",
		},
		{
			name:  "klingnet",
			input: []byte("klingnet"),
			want:  "677c013a662a24fb62497787316a59230409463ee36a1d7a57ba32607e20f467",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Hash(tt.input)
			want := hexToHash(t, tt.want)
			if got != want {
				t.Errorf("Hash(%q) = %x, want %x", tt.input, got, want)
			}
		})
	}
}

func TestHash_Deterministic(t *testing.T) {
	data := []byte("deterministic test input")
	h1 := Hash(data)
	h2 := Hash(data)
	if h1 != h2 {
		t.Errorf("Hash is not deterministic: %x != %x", h1, h2)
	}
}

func TestHash_DifferentInputs(t *testing.T) {
	h1 := Hash([]byte("input A"))
	h2 := Hash([]byte("input B"))
	if h1 == h2 {
		t.Error("different inputs produced the same hash")
	}
}

func TestDoubleHash(t *testing.T) {
	input := []byte("hello")
	got := DoubleHash(input)
	want := hexToHash(t, "0f79bf7f41e10b873e0f24b701159b4951037967529d18dcacc9392a8fbf5163")

	if got != want {
		t.Errorf("DoubleHash(%q) = %x, want %x", input, got, want)
	}
}

func TestDoubleHash_NotSameAsHash(t *testing.T) {
	data := []byte("test data")
	single := Hash(data)
	double := DoubleHash(data)
	if single == double {
		t.Error("DoubleHash should not equal single Hash")
	}
}

func TestHashConcat(t *testing.T) {
	a := Hash([]byte("left"))
	b := Hash([]byte("right"))
	result := HashConcat(a, b)

	// Should not be zero
	if result == (types.Hash{}) {
		t.Error("HashConcat returned zero hash")
	}

	// Order matters
	reversed := HashConcat(b, a)
	if result == reversed {
		t.Error("HashConcat(a,b) should differ from HashConcat(b,a)")
	}

	// Deterministic
	again := HashConcat(a, b)
	if result != again {
		t.Error("HashConcat is not deterministic")
	}
}

func TestHashConcat_EqualsManualConcat(t *testing.T) {
	a := Hash([]byte("left"))
	b := Hash([]byte("right"))

	// Manual concatenation and hash
	var buf [64]byte
	copy(buf[:32], a[:])
	copy(buf[32:], b[:])
	want := Hash(buf[:])

	got := HashConcat(a, b)
	if got != want {
		t.Errorf("HashConcat = %x, want %x", got, want)
	}
}

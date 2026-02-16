package block

import (
	"testing"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

func TestComputeMerkleRoot_Empty(t *testing.T) {
	root := ComputeMerkleRoot(nil)
	if !root.IsZero() {
		t.Errorf("empty input should return zero hash, got %s", root)
	}

	root2 := ComputeMerkleRoot([]types.Hash{})
	if !root2.IsZero() {
		t.Errorf("empty slice should return zero hash, got %s", root2)
	}
}

func TestComputeMerkleRoot_SingleHash(t *testing.T) {
	h := crypto.Hash([]byte("single tx"))
	root := ComputeMerkleRoot([]types.Hash{h})
	if root != h {
		t.Errorf("single hash should return itself: got %s, want %s", root, h)
	}
}

func TestComputeMerkleRoot_TwoHashes(t *testing.T) {
	h1 := crypto.Hash([]byte("tx1"))
	h2 := crypto.Hash([]byte("tx2"))

	root := ComputeMerkleRoot([]types.Hash{h1, h2})
	want := crypto.HashConcat(h1, h2)

	if root != want {
		t.Errorf("two hashes: got %s, want %s", root, want)
	}
}

func TestComputeMerkleRoot_ThreeHashes(t *testing.T) {
	h1 := crypto.Hash([]byte("tx1"))
	h2 := crypto.Hash([]byte("tx2"))
	h3 := crypto.Hash([]byte("tx3"))

	root := ComputeMerkleRoot([]types.Hash{h1, h2, h3})

	// With 3 hashes: h3 is duplicated -> [h1, h2, h3, h3]
	// Level 1: [Hash(h1||h2), Hash(h3||h3)]
	// Level 2: Hash(Hash(h1||h2) || Hash(h3||h3))
	left := crypto.HashConcat(h1, h2)
	right := crypto.HashConcat(h3, h3)
	want := crypto.HashConcat(left, right)

	if root != want {
		t.Errorf("three hashes: got %s, want %s", root, want)
	}
}

func TestComputeMerkleRoot_FourHashes(t *testing.T) {
	h1 := crypto.Hash([]byte("tx1"))
	h2 := crypto.Hash([]byte("tx2"))
	h3 := crypto.Hash([]byte("tx3"))
	h4 := crypto.Hash([]byte("tx4"))

	root := ComputeMerkleRoot([]types.Hash{h1, h2, h3, h4})

	// Level 1: [Hash(h1||h2), Hash(h3||h4)]
	// Level 2: Hash(Hash(h1||h2) || Hash(h3||h4))
	left := crypto.HashConcat(h1, h2)
	right := crypto.HashConcat(h3, h4)
	want := crypto.HashConcat(left, right)

	if root != want {
		t.Errorf("four hashes: got %s, want %s", root, want)
	}
}

func TestComputeMerkleRoot_Deterministic(t *testing.T) {
	hashes := make([]types.Hash, 5)
	for i := range hashes {
		hashes[i] = crypto.Hash([]byte{byte(i)})
	}

	r1 := ComputeMerkleRoot(hashes)
	r2 := ComputeMerkleRoot(hashes)
	if r1 != r2 {
		t.Error("merkle root is not deterministic")
	}
}

func TestComputeMerkleRoot_OrderMatters(t *testing.T) {
	h1 := crypto.Hash([]byte("tx1"))
	h2 := crypto.Hash([]byte("tx2"))

	r1 := ComputeMerkleRoot([]types.Hash{h1, h2})
	r2 := ComputeMerkleRoot([]types.Hash{h2, h1})

	if r1 == r2 {
		t.Error("different ordering should produce different merkle root")
	}
}

func TestComputeMerkleRoot_DoesNotMutateInput(t *testing.T) {
	h1 := crypto.Hash([]byte("tx1"))
	h2 := crypto.Hash([]byte("tx2"))
	h3 := crypto.Hash([]byte("tx3"))

	original := []types.Hash{h1, h2, h3}
	input := make([]types.Hash, len(original))
	copy(input, original)

	ComputeMerkleRoot(input)

	for i := range input {
		if input[i] != original[i] {
			t.Errorf("input[%d] was mutated: got %s, want %s", i, input[i], original[i])
		}
	}
}

func TestComputeMerkleRoot_LargerTree(t *testing.T) {
	// 7 hashes - exercises multi-level odd padding
	hashes := make([]types.Hash, 7)
	for i := range hashes {
		hashes[i] = crypto.Hash([]byte{byte(i)})
	}

	root := ComputeMerkleRoot(hashes)

	// Should not be zero
	if root.IsZero() {
		t.Error("merkle root of 7 hashes should not be zero")
	}

	// Should be deterministic
	root2 := ComputeMerkleRoot(hashes)
	if root != root2 {
		t.Error("merkle root of 7 hashes is not deterministic")
	}
}

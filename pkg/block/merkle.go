package block

import (
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// ComputeMerkleRoot calculates the merkle root of transaction hashes.
//
// Algorithm:
//   - 0 hashes: returns zero hash
//   - 1 hash: returns that hash
//   - Otherwise: pairwise hash, duplicating the last element if odd count,
//     then recurse on the resulting layer until one hash remains.
func ComputeMerkleRoot(txHashes []types.Hash) types.Hash {
	if len(txHashes) == 0 {
		return types.Hash{}
	}
	if len(txHashes) == 1 {
		return txHashes[0]
	}

	// Work on a copy so we don't mutate the caller's slice.
	level := make([]types.Hash, len(txHashes))
	copy(level, txHashes)

	for len(level) > 1 {
		// If odd, duplicate the last element.
		if len(level)%2 != 0 {
			level = append(level, level[len(level)-1])
		}

		next := make([]types.Hash, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			next[i/2] = crypto.HashConcat(level[i], level[i+1])
		}
		level = next
	}

	return level[0]
}

package utxo

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// Commitment computes a merkle root over all UTXOs in the store.
// Each UTXO is hashed deterministically, the hashes are sorted, and
// a merkle tree is built from them. Returns a zero hash for an empty set.
func Commitment(store *Store) (types.Hash, error) {
	var hashes []types.Hash

	err := store.ForEach(func(u *UTXO) error {
		hashes = append(hashes, hashUTXO(u))
		return nil
	})
	if err != nil {
		return types.Hash{}, fmt.Errorf("utxo commitment: %w", err)
	}

	if len(hashes) == 0 {
		return types.Hash{}, nil
	}

	// Sort for deterministic ordering (map iteration order varies).
	sort.Slice(hashes, func(i, j int) bool {
		return hashLess(hashes[i], hashes[j])
	})

	return block.ComputeMerkleRoot(hashes), nil
}

// hashUTXO produces a deterministic BLAKE3 hash of a UTXO.
// Format: txid(32) | index(4) | value(8) | script_type(1) | script_data
func hashUTXO(u *UTXO) types.Hash {
	var buf []byte
	buf = append(buf, u.Outpoint.TxID[:]...)
	buf = binary.LittleEndian.AppendUint32(buf, u.Outpoint.Index)
	buf = binary.LittleEndian.AppendUint64(buf, u.Value)
	buf = append(buf, byte(u.Script.Type))
	buf = append(buf, u.Script.Data...)
	return crypto.Hash(buf)
}

func hashLess(a, b types.Hash) bool {
	for i := 0; i < types.HashSize; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

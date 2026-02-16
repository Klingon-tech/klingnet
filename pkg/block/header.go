package block

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// Header contains block metadata.
type Header struct {
	Version      uint32     `json:"version"`
	PrevHash     types.Hash `json:"prev_hash"`
	MerkleRoot   types.Hash `json:"merkle_root"`
	Timestamp    uint64     `json:"timestamp"`
	Height       uint64     `json:"height"`
	Difficulty   uint64     `json:"difficulty,omitempty"` // PoW: target difficulty (0 for PoA blocks)
	Nonce        uint64     `json:"nonce"`
	ValidatorSig []byte     `json:"validator_sig,omitempty"`
}

// headerJSON is the JSON representation of Header with hex-encoded validator sig.
type headerJSON struct {
	Version      uint32     `json:"version"`
	PrevHash     types.Hash `json:"prev_hash"`
	MerkleRoot   types.Hash `json:"merkle_root"`
	Timestamp    uint64     `json:"timestamp"`
	Height       uint64     `json:"height"`
	Difficulty   uint64     `json:"difficulty,omitempty"`
	Nonce        uint64     `json:"nonce"`
	ValidatorSig string     `json:"validator_sig,omitempty"`
}

// MarshalJSON encodes the header with hex-encoded validator signature.
func (h *Header) MarshalJSON() ([]byte, error) {
	j := headerJSON{
		Version:    h.Version,
		PrevHash:   h.PrevHash,
		MerkleRoot: h.MerkleRoot,
		Timestamp:  h.Timestamp,
		Height:     h.Height,
		Difficulty: h.Difficulty,
		Nonce:      h.Nonce,
	}
	if h.ValidatorSig != nil {
		j.ValidatorSig = hex.EncodeToString(h.ValidatorSig)
	}
	return json.Marshal(j)
}

// UnmarshalJSON decodes a header with hex-encoded validator signature.
func (h *Header) UnmarshalJSON(data []byte) error {
	var j headerJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	h.Version = j.Version
	h.PrevHash = j.PrevHash
	h.MerkleRoot = j.MerkleRoot
	h.Timestamp = j.Timestamp
	h.Height = j.Height
	h.Difficulty = j.Difficulty
	h.Nonce = j.Nonce
	if j.ValidatorSig != "" {
		b, err := hex.DecodeString(j.ValidatorSig)
		if err != nil {
			return err
		}
		h.ValidatorSig = b
	}
	return nil
}

// Hash computes the block header hash.
// Excludes ValidatorSig so the hash is stable for signing.
func (h *Header) Hash() types.Hash {
	return crypto.Hash(h.SigningBytes())
}

// SigningBytes returns the canonical bytes for hashing/signing.
// Format: version(4) | prev_hash(32) | merkle_root(32) | timestamp(8) | height(8) | difficulty(8) | nonce(8)
func (h *Header) SigningBytes() []byte {
	buf := make([]byte, 0, 100)
	buf = binary.LittleEndian.AppendUint32(buf, h.Version)
	buf = append(buf, h.PrevHash[:]...)
	buf = append(buf, h.MerkleRoot[:]...)
	buf = binary.LittleEndian.AppendUint64(buf, h.Timestamp)
	buf = binary.LittleEndian.AppendUint64(buf, h.Height)
	buf = binary.LittleEndian.AppendUint64(buf, h.Difficulty)
	buf = binary.LittleEndian.AppendUint64(buf, h.Nonce)
	return buf
}

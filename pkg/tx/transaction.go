// Package tx defines transaction types and validation.
package tx

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// Transaction represents a blockchain transaction.
type Transaction struct {
	Version  uint32   `json:"version"`
	Inputs   []Input  `json:"inputs"`
	Outputs  []Output `json:"outputs"`
	LockTime uint64   `json:"locktime"`
}

// Input references a UTXO being spent.
type Input struct {
	PrevOut   types.Outpoint `json:"prevout"`
	Signature []byte         `json:"signature"`
	PubKey    []byte         `json:"pubkey"`
}

// inputJSON is the JSON representation of Input with hex-encoded byte fields.
type inputJSON struct {
	PrevOut   types.Outpoint `json:"prevout"`
	Signature *string        `json:"signature"`
	PubKey    *string        `json:"pubkey"`
}

// MarshalJSON encodes the input with hex-encoded signature and pubkey.
func (in Input) MarshalJSON() ([]byte, error) {
	j := inputJSON{PrevOut: in.PrevOut}
	if in.Signature != nil {
		s := hex.EncodeToString(in.Signature)
		j.Signature = &s
	}
	if in.PubKey != nil {
		p := hex.EncodeToString(in.PubKey)
		j.PubKey = &p
	}
	return json.Marshal(j)
}

// UnmarshalJSON decodes an input with hex-encoded signature and pubkey.
func (in *Input) UnmarshalJSON(data []byte) error {
	var j inputJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	in.PrevOut = j.PrevOut
	if j.Signature != nil {
		b, err := hex.DecodeString(*j.Signature)
		if err != nil {
			return err
		}
		in.Signature = b
	}
	if j.PubKey != nil {
		b, err := hex.DecodeString(*j.PubKey)
		if err != nil {
			return err
		}
		in.PubKey = b
	}
	return nil
}

// Output defines a new UTXO.
type Output struct {
	Value  uint64           `json:"value"`
	Script types.Script     `json:"script"`
	Token  *types.TokenData `json:"token,omitempty"`
}

// Hash computes the transaction ID (BLAKE3 hash of the serialized signing data).
// This excludes signatures to avoid circular dependency.
func (tx *Transaction) Hash() types.Hash {
	return crypto.Hash(tx.SigningBytes())
}

// SigningBytes returns the canonical byte representation used for signing.
// Format: version(4) | input_count(4) | [prevout(36)]... | output_count(4) | [value(8) + script_type(1) + script_data_len(4) + script_data]... | locktime(8)
func (tx *Transaction) SigningBytes() []byte {
	var buf []byte

	// Version.
	buf = binary.LittleEndian.AppendUint32(buf, tx.Version)

	// Input count + prevouts (no signatures, except coinbase data).
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(tx.Inputs)))
	for _, in := range tx.Inputs {
		buf = append(buf, in.PrevOut.TxID[:]...)
		buf = binary.LittleEndian.AppendUint32(buf, in.PrevOut.Index)
		// Include coinbase data (height) in the hash so each coinbase tx
		// has a unique ID. Regular inputs skip this (signature is excluded
		// to avoid circular dependency during signing).
		if in.PrevOut.IsZero() && len(in.Signature) > 0 {
			buf = binary.LittleEndian.AppendUint32(buf, uint32(len(in.Signature)))
			buf = append(buf, in.Signature...)
		}
	}

	// Output count + outputs.
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(tx.Outputs)))
	for _, out := range tx.Outputs {
		buf = binary.LittleEndian.AppendUint64(buf, out.Value)
		buf = append(buf, byte(out.Script.Type))
		buf = binary.LittleEndian.AppendUint32(buf, uint32(len(out.Script.Data)))
		buf = append(buf, out.Script.Data...)
		if out.Token != nil {
			buf = append(buf, out.Token.ID[:]...)
			buf = binary.LittleEndian.AppendUint64(buf, out.Token.Amount)
		}
	}

	// Locktime.
	buf = binary.LittleEndian.AppendUint64(buf, tx.LockTime)

	return buf
}

// TotalOutputValue returns the sum of all output values.
// Returns an error if the sum overflows uint64.
func (tx *Transaction) TotalOutputValue() (uint64, error) {
	var total uint64
	for _, out := range tx.Outputs {
		if total > math.MaxUint64-out.Value {
			return 0, fmt.Errorf("output value overflow")
		}
		total += out.Value
	}
	return total, nil
}

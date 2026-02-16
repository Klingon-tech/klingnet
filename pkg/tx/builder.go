package tx

import (
	"fmt"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// Builder constructs transactions incrementally.
type Builder struct {
	tx *Transaction
}

// NewBuilder creates a new transaction builder.
func NewBuilder() *Builder {
	return &Builder{
		tx: &Transaction{Version: 1},
	}
}

// AddInput adds an input referencing a previous output.
func (b *Builder) AddInput(prevOut types.Outpoint) *Builder {
	b.tx.Inputs = append(b.tx.Inputs, Input{PrevOut: prevOut})
	return b
}

// AddOutput adds an output with a value and script.
func (b *Builder) AddOutput(value uint64, script types.Script) *Builder {
	b.tx.Outputs = append(b.tx.Outputs, Output{Value: value, Script: script})
	return b
}

// AddTokenOutput adds a token-carrying output.
func (b *Builder) AddTokenOutput(value uint64, script types.Script, token types.TokenData) *Builder {
	b.tx.Outputs = append(b.tx.Outputs, Output{
		Value:  value,
		Script: script,
		Token:  &token,
	})
	return b
}

// SetLockTime sets the transaction lock time.
func (b *Builder) SetLockTime(lockTime uint64) *Builder {
	b.tx.LockTime = lockTime
	return b
}

// Sign signs all inputs with the provided private key.
// Each input gets the same signature (single-key spending).
func (b *Builder) Sign(key *crypto.PrivateKey) error {
	hash := b.tx.Hash()
	sig, err := key.Sign(hash[:])
	if err != nil {
		return fmt.Errorf("sign tx: %w", err)
	}
	pubKey := key.PublicKey()
	for i := range b.tx.Inputs {
		b.tx.Inputs[i].Signature = sig
		b.tx.Inputs[i].PubKey = pubKey
	}
	return nil
}

// SignMulti signs each input with the key that owns its outpoint.
// outpointAddr maps each input's outpoint to the address that owns it.
// signers maps each address to the private key that can spend from it.
func (b *Builder) SignMulti(
	signers map[types.Address]*crypto.PrivateKey,
	outpointAddr map[types.Outpoint]types.Address,
) error {
	hash := b.tx.Hash()

	// Cache signatures: same key always produces the same sig for the same hash.
	type sigPub struct {
		sig    []byte
		pubKey []byte
	}
	cache := make(map[types.Address]*sigPub)

	for i := range b.tx.Inputs {
		// Skip coinbase inputs.
		if b.tx.Inputs[i].PrevOut.IsZero() {
			continue
		}

		addr, ok := outpointAddr[b.tx.Inputs[i].PrevOut]
		if !ok {
			return fmt.Errorf("no address mapping for input %d outpoint", i)
		}
		key, ok := signers[addr]
		if !ok {
			return fmt.Errorf("no signer for address %s (input %d)", addr, i)
		}

		sp, cached := cache[addr]
		if !cached {
			sig, err := key.Sign(hash[:])
			if err != nil {
				return fmt.Errorf("sign input %d: %w", i, err)
			}
			sp = &sigPub{sig: sig, pubKey: key.PublicKey()}
			cache[addr] = sp
		}
		b.tx.Inputs[i].Signature = sp.sig
		b.tx.Inputs[i].PubKey = sp.pubKey
	}
	return nil
}

// Build returns the constructed transaction.
// Does NOT validate — call tx.Validate() separately.
func (b *Builder) Build() *Transaction {
	return b.tx
}

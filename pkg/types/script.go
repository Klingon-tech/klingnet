package types

import (
	"encoding/hex"
	"encoding/json"
)

// ScriptType identifies the type of locking/unlocking script.
type ScriptType uint8

const (
	ScriptTypeP2PKH    ScriptType = 0x01 // Pay to public key hash
	ScriptTypeP2SH     ScriptType = 0x02 // Pay to script hash
	ScriptTypeMint     ScriptType = 0x10 // Token mint operation
	ScriptTypeBurn     ScriptType = 0x11 // Token burn (unspendable)
	ScriptTypeAnchor   ScriptType = 0x20 // Sub-chain anchor commitment
	ScriptTypeRegister ScriptType = 0x21 // Sub-chain registration
	ScriptTypeBridge   ScriptType = 0x30 // Cross-chain bridge lock/unlock
	ScriptTypeStake    ScriptType = 0x40 // Validator stake lock (data = 33-byte compressed pubkey)
)

// String returns a human-readable name for the script type.
func (st ScriptType) String() string {
	switch st {
	case ScriptTypeP2PKH:
		return "P2PKH"
	case ScriptTypeP2SH:
		return "P2SH"
	case ScriptTypeMint:
		return "Mint"
	case ScriptTypeBurn:
		return "Burn"
	case ScriptTypeAnchor:
		return "Anchor"
	case ScriptTypeRegister:
		return "Register"
	case ScriptTypeBridge:
		return "Bridge"
	case ScriptTypeStake:
		return "Stake"
	default:
		return "Unknown"
	}
}

// Script defines the locking condition for a UTXO.
type Script struct {
	Type ScriptType `json:"type"`
	Data []byte     `json:"data"`
}

// scriptJSON is the JSON representation of a Script with hex-encoded data.
type scriptJSON struct {
	Type ScriptType `json:"type"`
	Data string     `json:"data"`
}

// MarshalJSON encodes the script with hex-encoded data.
func (s Script) MarshalJSON() ([]byte, error) {
	return json.Marshal(scriptJSON{
		Type: s.Type,
		Data: hex.EncodeToString(s.Data),
	})
}

// UnmarshalJSON decodes a script with hex-encoded data.
func (s *Script) UnmarshalJSON(data []byte) error {
	var j scriptJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	s.Type = j.Type
	if j.Data != "" {
		b, err := hex.DecodeString(j.Data)
		if err != nil {
			return err
		}
		s.Data = b
	}
	return nil
}

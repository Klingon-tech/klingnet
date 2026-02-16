package token

import (
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// EncodeMintData encodes a recipient address and token metadata into
// the Data field of a ScriptTypeMint output.
//
// Layout:
//
//	[20 bytes: recipient address]
//	[1 byte:  decimals]
//	[1 byte:  nameLen]
//	[nameLen bytes: name (UTF-8)]
//	[1 byte:  symbolLen]
//	[symbolLen bytes: symbol (UTF-8)]
//
// Legacy mint scripts used only the 20-byte address. Nodes that see
// len(Data)==20 treat it as a legacy mint with no embedded metadata.
func EncodeMintData(addr types.Address, name, symbol string, decimals uint8) []byte {
	nameBytes := []byte(name)
	symbolBytes := []byte(symbol)

	// Truncate to 255 bytes max each.
	if len(nameBytes) > 255 {
		nameBytes = nameBytes[:255]
	}
	if len(symbolBytes) > 255 {
		symbolBytes = symbolBytes[:255]
	}

	size := types.AddressSize + 1 + 1 + len(nameBytes) + 1 + len(symbolBytes)
	buf := make([]byte, size)

	off := 0
	copy(buf[off:], addr[:])
	off += types.AddressSize

	buf[off] = decimals
	off++

	buf[off] = uint8(len(nameBytes))
	off++
	copy(buf[off:], nameBytes)
	off += len(nameBytes)

	buf[off] = uint8(len(symbolBytes))
	off++
	copy(buf[off:], symbolBytes)

	return buf
}

// DecodeMintData extracts the recipient address and optional metadata
// from a mint script Data field. Returns ok=false if data is too short
// to contain an address (<20 bytes). For legacy 20-byte data, returns
// empty name/symbol and decimals=0.
func DecodeMintData(data []byte) (addr types.Address, name, symbol string, decimals uint8, ok bool) {
	if len(data) < types.AddressSize {
		return addr, "", "", 0, false
	}

	copy(addr[:], data[:types.AddressSize])

	// Legacy format: address only.
	if len(data) == types.AddressSize {
		return addr, "", "", 0, true
	}

	off := types.AddressSize

	// Need at least: decimals(1) + nameLen(1) + symbolLen(1) = 3 bytes.
	if len(data) < off+3 {
		return addr, "", "", 0, true
	}

	decimals = data[off]
	off++

	nameLen := int(data[off])
	off++
	if off+nameLen > len(data) {
		return addr, "", "", decimals, true
	}
	name = string(data[off : off+nameLen])
	off += nameLen

	if off >= len(data) {
		return addr, name, "", decimals, true
	}
	symbolLen := int(data[off])
	off++
	if off+symbolLen > len(data) {
		return addr, name, "", decimals, true
	}
	symbol = string(data[off : off+symbolLen])

	return addr, name, symbol, decimals, true
}

// ExtractAndStoreMetadata scans a block's transactions for mint outputs
// with embedded metadata and stores them in the token store. Tokens
// already present in the store are skipped.
func ExtractAndStoreMetadata(store *Store, blk *block.Block) {
	if store == nil || blk == nil {
		return
	}
	for _, t := range blk.Transactions {
		if t == nil {
			continue
		}
		for _, out := range t.Outputs {
			if out.Script.Type != types.ScriptTypeMint {
				continue
			}
			if out.Token == nil {
				continue
			}

			// Only process if metadata bytes are present (>20 bytes).
			if len(out.Script.Data) <= types.AddressSize {
				continue
			}

			tokenID := out.Token.ID

			// Skip if already stored.
			if has, _ := store.Has(tokenID); has {
				continue
			}

			addr, name, symbol, decimals, ok := DecodeMintData(out.Script.Data)
			if !ok || (name == "" && symbol == "") {
				continue
			}

			_ = store.Put(tokenID, &Metadata{
				Name:     name,
				Symbol:   symbol,
				Decimals: decimals,
				Creator:  addr,
			})
		}
	}
}

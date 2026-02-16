package utxo

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// Key prefixes for the UTXO store.
var (
	prefixUTXO  = []byte("u/") // u/<txid><index> -> UTXO JSON
	prefixAddr  = []byte("a/") // a/<address><txid><index> -> empty (index)
	prefixStake = []byte("k/") // k/<pubkey33><txid><index> -> empty (stake index)
)

// Store implements Set backed by a storage.DB.
type Store struct {
	db storage.DB
}

// NewStore creates a new UTXO store backed by the given database.
func NewStore(db storage.DB) *Store {
	return &Store{db: db}
}

// utxoKey builds a storage key for an outpoint: "u/" + txid(32) + index(4).
func utxoKey(op types.Outpoint) []byte {
	key := make([]byte, len(prefixUTXO)+types.HashSize+4)
	copy(key, prefixUTXO)
	copy(key[len(prefixUTXO):], op.TxID[:])
	binary.BigEndian.PutUint32(key[len(prefixUTXO)+types.HashSize:], op.Index)
	return key
}

// addrKey builds an address index key: "a/" + addr(20) + txid(32) + index(4).
func addrKey(addr types.Address, op types.Outpoint) []byte {
	key := make([]byte, len(prefixAddr)+types.AddressSize+types.HashSize+4)
	copy(key, prefixAddr)
	copy(key[len(prefixAddr):], addr[:])
	off := len(prefixAddr) + types.AddressSize
	copy(key[off:], op.TxID[:])
	binary.BigEndian.PutUint32(key[off+types.HashSize:], op.Index)
	return key
}

// compressedPubKeySize is the length of a compressed secp256k1 public key.
const compressedPubKeySize = 33

// stakeKey builds a stake index key: "k/" + pubkey(33) + txid(32) + index(4).
func stakeKey(pubKey []byte, op types.Outpoint) []byte {
	key := make([]byte, len(prefixStake)+compressedPubKeySize+types.HashSize+4)
	copy(key, prefixStake)
	copy(key[len(prefixStake):], pubKey)
	off := len(prefixStake) + compressedPubKeySize
	copy(key[off:], op.TxID[:])
	binary.BigEndian.PutUint32(key[off+types.HashSize:], op.Index)
	return key
}

// Get retrieves a UTXO by its outpoint.
func (s *Store) Get(outpoint types.Outpoint) (*UTXO, error) {
	data, err := s.db.Get(utxoKey(outpoint))
	if err != nil {
		return nil, fmt.Errorf("utxo get: %w", err)
	}
	var u UTXO
	if err := json.Unmarshal(data, &u); err != nil {
		return nil, fmt.Errorf("utxo unmarshal: %w", err)
	}
	return &u, nil
}

// scriptAddress returns the address embedded in a script, if any.
// P2PKH and Mint scripts both store a 20-byte address in Data.
func scriptAddress(s types.Script) (types.Address, bool) {
	switch s.Type {
	case types.ScriptTypeP2PKH, types.ScriptTypeMint:
		if len(s.Data) >= types.AddressSize {
			var addr types.Address
			copy(addr[:], s.Data[:types.AddressSize])
			return addr, true
		}
	}
	return types.Address{}, false
}

// Put stores a UTXO and updates the address index.
func (s *Store) Put(u *UTXO) error {
	data, err := json.Marshal(u)
	if err != nil {
		return fmt.Errorf("utxo marshal: %w", err)
	}
	if err := s.db.Put(utxoKey(u.Outpoint), data); err != nil {
		return fmt.Errorf("utxo put: %w", err)
	}

	// Index by address for script types that contain one.
	if addr, ok := scriptAddress(u.Script); ok {
		if err := s.db.Put(addrKey(addr, u.Outpoint), []byte{}); err != nil {
			return fmt.Errorf("utxo index put: %w", err)
		}
	}

	// Index by validator pubkey if it's a stake script.
	if u.Script.Type == types.ScriptTypeStake && len(u.Script.Data) == compressedPubKeySize {
		if err := s.db.Put(stakeKey(u.Script.Data, u.Outpoint), []byte{}); err != nil {
			return fmt.Errorf("stake index put: %w", err)
		}
	}

	return nil
}

// Delete removes a UTXO and its address index entry.
func (s *Store) Delete(outpoint types.Outpoint) error {
	// Read first to clean up secondary indexes.
	u, err := s.Get(outpoint)
	if err == nil {
		if addr, ok := scriptAddress(u.Script); ok {
			s.db.Delete(addrKey(addr, u.Outpoint))
		}
		if u.Script.Type == types.ScriptTypeStake && len(u.Script.Data) == compressedPubKeySize {
			s.db.Delete(stakeKey(u.Script.Data, u.Outpoint))
		}
	}

	if err := s.db.Delete(utxoKey(outpoint)); err != nil {
		return fmt.Errorf("utxo delete: %w", err)
	}
	return nil
}

// Has checks if a UTXO exists for the given outpoint.
func (s *Store) Has(outpoint types.Outpoint) (bool, error) {
	return s.db.Has(utxoKey(outpoint))
}

// ForEach iterates over all UTXOs in the store.
func (s *Store) ForEach(fn func(*UTXO) error) error {
	return s.db.ForEach(prefixUTXO, func(key, value []byte) error {
		var u UTXO
		if err := json.Unmarshal(value, &u); err != nil {
			return fmt.Errorf("utxo unmarshal: %w", err)
		}
		return fn(&u)
	})
}

// GetStakes returns all stake UTXOs locked by the given compressed public key.
// It scans the stake index and loads each referenced UTXO.
func (s *Store) GetStakes(pubKey []byte) ([]*UTXO, error) {
	if len(pubKey) != compressedPubKeySize {
		return nil, fmt.Errorf("pubkey must be %d bytes, got %d", compressedPubKeySize, len(pubKey))
	}

	// Build the prefix: "k/" + pubkey(33).
	prefix := make([]byte, len(prefixStake)+compressedPubKeySize)
	copy(prefix, prefixStake)
	copy(prefix[len(prefixStake):], pubKey)

	var utxos []*UTXO
	err := s.db.ForEach(prefix, func(key, _ []byte) error {
		// Key layout: "k/" + pubkey(33) + txid(32) + index(4).
		off := len(prefixStake) + compressedPubKeySize
		if len(key) < off+types.HashSize+4 {
			return nil // Malformed key, skip.
		}
		var op types.Outpoint
		copy(op.TxID[:], key[off:off+types.HashSize])
		op.Index = binary.BigEndian.Uint32(key[off+types.HashSize:])

		u, err := s.Get(op)
		if err != nil {
			return nil // UTXO may have been spent, skip.
		}
		utxos = append(utxos, u)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan stake index: %w", err)
	}
	return utxos, nil
}

// GetAllStakedValidators returns the unique compressed public keys of all
// validators that currently have stake UTXOs. It scans the "k/" stake index.
func (s *Store) GetAllStakedValidators() ([][]byte, error) {
	seen := make(map[string]struct{})
	var validators [][]byte

	err := s.db.ForEach(prefixStake, func(key, _ []byte) error {
		// Key layout: "k/" + pubkey(33) + txid(32) + index(4).
		if len(key) < len(prefixStake)+compressedPubKeySize {
			return nil
		}
		pk := key[len(prefixStake) : len(prefixStake)+compressedPubKeySize]
		pkStr := string(pk)
		if _, ok := seen[pkStr]; !ok {
			seen[pkStr] = struct{}{}
			pubKey := make([]byte, compressedPubKeySize)
			copy(pubKey, pk)
			validators = append(validators, pubKey)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan stake index: %w", err)
	}
	return validators, nil
}

// ClearAll removes all UTXOs and their secondary indexes (address, stake).
// Used during UTXO set recovery after a crash during reorg.
func (s *Store) ClearAll() error {
	var keys [][]byte
	for _, prefix := range [][]byte{prefixUTXO, prefixAddr, prefixStake} {
		if err := s.db.ForEach(prefix, func(key, _ []byte) error {
			k := make([]byte, len(key))
			copy(k, key)
			keys = append(keys, k)
			return nil
		}); err != nil {
			return fmt.Errorf("scan prefix %s: %w", prefix, err)
		}
	}
	for _, key := range keys {
		if err := s.db.Delete(key); err != nil {
			return fmt.Errorf("delete utxo key: %w", err)
		}
	}
	return nil
}

// GetByAddress returns all UTXOs belonging to the given address.
// It scans the address index and loads each referenced UTXO.
func (s *Store) GetByAddress(addr types.Address) ([]*UTXO, error) {
	// Build the prefix: "a/" + addr(20).
	prefix := make([]byte, len(prefixAddr)+types.AddressSize)
	copy(prefix, prefixAddr)
	copy(prefix[len(prefixAddr):], addr[:])

	var utxos []*UTXO
	err := s.db.ForEach(prefix, func(key, _ []byte) error {
		// Key layout: "a/" + addr(20) + txid(32) + index(4).
		off := len(prefixAddr) + types.AddressSize
		if len(key) < off+types.HashSize+4 {
			return nil // Malformed key, skip.
		}
		var op types.Outpoint
		copy(op.TxID[:], key[off:off+types.HashSize])
		op.Index = binary.BigEndian.Uint32(key[off+types.HashSize:])

		u, err := s.Get(op)
		if err != nil {
			return nil // UTXO may have been spent, skip.
		}
		utxos = append(utxos, u)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan address index: %w", err)
	}
	return utxos, nil
}

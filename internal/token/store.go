package token

import (
	"encoding/json"
	"fmt"

	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

var prefixToken = []byte("t/") // t/<tokenID(32)> -> Metadata JSON

// Store persists token metadata.
type Store struct {
	db storage.DB
}

// NewStore creates a token metadata store.
func NewStore(db storage.DB) *Store {
	return &Store{db: db}
}

// Put stores metadata for a token.
func (s *Store) Put(id types.TokenID, meta *Metadata) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("token marshal: %w", err)
	}
	return s.db.Put(tokenKey(id), data)
}

// Get retrieves metadata for a token.
func (s *Store) Get(id types.TokenID) (*Metadata, error) {
	data, err := s.db.Get(tokenKey(id))
	if err != nil {
		return nil, fmt.Errorf("token get: %w", err)
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("token unmarshal: %w", err)
	}
	return &meta, nil
}

// Has checks if metadata exists for a token.
func (s *Store) Has(id types.TokenID) (bool, error) {
	return s.db.Has(tokenKey(id))
}

// MetadataEntry pairs a token ID with its metadata.
type MetadataEntry struct {
	ID types.TokenID
	Metadata
}

// ForEach iterates over all token metadata entries.
// Return a non-nil error from fn to stop iteration early.
func (s *Store) ForEach(fn func(types.TokenID, *Metadata) error) error {
	return s.db.ForEach(prefixToken, func(key, value []byte) error {
		// Key layout: "t/" + tokenID(32).
		if len(key) < len(prefixToken)+types.HashSize {
			return nil // Malformed key, skip.
		}
		var id types.TokenID
		copy(id[:], key[len(prefixToken):])

		var meta Metadata
		if err := json.Unmarshal(value, &meta); err != nil {
			return nil // Skip corrupt entries.
		}
		return fn(id, &meta)
	})
}

// List returns all token metadata entries.
func (s *Store) List() ([]MetadataEntry, error) {
	var entries []MetadataEntry
	err := s.ForEach(func(id types.TokenID, meta *Metadata) error {
		entries = append(entries, MetadataEntry{ID: id, Metadata: *meta})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []MetadataEntry{}
	}
	return entries, nil
}

func tokenKey(id types.TokenID) []byte {
	key := make([]byte, len(prefixToken)+types.HashSize)
	copy(key, prefixToken)
	copy(key[len(prefixToken):], id[:])
	return key
}

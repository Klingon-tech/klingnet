package p2p

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/libp2p/go-libp2p/core/peer"
)

const banKeyPrefix = "ban/"

// BanRecord is a persisted ban entry.
type BanRecord struct {
	ID        string `json:"id"`         // base58 peer ID
	Reason    string `json:"reason"`     // Why banned
	Score     int    `json:"score"`      // Accumulated score at ban time
	BannedAt  int64  `json:"banned_at"`  // Unix timestamp
	ExpiresAt int64  `json:"expires_at"` // Unix timestamp (0 = permanent)
}

// IsExpired returns true if the ban has a non-zero expiry that has passed.
func (r *BanRecord) IsExpired() bool {
	return r.ExpiresAt > 0 && time.Now().Unix() >= r.ExpiresAt
}

// BanStore persists ban records in a storage.DB under the "ban/" prefix.
type BanStore struct {
	db storage.DB
}

// NewBanStore creates a new BanStore backed by the given DB.
func NewBanStore(db storage.DB) *BanStore {
	return &BanStore{db: db}
}

func banKey(id peer.ID) []byte {
	return []byte(banKeyPrefix + id.String())
}

func banKeyFromString(id string) []byte {
	return []byte(banKeyPrefix + id)
}

// Get retrieves a ban record by peer ID.
func (bs *BanStore) Get(id peer.ID) (*BanRecord, error) {
	data, err := bs.db.Get(banKey(id))
	if err != nil {
		return nil, err
	}
	var rec BanRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("unmarshal ban record: %w", err)
	}
	return &rec, nil
}

// Put persists a ban record.
func (bs *BanStore) Put(rec *BanRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal ban record: %w", err)
	}
	return bs.db.Put(banKeyFromString(rec.ID), data)
}

// Delete removes a ban record.
func (bs *BanStore) Delete(id peer.ID) error {
	return bs.db.Delete(banKey(id))
}

// ForEach iterates over all ban records.
func (bs *BanStore) ForEach(fn func(*BanRecord) error) error {
	return bs.db.ForEach([]byte(banKeyPrefix), func(key, value []byte) error {
		var rec BanRecord
		if err := json.Unmarshal(value, &rec); err != nil {
			return nil // Skip corrupt records.
		}
		return fn(&rec)
	})
}

// PruneExpired removes all expired ban records. Returns the number pruned.
func (bs *BanStore) PruneExpired() (int, error) {
	now := time.Now().Unix()
	var toDelete [][]byte

	err := bs.db.ForEach([]byte(banKeyPrefix), func(key, value []byte) error {
		var rec BanRecord
		if err := json.Unmarshal(value, &rec); err != nil {
			keyCopy := make([]byte, len(key))
			copy(keyCopy, key)
			toDelete = append(toDelete, keyCopy)
			return nil
		}
		if rec.ExpiresAt > 0 && now >= rec.ExpiresAt {
			keyCopy := make([]byte, len(key))
			copy(keyCopy, key)
			toDelete = append(toDelete, keyCopy)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("iterate for prune: %w", err)
	}

	for _, k := range toDelete {
		if err := bs.db.Delete(k); err != nil {
			return 0, fmt.Errorf("delete expired ban: %w", err)
		}
	}
	return len(toDelete), nil
}

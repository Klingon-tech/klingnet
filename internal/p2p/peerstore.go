package p2p

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	peerKeyPrefix     = "peer/"
	staleThreshold    = 24 * time.Hour
	persistInterval   = 5 * time.Minute
	maxPersistedPeers = 500
)

// PeerRecord is a persisted peer entry.
type PeerRecord struct {
	ID       string   `json:"id"`        // base58 peer ID
	Addrs    []string `json:"addrs"`     // multiaddr strings
	LastSeen int64    `json:"last_seen"` // unix timestamp
	Source   string   `json:"source"`    // "dht", "mdns", "seed", "gossip"
}

// PeerStore persists peer records in a storage.DB under the "peer/" prefix.
type PeerStore struct {
	db storage.DB
}

// NewPeerStore creates a new PeerStore backed by the given DB.
func NewPeerStore(db storage.DB) *PeerStore {
	return &PeerStore{db: db}
}

func peerKeyFromString(id string) []byte {
	return []byte(peerKeyPrefix + id)
}

func peerKey(id peer.ID) []byte {
	return peerKeyFromString(id.String())
}

// Save persists a peer record. If the store already has maxPersistedPeers
// records and this is a new peer, the save is silently skipped.
func (ps *PeerStore) Save(rec PeerRecord) error {
	key := peerKeyFromString(rec.ID)

	// Check if this is a new record vs an update.
	exists, err := ps.db.Has(key)
	if err != nil {
		return fmt.Errorf("check peer exists: %w", err)
	}
	if !exists {
		count, err := ps.Count()
		if err != nil {
			return fmt.Errorf("count peers: %w", err)
		}
		if count >= maxPersistedPeers {
			return nil // At capacity, skip new peers.
		}
	}

	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal peer record: %w", err)
	}
	return ps.db.Put(key, data)
}

// Load retrieves a single peer record by ID.
func (ps *PeerStore) Load(id peer.ID) (*PeerRecord, error) {
	data, err := ps.db.Get(peerKey(id))
	if err != nil {
		return nil, fmt.Errorf("get peer record: %w", err)
	}
	var rec PeerRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("unmarshal peer record: %w", err)
	}
	return &rec, nil
}

// LoadAll returns all persisted peer records.
func (ps *PeerStore) LoadAll() ([]PeerRecord, error) {
	var records []PeerRecord
	err := ps.db.ForEach([]byte(peerKeyPrefix), func(key, value []byte) error {
		var rec PeerRecord
		if err := json.Unmarshal(value, &rec); err != nil {
			return nil // Skip corrupt records.
		}
		records = append(records, rec)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("iterate peer records: %w", err)
	}
	return records, nil
}

// Delete removes a peer record.
func (ps *PeerStore) Delete(id peer.ID) error {
	return ps.db.Delete(peerKey(id))
}

// PruneStale removes records older than the given threshold. Returns the number pruned.
func (ps *PeerStore) PruneStale(threshold time.Duration) (int, error) {
	cutoff := time.Now().Add(-threshold).Unix()
	var toDelete [][]byte

	err := ps.db.ForEach([]byte(peerKeyPrefix), func(key, value []byte) error {
		var rec PeerRecord
		if err := json.Unmarshal(value, &rec); err != nil {
			// Corrupt record — prune it too.
			keyCopy := make([]byte, len(key))
			copy(keyCopy, key)
			toDelete = append(toDelete, keyCopy)
			return nil
		}
		if rec.LastSeen < cutoff {
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
		if err := ps.db.Delete(k); err != nil {
			return 0, fmt.Errorf("delete stale peer: %w", err)
		}
	}
	return len(toDelete), nil
}

// Count returns the number of persisted peer records.
func (ps *PeerStore) Count() (int, error) {
	count := 0
	err := ps.db.ForEach([]byte(peerKeyPrefix), func(key, value []byte) error {
		count++
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("count peers: %w", err)
	}
	return count, nil
}

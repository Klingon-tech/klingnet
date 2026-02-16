// Package subchain manages sub-chain registration, spawning, and anchoring.
package subchain

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// DB key prefix for registry persistence.
var prefixRegistry = []byte("r/")

// SubChain holds sub-chain metadata.
type SubChain struct {
	ID             types.ChainID    `json:"id"`
	ParentID       types.ChainID    `json:"parent_id"`
	Name           string           `json:"name"`
	Symbol         string           `json:"symbol"`
	CreatedAt      uint64           `json:"created_at"`      // Root chain height when registered
	RegistrationTx types.Hash       `json:"registration_tx"` // Tx hash that created this chain
	OutputIndex    uint32           `json:"output_index"`
	Registration   RegistrationData `json:"registration"`
}

// Registry tracks registered sub-chains.
type Registry struct {
	chains map[types.ChainID]*SubChain
	mu     sync.RWMutex
}

// NewRegistry creates a new empty sub-chain registry.
func NewRegistry() *Registry {
	return &Registry{
		chains: make(map[types.ChainID]*SubChain),
	}
}

// Register adds a new sub-chain to the registry.
func (r *Registry) Register(sc *SubChain) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.chains[sc.ID]; exists {
		return fmt.Errorf("sub-chain %s already registered", sc.ID)
	}
	r.chains[sc.ID] = sc
	return nil
}

// Get returns a registered sub-chain by ID.
func (r *Registry) Get(id types.ChainID) (*SubChain, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sc, ok := r.chains[id]
	return sc, ok
}

// Unregister removes a sub-chain from the registry.
func (r *Registry) Unregister(id types.ChainID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.chains, id)
}

// Has checks if a sub-chain is registered.
func (r *Registry) Has(id types.ChainID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.chains[id]
	return ok
}

// List returns all registered sub-chains.
func (r *Registry) List() []*SubChain {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*SubChain, 0, len(r.chains))
	for _, sc := range r.chains {
		out = append(out, sc)
	}
	return out
}

// Count returns the number of registered sub-chains.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.chains)
}

// registryKey builds a DB key for a sub-chain entry: "r/" + chainID(32).
func registryKey(id types.ChainID) []byte {
	key := make([]byte, len(prefixRegistry)+types.HashSize)
	copy(key, prefixRegistry)
	copy(key[len(prefixRegistry):], id[:])
	return key
}

// SaveTo persists the registry to the given DB.
func (r *Registry) SaveTo(db storage.DB) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, sc := range r.chains {
		data, err := json.Marshal(sc)
		if err != nil {
			return fmt.Errorf("marshal sub-chain %s: %w", sc.ID, err)
		}
		if err := db.Put(registryKey(sc.ID), data); err != nil {
			return fmt.Errorf("save sub-chain %s: %w", sc.ID, err)
		}
	}
	return nil
}

// DeleteFrom removes a single sub-chain entry from the DB.
func (r *Registry) DeleteFrom(db storage.DB, id types.ChainID) error {
	return db.Delete(registryKey(id))
}

// LoadRegistry loads the registry from the given DB.
func LoadRegistry(db storage.DB) (*Registry, error) {
	reg := NewRegistry()
	err := db.ForEach(prefixRegistry, func(key, value []byte) error {
		var sc SubChain
		if err := json.Unmarshal(value, &sc); err != nil {
			return fmt.Errorf("unmarshal sub-chain: %w", err)
		}
		reg.chains[sc.ID] = &sc
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load registry: %w", err)
	}
	return reg, nil
}

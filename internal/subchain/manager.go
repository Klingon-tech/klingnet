package subchain

import (
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// SyncFilter controls which sub-chains get spawned locally.
// Nil filter means sync all. The filter only affects spawning —
// all registrations are always persisted in the registry.
type SyncFilter struct {
	Mode     config.SubChainSyncMode
	ChainIDs map[types.ChainID]struct{} // populated when Mode == SubChainSyncList
}

// NewSyncFilter creates a SyncFilter from config.
func NewSyncFilter(cfg config.SubChainSyncConfig) *SyncFilter {
	sf := &SyncFilter{Mode: cfg.Mode}
	if cfg.Mode == config.SubChainSyncList && len(cfg.ChainIDs) > 0 {
		sf.ChainIDs = make(map[types.ChainID]struct{}, len(cfg.ChainIDs))
		for _, hexID := range cfg.ChainIDs {
			b, err := hex.DecodeString(hexID)
			if err != nil || len(b) != types.HashSize {
				continue
			}
			var id types.ChainID
			copy(id[:], b)
			sf.ChainIDs[id] = struct{}{}
		}
	}
	return sf
}

// ShouldSync returns true if the given chain should be spawned.
func (sf *SyncFilter) ShouldSync(id types.ChainID) bool {
	if sf == nil {
		return true
	}
	switch sf.Mode {
	case config.SubChainSyncNone:
		return false
	case config.SubChainSyncList:
		_, ok := sf.ChainIDs[id]
		return ok
	default: // "all" or empty
		return true
	}
}

// ManagerConfig holds configuration for creating a Manager.
type ManagerConfig struct {
	ParentDB   storage.DB
	ParentID   types.ChainID
	Rules      *config.SubChainRules
	SyncFilter *SyncFilter // nil = sync all
}

// MineFilter controls which sub-chains should be mined locally.
// Unlike SyncFilter, there is no "all" mode — operators must explicitly
// list chain IDs. Each PoW miner is CPU-intensive, so unlimited mining
// would be catastrophic with thousands of sub-chains.
type MineFilter struct {
	ChainIDs map[types.ChainID]struct{}
}

// NewMineFilter creates a MineFilter from a list of hex chain IDs.
func NewMineFilter(hexIDs []string) *MineFilter {
	mf := &MineFilter{
		ChainIDs: make(map[types.ChainID]struct{}, len(hexIDs)),
	}
	for _, hexID := range hexIDs {
		b, err := hex.DecodeString(hexID)
		if err != nil || len(b) != types.HashSize {
			continue
		}
		var id types.ChainID
		copy(id[:], b)
		mf.ChainIDs[id] = struct{}{}
	}
	return mf
}

// ShouldMine returns true if the given chain ID is in the mine list.
func (mf *MineFilter) ShouldMine(id types.ChainID) bool {
	if mf == nil || len(mf.ChainIDs) == 0 {
		return false
	}
	_, ok := mf.ChainIDs[id]
	return ok
}

// Manager coordinates sub-chain lifecycle: listens for registrations,
// spawns chains, tracks active instances, and provides access.
type Manager struct {
	registry     *Registry
	chains       map[types.ChainID]*SpawnResult
	parentDB     storage.DB
	parentID     types.ChainID
	rules        *config.SubChainRules
	syncFilter   *SyncFilter
	spawnHandler func(types.ChainID, *SpawnResult) // Called after a sub-chain is spawned.
	stopHandler  func(types.ChainID)               // Called before a sub-chain is stopped.
	mu           sync.RWMutex
}

// NewManager creates a new sub-chain manager.
func NewManager(cfg ManagerConfig) (*Manager, error) {
	if cfg.ParentDB == nil {
		return nil, fmt.Errorf("parent DB is nil")
	}
	if cfg.Rules == nil {
		return nil, fmt.Errorf("sub-chain rules are nil")
	}

	return &Manager{
		registry:   NewRegistry(),
		chains:     make(map[types.ChainID]*SpawnResult),
		parentDB:   cfg.ParentDB,
		parentID:   cfg.ParentID,
		rules:      cfg.Rules,
		syncFilter: cfg.SyncFilter,
	}, nil
}

// SetSpawnHandler registers a callback invoked after a sub-chain is spawned.
func (m *Manager) SetSpawnHandler(h func(types.ChainID, *SpawnResult)) {
	m.spawnHandler = h
}

// SetStopHandler registers a callback invoked before a sub-chain is stopped.
func (m *Manager) SetStopHandler(h func(types.ChainID)) {
	m.stopHandler = h
}

// HandleRegistration processes a confirmed ScriptTypeRegister output.
// It parses, validates, registers, and spawns the sub-chain.
// The value parameter is the output's KGX value (burn amount).
func (m *Manager) HandleRegistration(txHash types.Hash, outputIndex uint32, value uint64, scriptData []byte, height uint64) error {
	// Enforce minimum deposit (burn amount).
	if m.rules.MinDeposit > 0 && value < m.rules.MinDeposit {
		return fmt.Errorf("registration burn %d < min deposit %d", value, m.rules.MinDeposit)
	}

	// Parse registration data.
	rd, err := ParseRegistrationData(scriptData)
	if err != nil {
		return fmt.Errorf("parse registration: %w", err)
	}

	// Validate against protocol rules.
	if err := ValidateRegistrationData(rd, m.rules); err != nil {
		return fmt.Errorf("invalid registration: %w", err)
	}

	// Check max sub-chains limit.
	m.mu.RLock()
	count := m.registry.Count()
	m.mu.RUnlock()
	if m.rules.MaxPerParent > 0 && count >= m.rules.MaxPerParent {
		return fmt.Errorf("max sub-chains reached (%d)", m.rules.MaxPerParent)
	}

	// Derive chain ID.
	chainID := DeriveChainID(txHash, outputIndex)

	// Check for duplicate.
	if m.registry.Has(chainID) {
		return fmt.Errorf("sub-chain %s already exists", chainID)
	}

	// Register metadata.
	sc := &SubChain{
		ID:             chainID,
		ParentID:       m.parentID,
		Name:           rd.Name,
		Symbol:         rd.Symbol,
		CreatedAt:      height,
		RegistrationTx: txHash,
		OutputIndex:    outputIndex,
		Registration:   *rd,
	}
	if err := m.registry.Register(sc); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	// Persist registry (always, even if we don't spawn).
	if err := m.registry.SaveTo(m.parentDB); err != nil {
		return fmt.Errorf("persist registry: %w", err)
	}

	// Only spawn if the sync filter allows it.
	if !m.syncFilter.ShouldSync(chainID) {
		return nil
	}

	result, err := Spawn(SpawnConfig{
		ChainID:         chainID,
		Registration:    rd,
		ParentDB:        m.parentDB,
		CreatedAtHeight: height,
	})
	if err != nil {
		return fmt.Errorf("spawn sub-chain: %w", err)
	}

	m.mu.Lock()
	m.chains[chainID] = result
	m.mu.Unlock()

	if m.spawnHandler != nil {
		m.spawnHandler(chainID, result)
	}

	return nil
}

// HandleDeregistration reverses a registration during a reorg.
// It stops the running sub-chain instance, removes it from the registry,
// deletes its PrefixDB data, and removes the registry entry from the DB.
func (m *Manager) HandleDeregistration(txHash types.Hash, outputIndex uint32) error {
	chainID := DeriveChainID(txHash, outputIndex)

	// Notify stop handler before removing (P2P leave, stop miner, etc.).
	if m.stopHandler != nil {
		m.stopHandler(chainID)
	}

	// Remove running instance if spawned.
	m.mu.Lock()
	if sr, ok := m.chains[chainID]; ok {
		// Wipe all sub-chain data from PrefixDB.
		if pdb, ok := sr.DB.(*storage.PrefixDB); ok {
			if err := pdb.DeleteAll(); err != nil {
				m.mu.Unlock()
				return fmt.Errorf("clean sub-chain data %s: %w", chainID, err)
			}
		}
		delete(m.chains, chainID)
	}
	m.mu.Unlock()

	// Remove from registry (memory).
	m.registry.Unregister(chainID)

	// Remove from persistent registry (DB).
	if err := m.registry.DeleteFrom(m.parentDB, chainID); err != nil {
		return fmt.Errorf("delete registry entry %s: %w", chainID, err)
	}

	return nil
}

// GetChain returns the sub-chain instance for the given chain ID.
func (m *Manager) GetChain(id types.ChainID) (*SpawnResult, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.chains[id]
	return r, ok
}

// ListChains returns metadata for all registered sub-chains.
func (m *Manager) ListChains() []*SubChain {
	return m.registry.List()
}

// Count returns the number of registered sub-chains (includes non-synced).
func (m *Manager) Count() int {
	return m.registry.Count()
}

// SyncedCount returns the number of actively synced (spawned) sub-chains.
func (m *Manager) SyncedCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.chains)
}

// RestoreChains re-spawns all previously registered sub-chains from the
// persisted registry. Called during node startup.
func (m *Manager) RestoreChains() error {
	loaded, err := LoadRegistry(m.parentDB)
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}

	for _, sc := range loaded.List() {
		// Always register in memory (so registry is complete).
		m.registry.chains[sc.ID] = sc

		// Only spawn if sync filter allows it.
		if !m.syncFilter.ShouldSync(sc.ID) {
			continue
		}

		result, err := Spawn(SpawnConfig{
			ChainID:         sc.ID,
			Registration:    &sc.Registration,
			ParentDB:        m.parentDB,
			CreatedAtHeight: sc.CreatedAt,
		})
		if err != nil {
			return fmt.Errorf("restore sub-chain %s: %w", sc.ID, err)
		}

		m.mu.Lock()
		m.chains[sc.ID] = result
		m.mu.Unlock()

		if m.spawnHandler != nil {
			m.spawnHandler(sc.ID, result)
		}
	}

	return nil
}

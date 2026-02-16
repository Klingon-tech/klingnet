package subchain

import (
	"encoding/json"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/config"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// testDeposit is the minimum burn amount used in tests (matches testRules().MinDeposit).
const testDeposit = 50 * config.Coin

func newTestManager(t *testing.T) (*Manager, storage.DB) {
	t.Helper()
	db := storage.NewMemory()
	rules := testRules()
	mgr, err := NewManager(ManagerConfig{
		ParentDB: db,
		Rules:    rules,
	})
	if err != nil {
		t.Fatal(err)
	}
	return mgr, db
}

func regData(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(validPoARegistration())
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestManager_HandleRegistration(t *testing.T) {
	mgr, _ := newTestManager(t)
	txHash := types.Hash{1, 2, 3}

	err := mgr.HandleRegistration(txHash, 0, testDeposit, regData(t), 10)
	if err != nil {
		t.Fatalf("HandleRegistration: %v", err)
	}

	// Should have 1 chain.
	if mgr.Count() != 1 {
		t.Fatalf("Count = %d, want 1", mgr.Count())
	}

	// Chain should be accessible.
	chainID := DeriveChainID(txHash, 0)
	r, ok := mgr.GetChain(chainID)
	if !ok {
		t.Fatal("GetChain returned false")
	}
	if r.Chain.Height() != 0 {
		t.Fatalf("sub-chain height = %d, want 0", r.Chain.Height())
	}
}

func TestManager_DuplicateRejected(t *testing.T) {
	mgr, _ := newTestManager(t)
	txHash := types.Hash{1}
	data := regData(t)

	if err := mgr.HandleRegistration(txHash, 0, testDeposit, data, 1); err != nil {
		t.Fatal(err)
	}
	// Same tx + index should fail.
	if err := mgr.HandleRegistration(txHash, 0, testDeposit, data, 2); err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestManager_MaxPerParent(t *testing.T) {
	db := storage.NewMemory()
	rules := testRules()
	rules.MaxPerParent = 1

	mgr, err := NewManager(ManagerConfig{ParentDB: db, Rules: rules})
	if err != nil {
		t.Fatal(err)
	}

	data := regData(t)
	if err := mgr.HandleRegistration(types.Hash{1}, 0, testDeposit, data, 1); err != nil {
		t.Fatal(err)
	}
	// Second registration should fail.
	if err := mgr.HandleRegistration(types.Hash{2}, 0, testDeposit, data, 2); err == nil {
		t.Fatal("expected error when max sub-chains reached")
	}
}

func TestManager_ListChains(t *testing.T) {
	mgr, _ := newTestManager(t)
	data := regData(t)

	mgr.HandleRegistration(types.Hash{1}, 0, testDeposit, data, 1)
	mgr.HandleRegistration(types.Hash{2}, 0, testDeposit, data, 2)

	list := mgr.ListChains()
	if len(list) != 2 {
		t.Fatalf("ListChains = %d, want 2", len(list))
	}
}

func TestManager_RestoreChains(t *testing.T) {
	db := storage.NewMemory()
	rules := testRules()

	// Create manager and register a chain.
	mgr1, err := NewManager(ManagerConfig{ParentDB: db, Rules: rules})
	if err != nil {
		t.Fatal(err)
	}

	txHash := types.Hash{0xAB}
	if err := mgr1.HandleRegistration(txHash, 0, testDeposit, regData(t), 5); err != nil {
		t.Fatal(err)
	}

	chainID := DeriveChainID(txHash, 0)
	r1, _ := mgr1.GetChain(chainID)
	tip1 := r1.Chain.TipHash()

	// Create a new manager and restore.
	mgr2, err := NewManager(ManagerConfig{ParentDB: db, Rules: rules})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr2.RestoreChains(); err != nil {
		t.Fatalf("RestoreChains: %v", err)
	}

	// Should have 1 chain.
	if mgr2.Count() != 1 {
		t.Fatalf("restored Count = %d, want 1", mgr2.Count())
	}

	r2, ok := mgr2.GetChain(chainID)
	if !ok {
		t.Fatal("restored GetChain returned false")
	}

	// Tip should match (same data in same PrefixDB).
	tip2 := r2.Chain.TipHash()
	if tip1 != tip2 {
		t.Fatalf("restored tip %s != original %s", tip2, tip1)
	}
}

func TestManager_InvalidRegistrationData(t *testing.T) {
	mgr, _ := newTestManager(t)
	err := mgr.HandleRegistration(types.Hash{1}, 0, testDeposit, []byte("bad json"), 1)
	if err == nil {
		t.Fatal("expected error for invalid registration data")
	}
}

func TestManager_InsufficientBurn_Rejected(t *testing.T) {
	mgr, _ := newTestManager(t)
	// Try to register with 1 base unit instead of 50 KGX.
	err := mgr.HandleRegistration(types.Hash{0xBB}, 0, 1, regData(t), 1)
	if err == nil {
		t.Fatal("expected error for insufficient burn amount")
	}
	// Should have 0 chains — registration was rejected.
	if mgr.Count() != 0 {
		t.Fatalf("Count = %d, want 0", mgr.Count())
	}
}

func TestNewManager_NilDB(t *testing.T) {
	_, err := NewManager(ManagerConfig{Rules: testRules()})
	if err == nil {
		t.Fatal("expected error for nil DB")
	}
}

func TestNewManager_NilRules(t *testing.T) {
	_, err := NewManager(ManagerConfig{ParentDB: storage.NewMemory()})
	if err == nil {
		t.Fatal("expected error for nil rules")
	}
}

// ── Sync filter tests ──────────────────────────────────────────────────

func TestSyncFilter_NilMeansAll(t *testing.T) {
	var sf *SyncFilter
	if !sf.ShouldSync(types.ChainID{1}) {
		t.Fatal("nil SyncFilter should allow all")
	}
}

func TestSyncFilter_All(t *testing.T) {
	sf := NewSyncFilter(config.SubChainSyncConfig{Mode: config.SubChainSyncAll})
	if !sf.ShouldSync(types.ChainID{1}) {
		t.Fatal("SyncAll should allow any chain")
	}
}

func TestSyncFilter_None(t *testing.T) {
	sf := NewSyncFilter(config.SubChainSyncConfig{Mode: config.SubChainSyncNone})
	if sf.ShouldSync(types.ChainID{1}) {
		t.Fatal("SyncNone should reject all chains")
	}
}

func TestSyncFilter_List(t *testing.T) {
	// Create a known chain ID.
	txHash := types.Hash{0xAA}
	wantID := DeriveChainID(txHash, 0)

	sf := NewSyncFilter(config.SubChainSyncConfig{
		Mode:     config.SubChainSyncList,
		ChainIDs: []string{wantID.String()},
	})

	if !sf.ShouldSync(wantID) {
		t.Fatal("listed chain should be allowed")
	}
	if sf.ShouldSync(types.ChainID{0xFF}) {
		t.Fatal("unlisted chain should be rejected")
	}
}

func TestSyncFilter_ListIgnoresInvalidHex(t *testing.T) {
	sf := NewSyncFilter(config.SubChainSyncConfig{
		Mode:     config.SubChainSyncList,
		ChainIDs: []string{"not-hex", "ab"},
	})
	// Both invalid (wrong length or bad hex), map should be empty.
	if sf.ShouldSync(types.ChainID{}) {
		t.Fatal("empty chain ID should not match invalid list")
	}
}

// ── MineFilter tests ────────────────────────────────────────────────────

func TestMineFilter_NilRejectsAll(t *testing.T) {
	var mf *MineFilter
	if mf.ShouldMine(types.ChainID{1}) {
		t.Fatal("nil MineFilter should reject all")
	}
}

func TestMineFilter_EmptyRejectsAll(t *testing.T) {
	mf := NewMineFilter(nil)
	if mf.ShouldMine(types.ChainID{1}) {
		t.Fatal("empty MineFilter should reject all")
	}
}

func TestMineFilter_MatchesListed(t *testing.T) {
	txHash := types.Hash{0xBB}
	wantID := DeriveChainID(txHash, 0)

	mf := NewMineFilter([]string{wantID.String()})
	if !mf.ShouldMine(wantID) {
		t.Fatal("listed chain should be allowed")
	}
}

func TestMineFilter_RejectsUnlisted(t *testing.T) {
	txHash := types.Hash{0xBB}
	wantID := DeriveChainID(txHash, 0)

	mf := NewMineFilter([]string{wantID.String()})
	if mf.ShouldMine(types.ChainID{0xFF}) {
		t.Fatal("unlisted chain should be rejected")
	}
}

func TestMineFilter_IgnoresInvalidHex(t *testing.T) {
	mf := NewMineFilter([]string{"not-hex", "ab"})
	if mf.ShouldMine(types.ChainID{}) {
		t.Fatal("invalid hex should not match")
	}
}

// ── Spawn/Stop handler tests ─────────────────────────────────────────

func TestManager_SpawnHandler_CalledOnRegistration(t *testing.T) {
	mgr, _ := newTestManager(t)

	var called bool
	var gotID types.ChainID
	mgr.SetSpawnHandler(func(id types.ChainID, sr *SpawnResult) {
		called = true
		gotID = id
	})

	txHash := types.Hash{0xAB}
	if err := mgr.HandleRegistration(txHash, 0, testDeposit, regData(t), 1); err != nil {
		t.Fatal(err)
	}

	if !called {
		t.Fatal("spawn handler should have been called")
	}
	wantID := DeriveChainID(txHash, 0)
	if gotID != wantID {
		t.Fatalf("spawn handler got chain ID %s, want %s", gotID, wantID)
	}
}

func TestManager_StopHandler_CalledOnDeregistration(t *testing.T) {
	mgr, _ := newTestManager(t)

	txHash := types.Hash{0xAC}
	if err := mgr.HandleRegistration(txHash, 0, testDeposit, regData(t), 1); err != nil {
		t.Fatal(err)
	}

	var called bool
	var gotID types.ChainID
	mgr.SetStopHandler(func(id types.ChainID) {
		called = true
		gotID = id
	})

	if err := mgr.HandleDeregistration(txHash, 0); err != nil {
		t.Fatal(err)
	}

	if !called {
		t.Fatal("stop handler should have been called")
	}
	wantID := DeriveChainID(txHash, 0)
	if gotID != wantID {
		t.Fatalf("stop handler got chain ID %s, want %s", gotID, wantID)
	}
}

func TestManager_SpawnHandler_NotCalledWhenFiltered(t *testing.T) {
	db := storage.NewMemory()
	rules := testRules()
	sf := NewSyncFilter(config.SubChainSyncConfig{Mode: config.SubChainSyncNone})
	mgr, _ := NewManager(ManagerConfig{ParentDB: db, Rules: rules, SyncFilter: sf})

	var called bool
	mgr.SetSpawnHandler(func(id types.ChainID, sr *SpawnResult) {
		called = true
	})

	txHash := types.Hash{0xAD}
	if err := mgr.HandleRegistration(txHash, 0, testDeposit, regData(t), 1); err != nil {
		t.Fatal(err)
	}

	if called {
		t.Fatal("spawn handler should NOT be called when sync filter rejects the chain")
	}
}

func TestManager_SyncNone_RegistersButDoesNotSpawn(t *testing.T) {
	db := storage.NewMemory()
	rules := testRules()
	sf := NewSyncFilter(config.SubChainSyncConfig{Mode: config.SubChainSyncNone})

	mgr, err := NewManager(ManagerConfig{
		ParentDB:   db,
		Rules:      rules,
		SyncFilter: sf,
	})
	if err != nil {
		t.Fatal(err)
	}

	txHash := types.Hash{0xCC}
	if err := mgr.HandleRegistration(txHash, 0, testDeposit, regData(t), 1); err != nil {
		t.Fatalf("HandleRegistration: %v", err)
	}

	// Registry should have 1 entry.
	if mgr.Count() != 1 {
		t.Fatalf("Count = %d, want 1", mgr.Count())
	}

	// But no chain should be spawned.
	if mgr.SyncedCount() != 0 {
		t.Fatalf("SyncedCount = %d, want 0", mgr.SyncedCount())
	}

	chainID := DeriveChainID(txHash, 0)
	if _, ok := mgr.GetChain(chainID); ok {
		t.Fatal("GetChain should return false when sync=none")
	}
}

func TestManager_SyncList_OnlySpawnsListed(t *testing.T) {
	db := storage.NewMemory()
	rules := testRules()

	// We'll register two chains, but only list one in the filter.
	tx1 := types.Hash{0x01}
	tx2 := types.Hash{0x02}
	wantID := DeriveChainID(tx1, 0)

	sf := NewSyncFilter(config.SubChainSyncConfig{
		Mode:     config.SubChainSyncList,
		ChainIDs: []string{wantID.String()},
	})

	mgr, err := NewManager(ManagerConfig{
		ParentDB:   db,
		Rules:      rules,
		SyncFilter: sf,
	})
	if err != nil {
		t.Fatal(err)
	}

	data := regData(t)
	if err := mgr.HandleRegistration(tx1, 0, testDeposit, data, 1); err != nil {
		t.Fatal(err)
	}
	if err := mgr.HandleRegistration(tx2, 0, testDeposit, data, 2); err != nil {
		t.Fatal(err)
	}

	// Both should be registered.
	if mgr.Count() != 2 {
		t.Fatalf("Count = %d, want 2", mgr.Count())
	}

	// Only the listed one should be spawned.
	if mgr.SyncedCount() != 1 {
		t.Fatalf("SyncedCount = %d, want 1", mgr.SyncedCount())
	}

	if _, ok := mgr.GetChain(wantID); !ok {
		t.Fatal("listed chain should be spawned")
	}

	skippedID := DeriveChainID(tx2, 0)
	if _, ok := mgr.GetChain(skippedID); ok {
		t.Fatal("unlisted chain should not be spawned")
	}
}

func TestManager_RestoreChains_RespectsFilter(t *testing.T) {
	db := storage.NewMemory()
	rules := testRules()

	// First manager: sync all, register 2 chains.
	mgr1, err := NewManager(ManagerConfig{ParentDB: db, Rules: rules})
	if err != nil {
		t.Fatal(err)
	}

	tx1 := types.Hash{0x10}
	tx2 := types.Hash{0x20}
	data := regData(t)
	mgr1.HandleRegistration(tx1, 0, testDeposit, data, 1)
	mgr1.HandleRegistration(tx2, 0, testDeposit, data, 2)

	// Second manager: sync=none, restore.
	sf := NewSyncFilter(config.SubChainSyncConfig{Mode: config.SubChainSyncNone})
	mgr2, err := NewManager(ManagerConfig{
		ParentDB:   db,
		Rules:      rules,
		SyncFilter: sf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr2.RestoreChains(); err != nil {
		t.Fatal(err)
	}

	// Should see 2 registered, 0 synced.
	if mgr2.Count() != 2 {
		t.Fatalf("Count = %d, want 2", mgr2.Count())
	}
	if mgr2.SyncedCount() != 0 {
		t.Fatalf("SyncedCount = %d, want 0", mgr2.SyncedCount())
	}
}

// ── Deregistration tests ────────────────────────────────────────────────

func TestManager_HandleDeregistration(t *testing.T) {
	mgr, db := newTestManager(t)
	txHash := types.Hash{0xDD}

	// Register a sub-chain.
	if err := mgr.HandleRegistration(txHash, 0, testDeposit, regData(t), 5); err != nil {
		t.Fatalf("HandleRegistration: %v", err)
	}

	chainID := DeriveChainID(txHash, 0)

	// Verify it's running.
	if mgr.Count() != 1 {
		t.Fatalf("Count = %d, want 1", mgr.Count())
	}
	if mgr.SyncedCount() != 1 {
		t.Fatalf("SyncedCount = %d, want 1", mgr.SyncedCount())
	}
	if _, ok := mgr.GetChain(chainID); !ok {
		t.Fatal("chain should be running")
	}

	// Verify registry is persisted.
	loaded, _ := LoadRegistry(db)
	if loaded.Count() != 1 {
		t.Fatalf("persisted Count = %d, want 1", loaded.Count())
	}

	// Deregister it.
	if err := mgr.HandleDeregistration(txHash, 0); err != nil {
		t.Fatalf("HandleDeregistration: %v", err)
	}

	// Should be completely gone.
	if mgr.Count() != 0 {
		t.Fatalf("Count after dereg = %d, want 0", mgr.Count())
	}
	if mgr.SyncedCount() != 0 {
		t.Fatalf("SyncedCount after dereg = %d, want 0", mgr.SyncedCount())
	}
	if _, ok := mgr.GetChain(chainID); ok {
		t.Fatal("chain should not be running after deregistration")
	}

	// Registry persistence should be cleaned.
	loaded2, _ := LoadRegistry(db)
	if loaded2.Count() != 0 {
		t.Fatalf("persisted Count after dereg = %d, want 0", loaded2.Count())
	}
}

func TestManager_HandleDeregistration_CleansData(t *testing.T) {
	db := storage.NewMemory()
	rules := testRules()
	mgr, _ := NewManager(ManagerConfig{ParentDB: db, Rules: rules})

	txHash := types.Hash{0xEE}
	if err := mgr.HandleRegistration(txHash, 0, testDeposit, regData(t), 1); err != nil {
		t.Fatal(err)
	}

	chainID := DeriveChainID(txHash, 0)

	// Verify sub-chain data exists in parent DB via PrefixDB.
	sr, _ := mgr.GetChain(chainID)
	if sr == nil {
		t.Fatal("chain should exist")
	}
	// The sub-chain should have genesis block data stored.
	hasState, _ := sr.DB.Has([]byte("s/tip"))
	if !hasState {
		t.Fatal("sub-chain should have state data before deregistration")
	}

	// Deregister.
	if err := mgr.HandleDeregistration(txHash, 0); err != nil {
		t.Fatal(err)
	}

	// The PrefixDB namespace should be wiped from parent DB.
	// Check by trying to read the state key directly from inner DB.
	prefix := "sc/" + chainID.String() + "/"
	innerKey := prefix + "s/tip"
	has, _ := db.Has([]byte(innerKey))
	if has {
		t.Fatal("sub-chain data should be wiped after deregistration")
	}
}

func TestManager_HandleDeregistration_UnknownChain(t *testing.T) {
	mgr, _ := newTestManager(t)

	// Deregistering a chain that doesn't exist should not error
	// (it's a no-op — the chain may not have been registered on this fork).
	err := mgr.HandleDeregistration(types.Hash{0xFF}, 0)
	if err != nil {
		t.Fatalf("HandleDeregistration of unknown chain should not error: %v", err)
	}
}

func TestManager_RegisterAfterDeregister(t *testing.T) {
	mgr, _ := newTestManager(t)
	txHash := types.Hash{0xAA}
	data := regData(t)

	// Register, deregister, then register again with same tx (simulates reorg replay).
	mgr.HandleRegistration(txHash, 0, testDeposit, data, 1)
	mgr.HandleDeregistration(txHash, 0)

	// Should be able to re-register.
	if err := mgr.HandleRegistration(txHash, 0, testDeposit, data, 1); err != nil {
		t.Fatalf("re-register after deregister should work: %v", err)
	}
	if mgr.Count() != 1 {
		t.Fatalf("Count = %d, want 1", mgr.Count())
	}
}

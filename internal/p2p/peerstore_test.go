package p2p

import (
	"testing"
	"time"

	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/libp2p/go-libp2p/core/peer"
)

func newTestPeerStore() *PeerStore {
	return NewPeerStore(storage.NewMemory())
}

// testPeerID returns a peer.ID whose String() matches the raw string s.
// For test convenience, we use the peer.ID String() output as our canonical ID.
func testPeerID(s string) (peer.ID, string) {
	id := peer.ID(s)
	return id, id.String()
}

func TestPeerStore_SaveLoad(t *testing.T) {
	ps := newTestPeerStore()

	pid, pidStr := testPeerID("peer-1")

	rec := PeerRecord{
		ID:       pidStr,
		Addrs:    []string{"/ip4/192.168.1.1/tcp/4001"},
		LastSeen: time.Now().Unix(),
		Source:   "dht",
	}

	if err := ps.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := ps.Load(pid)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.ID != rec.ID {
		t.Errorf("ID mismatch: got %q, want %q", loaded.ID, rec.ID)
	}
	if len(loaded.Addrs) != 1 || loaded.Addrs[0] != rec.Addrs[0] {
		t.Errorf("Addrs mismatch: got %v, want %v", loaded.Addrs, rec.Addrs)
	}
	if loaded.LastSeen != rec.LastSeen {
		t.Errorf("LastSeen mismatch: got %d, want %d", loaded.LastSeen, rec.LastSeen)
	}
	if loaded.Source != rec.Source {
		t.Errorf("Source mismatch: got %q, want %q", loaded.Source, rec.Source)
	}
}

func TestPeerStore_LoadAll(t *testing.T) {
	ps := newTestPeerStore()
	now := time.Now().Unix()

	for i, raw := range []string{"pa", "pb", "pc"} {
		_, pidStr := testPeerID(raw)
		rec := PeerRecord{
			ID:       pidStr,
			Addrs:    []string{"/ip4/10.0.0.1/tcp/4001"},
			LastSeen: now + int64(i),
			Source:   "seed",
		}
		if err := ps.Save(rec); err != nil {
			t.Fatalf("Save %s: %v", pidStr, err)
		}
	}

	all, err := ps.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 records, got %d", len(all))
	}
}

func TestPeerStore_Delete(t *testing.T) {
	ps := newTestPeerStore()

	pid, pidStr := testPeerID("del-peer")

	rec := PeerRecord{
		ID:       pidStr,
		Addrs:    []string{"/ip4/10.0.0.1/tcp/4001"},
		LastSeen: time.Now().Unix(),
		Source:   "mdns",
	}
	if err := ps.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := ps.Delete(pid); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := ps.Load(pid)
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestPeerStore_PruneStale(t *testing.T) {
	ps := newTestPeerStore()

	_, oldStr := testPeerID("old-peer")
	recentPID, recentStr := testPeerID("recent-peer")

	// Old record (48h ago).
	old := PeerRecord{
		ID:       oldStr,
		Addrs:    []string{"/ip4/10.0.0.1/tcp/4001"},
		LastSeen: time.Now().Add(-48 * time.Hour).Unix(),
		Source:   "dht",
	}
	if err := ps.Save(old); err != nil {
		t.Fatalf("Save old: %v", err)
	}

	// Recent record (1h ago).
	recent := PeerRecord{
		ID:       recentStr,
		Addrs:    []string{"/ip4/10.0.0.2/tcp/4001"},
		LastSeen: time.Now().Add(-1 * time.Hour).Unix(),
		Source:   "dht",
	}
	if err := ps.Save(recent); err != nil {
		t.Fatalf("Save recent: %v", err)
	}

	pruned, err := ps.PruneStale(24 * time.Hour)
	if err != nil {
		t.Fatalf("PruneStale: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}

	count, _ := ps.Count()
	if count != 1 {
		t.Errorf("expected 1 remaining, got %d", count)
	}

	// The recent peer should still be loadable.
	rec, err := ps.Load(recentPID)
	if err != nil {
		t.Fatalf("Load recent after prune: %v", err)
	}
	if rec.ID != recentStr {
		t.Errorf("wrong peer survived prune: %q", rec.ID)
	}
}

func TestPeerStore_Count(t *testing.T) {
	ps := newTestPeerStore()

	count, err := ps.Count()
	if err != nil {
		t.Fatalf("Count empty: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}

	for _, raw := range []string{"a", "b", "c", "d"} {
		_, pidStr := testPeerID(raw)
		ps.Save(PeerRecord{ID: pidStr, LastSeen: time.Now().Unix()})
	}

	count, err = ps.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 4 {
		t.Errorf("expected 4, got %d", count)
	}
}

func TestPeerStore_SaveOverwrite(t *testing.T) {
	ps := newTestPeerStore()

	pid, pidStr := testPeerID("overwrite-peer")

	rec1 := PeerRecord{
		ID:       pidStr,
		Addrs:    []string{"/ip4/10.0.0.1/tcp/4001"},
		LastSeen: 1000,
		Source:   "mdns",
	}
	if err := ps.Save(rec1); err != nil {
		t.Fatalf("Save v1: %v", err)
	}

	rec2 := PeerRecord{
		ID:       pidStr,
		Addrs:    []string{"/ip4/10.0.0.2/tcp/4001", "/ip4/10.0.0.3/tcp/4001"},
		LastSeen: 2000,
		Source:   "dht",
	}
	if err := ps.Save(rec2); err != nil {
		t.Fatalf("Save v2: %v", err)
	}

	loaded, err := ps.Load(pid)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.LastSeen != 2000 {
		t.Errorf("LastSeen not updated: got %d, want 2000", loaded.LastSeen)
	}
	if len(loaded.Addrs) != 2 {
		t.Errorf("Addrs not updated: got %d addrs, want 2", len(loaded.Addrs))
	}
	if loaded.Source != "dht" {
		t.Errorf("Source not updated: got %q, want %q", loaded.Source, "dht")
	}

	// Should still only be 1 record.
	count, _ := ps.Count()
	if count != 1 {
		t.Errorf("expected 1 record after overwrite, got %d", count)
	}
}

func TestPeerStore_Empty(t *testing.T) {
	ps := newTestPeerStore()

	all, err := ps.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll empty: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected 0 records, got %d", len(all))
	}
}

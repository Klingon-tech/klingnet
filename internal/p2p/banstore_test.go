package p2p

import (
	"testing"
	"time"

	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestBanStore_PutGet(t *testing.T) {
	db := storage.NewMemory()
	bs := NewBanStore(db)

	id := peer.ID("test-peer-1")
	rec := &BanRecord{
		ID:        id.String(),
		Reason:    "invalid block",
		Score:     100,
		BannedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
	}

	if err := bs.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := bs.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != rec.ID || got.Reason != rec.Reason || got.Score != rec.Score {
		t.Errorf("record mismatch: got %+v, want %+v", got, rec)
	}
}

func TestBanStore_GetNotFound(t *testing.T) {
	db := storage.NewMemory()
	bs := NewBanStore(db)

	_, err := bs.Get(peer.ID("nonexistent"))
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
}

func TestBanStore_Delete(t *testing.T) {
	db := storage.NewMemory()
	bs := NewBanStore(db)

	id := peer.ID("test-peer-2")
	rec := &BanRecord{
		ID:       id.String(),
		Reason:   "spam",
		BannedAt: time.Now().Unix(),
	}

	if err := bs.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := bs.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := bs.Get(id)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestBanStore_ForEach(t *testing.T) {
	db := storage.NewMemory()
	bs := NewBanStore(db)

	for i := 0; i < 3; i++ {
		id := peer.ID("peer-" + string(rune('a'+i)))
		rec := &BanRecord{
			ID:       id.String(),
			Reason:   "test",
			BannedAt: time.Now().Unix(),
		}
		if err := bs.Put(rec); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	var count int
	err := bs.ForEach(func(rec *BanRecord) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("ForEach: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 records, got %d", count)
	}
}

func TestBanStore_PruneExpired(t *testing.T) {
	db := storage.NewMemory()
	bs := NewBanStore(db)

	now := time.Now().Unix()

	// Expired ban.
	expired := &BanRecord{
		ID:        peer.ID("expired-peer").String(),
		Reason:    "old",
		BannedAt:  now - 3600,
		ExpiresAt: now - 1, // Already expired.
	}

	// Active ban.
	active := &BanRecord{
		ID:        peer.ID("active-peer").String(),
		Reason:    "bad",
		BannedAt:  now,
		ExpiresAt: now + 3600, // Expires in 1 hour.
	}

	// Permanent ban (ExpiresAt = 0).
	permanent := &BanRecord{
		ID:        peer.ID("perm-peer").String(),
		Reason:    "very bad",
		BannedAt:  now - 7200,
		ExpiresAt: 0,
	}

	for _, rec := range []*BanRecord{expired, active, permanent} {
		if err := bs.Put(rec); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	pruned, err := bs.PruneExpired()
	if err != nil {
		t.Fatalf("PruneExpired: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}

	// Active and permanent should still exist.
	var remaining int
	bs.ForEach(func(rec *BanRecord) error {
		remaining++
		return nil
	})
	if remaining != 2 {
		t.Errorf("expected 2 remaining, got %d", remaining)
	}
}

func TestBanRecord_IsExpired(t *testing.T) {
	now := time.Now().Unix()

	tests := []struct {
		name    string
		rec     BanRecord
		expired bool
	}{
		{"permanent", BanRecord{ExpiresAt: 0}, false},
		{"future", BanRecord{ExpiresAt: now + 3600}, false},
		{"past", BanRecord{ExpiresAt: now - 1}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rec.IsExpired(); got != tt.expired {
				t.Errorf("IsExpired() = %v, want %v", got, tt.expired)
			}
		})
	}
}

package consensus

import (
	"testing"
	"time"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
)

func TestNewValidatorTracker(t *testing.T) {
	tr := NewValidatorTracker(60 * time.Second)
	if tr == nil {
		t.Fatal("NewValidatorTracker returned nil")
	}
	if tr.HeartbeatInterval() != 60*time.Second {
		t.Errorf("interval = %v, want 60s", tr.HeartbeatInterval())
	}
}

func TestValidatorTracker_RecordHeartbeat(t *testing.T) {
	tr := NewValidatorTracker(60 * time.Second)
	key, _ := crypto.GenerateKey()
	pub := key.PublicKey()

	tr.RecordHeartbeat(pub)

	s := tr.GetStats(pub)
	if s == nil {
		t.Fatal("GetStats returned nil after RecordHeartbeat")
	}
	if s.LastHeartbeat.IsZero() {
		t.Error("LastHeartbeat should be set")
	}
	if !tr.IsOnline(pub) {
		t.Error("validator should be online after heartbeat")
	}
}

func TestValidatorTracker_RecordBlock(t *testing.T) {
	tr := NewValidatorTracker(60 * time.Second)
	key, _ := crypto.GenerateKey()
	pub := key.PublicKey()

	tr.RecordBlock(pub)
	tr.RecordBlock(pub)
	tr.RecordBlock(pub)

	s := tr.GetStats(pub)
	if s == nil {
		t.Fatal("GetStats returned nil")
	}
	if s.BlockCount != 3 {
		t.Errorf("BlockCount = %d, want 3", s.BlockCount)
	}
	if s.LastBlock.IsZero() {
		t.Error("LastBlock should be set")
	}
}

func TestValidatorTracker_RecordMiss(t *testing.T) {
	tr := NewValidatorTracker(60 * time.Second)
	key, _ := crypto.GenerateKey()
	pub := key.PublicKey()

	tr.RecordMiss(pub)
	tr.RecordMiss(pub)

	s := tr.GetStats(pub)
	if s == nil {
		t.Fatal("GetStats returned nil")
	}
	if s.MissedCount != 2 {
		t.Errorf("MissedCount = %d, want 2", s.MissedCount)
	}
}

func TestValidatorTracker_IsOnline_NoHeartbeat(t *testing.T) {
	tr := NewValidatorTracker(60 * time.Second)
	key, _ := crypto.GenerateKey()
	pub := key.PublicKey()

	if tr.IsOnline(pub) {
		t.Error("should not be online without any heartbeat")
	}

	// Record a block but not a heartbeat — should still be offline.
	tr.RecordBlock(pub)
	if tr.IsOnline(pub) {
		t.Error("should not be online without heartbeat (only block)")
	}
}

func TestValidatorTracker_GetStats_NotTracked(t *testing.T) {
	tr := NewValidatorTracker(60 * time.Second)
	key, _ := crypto.GenerateKey()

	s := tr.GetStats(key.PublicKey())
	if s != nil {
		t.Error("GetStats should return nil for untracked validator")
	}
}

func TestValidatorTracker_GetAllStats(t *testing.T) {
	tr := NewValidatorTracker(60 * time.Second)

	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	tr.RecordHeartbeat(key1.PublicKey())
	tr.RecordBlock(key2.PublicKey())

	all := tr.GetAllStats()
	if len(all) != 2 {
		t.Errorf("GetAllStats count = %d, want 2", len(all))
	}
}

func TestValidatorTracker_GetStats_ReturnsCopy(t *testing.T) {
	tr := NewValidatorTracker(60 * time.Second)
	key, _ := crypto.GenerateKey()
	pub := key.PublicKey()

	tr.RecordBlock(pub)

	s1 := tr.GetStats(pub)
	s1.BlockCount = 999 // Modify the copy.

	s2 := tr.GetStats(pub)
	if s2.BlockCount == 999 {
		t.Error("GetStats should return a copy, not a reference")
	}
}

func TestValidatorTracker_MultipleValidators(t *testing.T) {
	tr := NewValidatorTracker(60 * time.Second)

	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	tr.RecordBlock(key1.PublicKey())
	tr.RecordBlock(key1.PublicKey())
	tr.RecordBlock(key2.PublicKey())
	tr.RecordMiss(key2.PublicKey())

	s1 := tr.GetStats(key1.PublicKey())
	s2 := tr.GetStats(key2.PublicKey())

	if s1.BlockCount != 2 {
		t.Errorf("key1 BlockCount = %d, want 2", s1.BlockCount)
	}
	if s2.BlockCount != 1 {
		t.Errorf("key2 BlockCount = %d, want 1", s2.BlockCount)
	}
	if s2.MissedCount != 1 {
		t.Errorf("key2 MissedCount = %d, want 1", s2.MissedCount)
	}
}

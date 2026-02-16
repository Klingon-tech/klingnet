package consensus

import (
	"encoding/hex"
	"sync"
	"time"
)

// ValidatorStats holds in-memory liveness statistics for a single validator.
// Stats reset on node restart (no persistence).
type ValidatorStats struct {
	PubKey        []byte    // 33-byte compressed public key
	LastHeartbeat time.Time // zero if never seen
	LastBlock     time.Time // zero if never produced
	BlockCount    uint64    // blocks produced since tracker started
	MissedCount   uint64    // times this validator was selected but didn't produce
}

// ValidatorTracker tracks validator liveness via heartbeats and block production.
// All data is in-memory only — no consensus impact, resets on restart.
type ValidatorTracker struct {
	mu                sync.RWMutex
	stats             map[string]*ValidatorStats // hex(pubkey) → stats
	heartbeatInterval time.Duration
}

// NewValidatorTracker creates a tracker with the expected heartbeat interval.
func NewValidatorTracker(heartbeatInterval time.Duration) *ValidatorTracker {
	return &ValidatorTracker{
		stats:             make(map[string]*ValidatorStats),
		heartbeatInterval: heartbeatInterval,
	}
}

// RecordHeartbeat records a heartbeat from the given validator.
func (t *ValidatorTracker) RecordHeartbeat(pubKey []byte) {
	key := hex.EncodeToString(pubKey)
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.getOrCreate(key, pubKey)
	s.LastHeartbeat = time.Now()
}

// RecordBlock records that a validator produced a block.
func (t *ValidatorTracker) RecordBlock(signerPubKey []byte) {
	key := hex.EncodeToString(signerPubKey)
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.getOrCreate(key, signerPubKey)
	s.LastBlock = time.Now()
	s.BlockCount++
}

// RecordMiss records that a validator was selected but did not produce in time.
func (t *ValidatorTracker) RecordMiss(selectedPubKey []byte) {
	key := hex.EncodeToString(selectedPubKey)
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.getOrCreate(key, selectedPubKey)
	s.MissedCount++
}

// IsOnline returns true if the validator's last heartbeat is within 2x the expected interval.
func (t *ValidatorTracker) IsOnline(pubKey []byte) bool {
	key := hex.EncodeToString(pubKey)
	t.mu.RLock()
	defer t.mu.RUnlock()

	s, ok := t.stats[key]
	if !ok || s.LastHeartbeat.IsZero() {
		return false
	}
	return time.Since(s.LastHeartbeat) <= 2*t.heartbeatInterval
}

// GetStats returns a copy of stats for a specific validator, or nil if not tracked.
func (t *ValidatorTracker) GetStats(pubKey []byte) *ValidatorStats {
	key := hex.EncodeToString(pubKey)
	t.mu.RLock()
	defer t.mu.RUnlock()

	s, ok := t.stats[key]
	if !ok {
		return nil
	}
	cp := *s
	cp.PubKey = make([]byte, len(s.PubKey))
	copy(cp.PubKey, s.PubKey)
	return &cp
}

// GetAllStats returns copies of all tracked validator stats.
func (t *ValidatorTracker) GetAllStats() []*ValidatorStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make([]*ValidatorStats, 0, len(t.stats))
	for _, s := range t.stats {
		cp := *s
		cp.PubKey = make([]byte, len(s.PubKey))
		copy(cp.PubKey, s.PubKey)
		out = append(out, &cp)
	}
	return out
}

// HeartbeatInterval returns the configured heartbeat interval.
func (t *ValidatorTracker) HeartbeatInterval() time.Duration {
	return t.heartbeatInterval
}

func (t *ValidatorTracker) getOrCreate(key string, pubKey []byte) *ValidatorStats {
	s, ok := t.stats[key]
	if !ok {
		pk := make([]byte, len(pubKey))
		copy(pk, pubKey)
		s = &ValidatorStats{PubKey: pk}
		t.stats[key] = s
	}
	return s
}

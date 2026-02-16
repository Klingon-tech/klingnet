package p2p

import (
	"sync"
	"time"

	klog "github.com/Klingon-tech/klingnet-chain/internal/log"
	"github.com/libp2p/go-libp2p/core/peer"
)

// Ban thresholds and durations.
const (
	BanThreshold = 100 // Score at which a peer gets banned.
	BanDuration  = 24 * time.Hour
)

// Penalty values for different offenses.
const (
	PenaltyInvalidBlock  = 50  // Bad sig, consensus fail.
	PenaltyInvalidTx     = 20  // Validation failure.
	PenaltyHandshakeFail = 100 // Instant ban (genesis mismatch).
)

// BanManager tracks peer offense scores and manages bans.
type BanManager struct {
	mu     sync.RWMutex
	scores map[peer.ID]int        // In-memory scores.
	bans   map[peer.ID]*BanRecord // In-memory ban cache.
	store  *BanStore              // Persistence (nil for tests).
	node   *Node                  // For DisconnectPeer (nil in unit tests).
}

// NewBanManager creates a new BanManager.
// store may be nil to disable persistence (useful for tests).
// node may be nil if disconnect-on-ban is not needed.
func NewBanManager(store *BanStore, node *Node) *BanManager {
	return &BanManager{
		scores: make(map[peer.ID]int),
		bans:   make(map[peer.ID]*BanRecord),
		store:  store,
		node:   node,
	}
}

// LoadBans restores persisted bans from the store into the in-memory cache.
func (bm *BanManager) LoadBans() {
	if bm.store == nil {
		return
	}

	// Prune expired bans first.
	bm.store.PruneExpired()

	bm.mu.Lock()
	defer bm.mu.Unlock()

	bm.store.ForEach(func(rec *BanRecord) error {
		if !rec.IsExpired() {
			id, err := peer.Decode(rec.ID)
			if err != nil {
				return nil
			}
			bm.bans[id] = rec
		}
		return nil
	})
}

// RecordOffense adds a penalty score to a peer. If the cumulative score
// reaches BanThreshold, the peer is banned and disconnected.
func (bm *BanManager) RecordOffense(id peer.ID, penalty int, reason string) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Already banned — nothing to do.
	if rec, ok := bm.bans[id]; ok && !rec.IsExpired() {
		return
	}

	bm.scores[id] += penalty
	if bm.scores[id] < BanThreshold {
		return
	}

	// Ban the peer.
	now := time.Now()
	rec := &BanRecord{
		ID:        id.String(),
		Reason:    reason,
		Score:     bm.scores[id],
		BannedAt:  now.Unix(),
		ExpiresAt: now.Add(BanDuration).Unix(),
	}
	bm.bans[id] = rec
	delete(bm.scores, id) // Clear score, ban is active.

	// Persist.
	if bm.store != nil {
		bm.store.Put(rec)
	}

	logger := klog.WithComponent("banmgr")
	peerStr := id.String()
	if len(peerStr) > 16 {
		peerStr = peerStr[:16]
	}
	logger.Warn().
		Str("peer", peerStr).
		Str("reason", reason).
		Int("score", rec.Score).
		Msg("Peer banned")

	// Disconnect.
	if bm.node != nil {
		go bm.node.DisconnectPeer(id)
	}
}

// IsBanned returns true if the peer is currently banned.
func (bm *BanManager) IsBanned(id peer.ID) bool {
	bm.mu.RLock()
	rec, ok := bm.bans[id]
	bm.mu.RUnlock()

	if !ok {
		return false
	}

	if rec.IsExpired() {
		// Clean up expired ban.
		bm.mu.Lock()
		delete(bm.bans, id)
		bm.mu.Unlock()
		if bm.store != nil {
			bm.store.Delete(id)
		}
		return false
	}

	return true
}

// Unban manually removes a ban.
func (bm *BanManager) Unban(id peer.ID) {
	bm.mu.Lock()
	delete(bm.bans, id)
	delete(bm.scores, id)
	bm.mu.Unlock()

	if bm.store != nil {
		bm.store.Delete(id)
	}
}

// BanList returns a snapshot of all active bans.
func (bm *BanManager) BanList() []BanRecord {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	var list []BanRecord
	for _, rec := range bm.bans {
		if !rec.IsExpired() {
			list = append(list, *rec)
		}
	}
	return list
}

// RunPruneLoop periodically prunes expired bans.
// Call in a goroutine. Stops when done channel is closed.
func (bm *BanManager) RunPruneLoop(done <-chan struct{}) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			bm.pruneExpired()
		}
	}
}

func (bm *BanManager) pruneExpired() {
	bm.mu.Lock()
	var expired []peer.ID
	for id, rec := range bm.bans {
		if rec.IsExpired() {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		delete(bm.bans, id)
	}
	bm.mu.Unlock()

	if bm.store != nil {
		bm.store.PruneExpired()
	}
}

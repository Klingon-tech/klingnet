package p2p

import (
	"crypto/rand"
	"testing"

	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestBanManager_ScoreAccumulation(t *testing.T) {
	bm := NewBanManager(nil, nil)

	id := peer.ID("test-peer")

	// 20 points should not trigger ban.
	bm.RecordOffense(id, PenaltyInvalidTx, "bad tx 1")
	if bm.IsBanned(id) {
		t.Error("peer should not be banned after 20 points")
	}

	// Another 20 points (total 40) — still not banned.
	bm.RecordOffense(id, PenaltyInvalidTx, "bad tx 2")
	if bm.IsBanned(id) {
		t.Error("peer should not be banned after 40 points")
	}
}

func TestBanManager_ThresholdBan(t *testing.T) {
	bm := NewBanManager(nil, nil)

	id := peer.ID("test-peer")

	// 50 + 50 = 100 = BanThreshold → banned.
	bm.RecordOffense(id, PenaltyInvalidBlock, "bad block 1")
	bm.RecordOffense(id, PenaltyInvalidBlock, "bad block 2")

	if !bm.IsBanned(id) {
		t.Error("peer should be banned at threshold")
	}
}

func TestBanManager_InstantBan(t *testing.T) {
	bm := NewBanManager(nil, nil)

	id := peer.ID("test-peer")

	// 100 points in one shot = instant ban.
	bm.RecordOffense(id, PenaltyHandshakeFail, "genesis mismatch")

	if !bm.IsBanned(id) {
		t.Error("peer should be banned after handshake fail")
	}
}

func TestBanManager_IsBanned_NotBanned(t *testing.T) {
	bm := NewBanManager(nil, nil)

	if bm.IsBanned(peer.ID("unknown")) {
		t.Error("unknown peer should not be banned")
	}
}

func TestBanManager_Unban(t *testing.T) {
	bm := NewBanManager(nil, nil)

	id := peer.ID("test-peer")
	bm.RecordOffense(id, PenaltyHandshakeFail, "bad handshake")

	if !bm.IsBanned(id) {
		t.Fatal("peer should be banned")
	}

	bm.Unban(id)
	if bm.IsBanned(id) {
		t.Error("peer should not be banned after Unban")
	}
}

func TestBanManager_BanList(t *testing.T) {
	bm := NewBanManager(nil, nil)

	bm.RecordOffense(peer.ID("peer-a"), PenaltyHandshakeFail, "bad")
	bm.RecordOffense(peer.ID("peer-b"), PenaltyHandshakeFail, "bad")

	list := bm.BanList()
	if len(list) != 2 {
		t.Errorf("expected 2 bans, got %d", len(list))
	}
}

func TestBanManager_Persistence(t *testing.T) {
	db := storage.NewMemory()
	store := NewBanStore(db)
	bm := NewBanManager(store, nil)

	// Use a real peer ID so that String()/Decode() roundtrips correctly.
	id := generateTestPeerID(t)
	bm.RecordOffense(id, PenaltyHandshakeFail, "genesis mismatch")

	if !bm.IsBanned(id) {
		t.Fatal("peer should be banned")
	}

	// Create a new BanManager from the same store.
	bm2 := NewBanManager(store, nil)
	bm2.LoadBans()

	if !bm2.IsBanned(id) {
		t.Error("ban should survive reload from store")
	}
}

func generateTestPeerID(t *testing.T) peer.ID {
	t.Helper()
	priv, _, err := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	id, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		t.Fatalf("peer id from key: %v", err)
	}
	return id
}

func TestBanManager_DuplicateOffense_AlreadyBanned(t *testing.T) {
	bm := NewBanManager(nil, nil)

	id := peer.ID("test-peer")
	bm.RecordOffense(id, PenaltyHandshakeFail, "bad handshake")

	// Recording another offense on a banned peer should be a no-op.
	bm.RecordOffense(id, PenaltyInvalidBlock, "bad block")

	list := bm.BanList()
	if len(list) != 1 {
		t.Errorf("expected 1 ban, got %d", len(list))
	}
}

func TestBanManager_MultiPeer(t *testing.T) {
	bm := NewBanManager(nil, nil)

	// Peer A gets banned, peer B doesn't.
	bm.RecordOffense(peer.ID("a"), PenaltyHandshakeFail, "bad")
	bm.RecordOffense(peer.ID("b"), PenaltyInvalidTx, "bad tx")

	if !bm.IsBanned(peer.ID("a")) {
		t.Error("peer a should be banned")
	}
	if bm.IsBanned(peer.ID("b")) {
		t.Error("peer b should not be banned")
	}
}

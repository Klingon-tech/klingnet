package p2p

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestBanGater_InterceptPeerDial_Allowed(t *testing.T) {
	bm := NewBanManager(nil, nil)
	g := &banGater{banMgr: bm}

	if !g.InterceptPeerDial(peer.ID("good-peer")) {
		t.Error("should allow non-banned peer")
	}
}

func TestBanGater_InterceptPeerDial_Banned(t *testing.T) {
	bm := NewBanManager(nil, nil)
	g := &banGater{banMgr: bm}

	id := peer.ID("bad-peer")
	bm.RecordOffense(id, PenaltyHandshakeFail, "genesis mismatch")

	if g.InterceptPeerDial(id) {
		t.Error("should reject banned peer dial")
	}
}

func TestBanGater_InterceptSecured_Banned(t *testing.T) {
	bm := NewBanManager(nil, nil)
	g := &banGater{banMgr: bm}

	id := peer.ID("bad-peer")
	bm.RecordOffense(id, PenaltyHandshakeFail, "bad")

	if g.InterceptSecured(0, id, nil) {
		t.Error("should reject banned peer on secured connection")
	}
}

func TestBanGater_InterceptSecured_Allowed(t *testing.T) {
	bm := NewBanManager(nil, nil)
	g := &banGater{banMgr: bm}

	if !g.InterceptSecured(0, peer.ID("good-peer"), nil) {
		t.Error("should allow non-banned peer on secured connection")
	}
}

func TestBanGater_InterceptAccept_AlwaysAllows(t *testing.T) {
	bm := NewBanManager(nil, nil)
	g := &banGater{banMgr: bm}

	// Accept always allows because peer identity is not known yet.
	if !g.InterceptAccept(nil) {
		t.Error("InterceptAccept should always allow")
	}
}

func TestBanGater_InterceptUpgraded_AlwaysAllows(t *testing.T) {
	bm := NewBanManager(nil, nil)
	g := &banGater{banMgr: bm}

	allow, reason := g.InterceptUpgraded(nil)
	if !allow {
		t.Error("InterceptUpgraded should always allow")
	}
	if reason != 0 {
		t.Errorf("expected reason 0, got %d", reason)
	}
}

func TestBanGater_AfterUnban(t *testing.T) {
	bm := NewBanManager(nil, nil)
	g := &banGater{banMgr: bm}

	id := peer.ID("temp-banned")
	bm.RecordOffense(id, PenaltyHandshakeFail, "bad")

	if g.InterceptPeerDial(id) {
		t.Error("should reject banned peer")
	}

	bm.Unban(id)
	if !g.InterceptPeerDial(id) {
		t.Error("should allow after unban")
	}
}

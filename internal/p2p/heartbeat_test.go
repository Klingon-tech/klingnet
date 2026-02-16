package p2p

import (
	"testing"
	"time"

	"github.com/Klingon-tech/klingnet-chain/pkg/crypto"
)

func TestHeartbeatSigningBytes(t *testing.T) {
	pubKey := make([]byte, 33)
	pubKey[0] = 0x02
	pubKey[1] = 0xAA

	height := uint64(42)
	ts := int64(1700000000)

	b1 := HeartbeatSigningBytes(pubKey, height, ts)
	b2 := HeartbeatSigningBytes(pubKey, height, ts)

	if len(b1) != 33+8+8 {
		t.Errorf("signing bytes length = %d, want %d", len(b1), 33+8+8)
	}

	// Deterministic.
	for i := range b1 {
		if b1[i] != b2[i] {
			t.Fatal("signing bytes should be deterministic")
		}
	}

	// Different height produces different bytes.
	b3 := HeartbeatSigningBytes(pubKey, 43, ts)
	same := true
	for i := range b1 {
		if b1[i] != b3[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different heights should produce different signing bytes")
	}
}

func TestVerifyHeartbeat_Valid(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	height := uint64(100)
	ts := time.Now().Unix()

	data := HeartbeatSigningBytes(key.PublicKey(), height, ts)
	hash := crypto.Hash(data)
	sig, err := key.Sign(hash[:])
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	msg := &HeartbeatMessage{
		PubKey:    key.PublicKey(),
		Height:    height,
		Timestamp: ts,
		Signature: sig,
	}

	if !VerifyHeartbeat(msg) {
		t.Error("VerifyHeartbeat should return true for valid message")
	}
}

func TestVerifyHeartbeat_InvalidSignature(t *testing.T) {
	key, _ := crypto.GenerateKey()

	msg := &HeartbeatMessage{
		PubKey:    key.PublicKey(),
		Height:    100,
		Timestamp: time.Now().Unix(),
		Signature: []byte("garbage"),
	}

	if VerifyHeartbeat(msg) {
		t.Error("VerifyHeartbeat should return false for invalid signature")
	}
}

func TestVerifyHeartbeat_WrongKey(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	height := uint64(100)
	ts := time.Now().Unix()

	// Sign with key1 but claim pubkey is key2.
	data := HeartbeatSigningBytes(key2.PublicKey(), height, ts)
	hash := crypto.Hash(data)
	sig, _ := key1.Sign(hash[:])

	msg := &HeartbeatMessage{
		PubKey:    key2.PublicKey(),
		Height:    height,
		Timestamp: ts,
		Signature: sig,
	}

	if VerifyHeartbeat(msg) {
		t.Error("VerifyHeartbeat should return false for wrong key")
	}
}

func TestVerifyHeartbeat_EmptyPubKey(t *testing.T) {
	msg := &HeartbeatMessage{
		PubKey:    nil,
		Height:    100,
		Timestamp: time.Now().Unix(),
		Signature: []byte("sig"),
	}
	if VerifyHeartbeat(msg) {
		t.Error("VerifyHeartbeat should return false for empty pubkey")
	}
}

func TestVerifyHeartbeat_EmptySignature(t *testing.T) {
	key, _ := crypto.GenerateKey()
	msg := &HeartbeatMessage{
		PubKey:    key.PublicKey(),
		Height:    100,
		Timestamp: time.Now().Unix(),
		Signature: nil,
	}
	if VerifyHeartbeat(msg) {
		t.Error("VerifyHeartbeat should return false for empty signature")
	}
}

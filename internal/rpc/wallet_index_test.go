package rpc

import (
	"testing"

	"github.com/Klingon-tech/klingnet-chain/internal/storage"
)

func newTestIndex() *WalletTxIndex {
	return NewWalletTxIndex(storage.NewMemory())
}

func TestWalletTxIndex_PutAndQuery(t *testing.T) {
	idx := newTestIndex()

	entries := []TxHistoryEntry{
		{TxHash: "aaa", Type: "mined", Amount: "1.000000000000", Height: 0},
		{TxHash: "bbb", Type: "received", Amount: "2.000000000000", Height: 0},
	}

	if err := idx.PutEntries("w1", "root", 0, entries); err != nil {
		t.Fatalf("put entries: %v", err)
	}

	// Update metadata manually.
	meta := indexMeta{LastHeight: 0, Count: 2}
	if err := idx.setMeta("w1", "root", meta); err != nil {
		t.Fatalf("set meta: %v", err)
	}

	// Query all.
	result, total, err := idx.Query("w1", "root", 50, 0)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(result) != 2 {
		t.Errorf("entries = %d, want 2", len(result))
	}
}

func TestWalletTxIndex_Ordering(t *testing.T) {
	idx := newTestIndex()

	// Insert entries at different heights.
	if err := idx.PutEntries("w1", "root", 0, []TxHistoryEntry{
		{TxHash: "genesis", Type: "mined", Height: 0},
	}); err != nil {
		t.Fatal(err)
	}
	if err := idx.PutEntries("w1", "root", 5, []TxHistoryEntry{
		{TxHash: "block5", Type: "mined", Height: 5},
	}); err != nil {
		t.Fatal(err)
	}
	if err := idx.PutEntries("w1", "root", 10, []TxHistoryEntry{
		{TxHash: "block10", Type: "sent", Height: 10},
	}); err != nil {
		t.Fatal(err)
	}

	result, total, err := idx.Query("w1", "root", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}

	// Should be newest first (height 10, 5, 0).
	if result[0].TxHash != "block10" {
		t.Errorf("first entry = %s, want block10", result[0].TxHash)
	}
	if result[1].TxHash != "block5" {
		t.Errorf("second entry = %s, want block5", result[1].TxHash)
	}
	if result[2].TxHash != "genesis" {
		t.Errorf("third entry = %s, want genesis", result[2].TxHash)
	}
}

func TestWalletTxIndex_Pagination(t *testing.T) {
	idx := newTestIndex()

	// Insert 5 entries at different heights.
	for h := uint64(0); h < 5; h++ {
		if err := idx.PutEntries("w1", "root", h, []TxHistoryEntry{
			{TxHash: "tx" + string(rune('A'+h)), Type: "mined", Height: h},
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Page 1: limit=2, offset=0.
	page1, total, err := idx.Query("w1", "root", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(page1) != 2 {
		t.Errorf("page1 len = %d, want 2", len(page1))
	}

	// Page 2: limit=2, offset=2.
	page2, total2, err := idx.Query("w1", "root", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total2 != 5 {
		t.Errorf("total changed: %d vs %d", total, total2)
	}
	if len(page2) != 2 {
		t.Errorf("page2 len = %d, want 2", len(page2))
	}

	// Page 3: limit=2, offset=4 (only 1 remaining).
	page3, _, err := idx.Query("w1", "root", 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(page3) != 1 {
		t.Errorf("page3 len = %d, want 1", len(page3))
	}

	// Beyond end.
	page4, _, err := idx.Query("w1", "root", 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page4) != 0 {
		t.Errorf("page4 len = %d, want 0", len(page4))
	}
}

func TestWalletTxIndex_DeleteAbove(t *testing.T) {
	idx := newTestIndex()

	// Insert entries at heights 0, 5, 10.
	for _, h := range []uint64{0, 5, 10} {
		if err := idx.PutEntries("w1", "root", h, []TxHistoryEntry{
			{TxHash: "tx", Type: "mined", Height: h},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := idx.setMeta("w1", "root", indexMeta{LastHeight: 10, Count: 3}); err != nil {
		t.Fatal(err)
	}

	// Delete above height 5 (should remove height 10).
	if err := idx.DeleteAbove("w1", "root", 5); err != nil {
		t.Fatalf("delete above: %v", err)
	}

	result, total, err := idx.Query("w1", "root", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}

	// Verify remaining entries are at heights 0 and 5.
	for _, e := range result {
		if e.Height > 5 {
			t.Errorf("entry at height %d should have been deleted", e.Height)
		}
	}

	// Verify metadata was updated.
	meta, _ := idx.GetMeta("w1", "root")
	if meta.LastHeight != 5 {
		t.Errorf("meta.LastHeight = %d, want 5", meta.LastHeight)
	}
	if meta.Count != 2 {
		t.Errorf("meta.Count = %d, want 2", meta.Count)
	}
}

func TestWalletTxIndex_ClearWallet(t *testing.T) {
	idx := newTestIndex()

	if err := idx.PutEntries("w1", "root", 0, []TxHistoryEntry{
		{TxHash: "tx1", Type: "mined"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := idx.setMeta("w1", "root", indexMeta{LastHeight: 0, Count: 1}); err != nil {
		t.Fatal(err)
	}

	if err := idx.ClearWallet("w1", "root"); err != nil {
		t.Fatalf("clear: %v", err)
	}

	result, total, err := idx.Query("w1", "root", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0 after clear", total)
	}
	if len(result) != 0 {
		t.Errorf("entries = %d, want 0 after clear", len(result))
	}

	meta, _ := idx.GetMeta("w1", "root")
	if meta.Count != 0 {
		t.Errorf("meta count = %d, want 0", meta.Count)
	}
}

func TestWalletTxIndex_MultipleWallets(t *testing.T) {
	idx := newTestIndex()

	// Two wallets, same chain.
	if err := idx.PutEntries("alice", "root", 0, []TxHistoryEntry{
		{TxHash: "alice-tx", Type: "mined"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := idx.PutEntries("bob", "root", 0, []TxHistoryEntry{
		{TxHash: "bob-tx", Type: "received"},
	}); err != nil {
		t.Fatal(err)
	}

	// Query alice.
	alice, total, _ := idx.Query("alice", "root", 50, 0)
	if total != 1 {
		t.Errorf("alice total = %d, want 1", total)
	}
	if alice[0].TxHash != "alice-tx" {
		t.Errorf("alice tx = %s, want alice-tx", alice[0].TxHash)
	}

	// Query bob.
	bob, total, _ := idx.Query("bob", "root", 50, 0)
	if total != 1 {
		t.Errorf("bob total = %d, want 1", total)
	}
	if bob[0].TxHash != "bob-tx" {
		t.Errorf("bob tx = %s, want bob-tx", bob[0].TxHash)
	}
}

func TestWalletTxIndex_MultipleChains(t *testing.T) {
	idx := newTestIndex()

	// Same wallet, different chains.
	if err := idx.PutEntries("w1", "root", 0, []TxHistoryEntry{
		{TxHash: "root-tx", Type: "mined"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := idx.PutEntries("w1", "abc123", 0, []TxHistoryEntry{
		{TxHash: "sc-tx", Type: "received"},
	}); err != nil {
		t.Fatal(err)
	}

	root, rootTotal, _ := idx.Query("w1", "root", 50, 0)
	if rootTotal != 1 || root[0].TxHash != "root-tx" {
		t.Errorf("root query: total=%d, tx=%v", rootTotal, root)
	}

	sc, scTotal, _ := idx.Query("w1", "abc123", 50, 0)
	if scTotal != 1 || sc[0].TxHash != "sc-tx" {
		t.Errorf("sub-chain query: total=%d, tx=%v", scTotal, sc)
	}
}

func TestWalletTxIndex_MetaFresh(t *testing.T) {
	idx := newTestIndex()

	// Getting meta for a non-existent wallet should return zero values.
	meta, err := idx.GetMeta("nonexistent", "root")
	if err != nil {
		t.Fatalf("get meta: %v", err)
	}
	if meta.LastHeight != 0 || meta.Count != 0 {
		t.Errorf("fresh meta = %+v, want zero", meta)
	}
}

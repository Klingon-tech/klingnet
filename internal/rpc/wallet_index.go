package rpc

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Klingon-tech/klingnet-chain/internal/chain"
	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// WalletTxIndex is a persistent, incremental transaction index for wallet
// history. It stores classified TxHistoryEntry records keyed by wallet name,
// chain ID, and reverse block height so that newest entries come first during
// iteration.
//
// Key layout (all under the "x/" prefix namespace):
//
//	Entry:    "e/<wallet>/<chain>/<revHeight8><txIdx4>" → JSON TxHistoryEntry
//	Metadata: "m/<wallet>/<chain>"                      → JSON indexMeta
//
// revHeight is (math.MaxUint64 - blockHeight) encoded as 8 big-endian bytes,
// ensuring ForEach iterates from newest to oldest.
type WalletTxIndex struct {
	db storage.DB
}

// indexMeta tracks the last indexed height per wallet+chain so we can do
// incremental updates.
type indexMeta struct {
	LastHeight uint64 `json:"last_height"`
	Count      int    `json:"count"`
}

// NewWalletTxIndex creates a new wallet transaction index backed by db.
// The index uses a "x/" prefix namespace to avoid collisions with other data.
func NewWalletTxIndex(db storage.DB) *WalletTxIndex {
	return &WalletTxIndex{db: storage.NewPrefixDB(db, []byte("x/"))}
}

// entryKeyPrefix returns the prefix for all entries of a wallet on a chain.
// chain is "root" for the root chain or hex chain ID for sub-chains.
func entryKeyPrefix(wallet, chainID string) []byte {
	return []byte(fmt.Sprintf("e/%s/%s/", wallet, chainID))
}

// entryKey builds the full key for a specific entry.
func entryKey(wallet, chainID string, height uint64, txIdx int) []byte {
	prefix := entryKeyPrefix(wallet, chainID)
	// Reverse height (newest first) + tx index.
	revHeight := ^height // math.MaxUint64 - height
	var buf [12]byte
	binary.BigEndian.PutUint64(buf[:8], revHeight)
	binary.BigEndian.PutUint32(buf[8:], uint32(txIdx))
	return append(prefix, buf[:]...)
}

// metaKey returns the metadata key for a wallet+chain.
func metaKey(wallet, chainID string) []byte {
	return []byte(fmt.Sprintf("m/%s/%s", wallet, chainID))
}

// GetMeta returns the index metadata for a wallet on a chain.
func (idx *WalletTxIndex) GetMeta(wallet, chainID string) (indexMeta, error) {
	data, err := idx.db.Get(metaKey(wallet, chainID))
	if err != nil {
		return indexMeta{}, nil // Not found = fresh index.
	}
	var meta indexMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return indexMeta{}, fmt.Errorf("corrupt index meta: %w", err)
	}
	return meta, nil
}

// setMeta stores the index metadata.
func (idx *WalletTxIndex) setMeta(wallet, chainID string, meta indexMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return idx.db.Put(metaKey(wallet, chainID), data)
}

// PutEntries stores a batch of classified entries for a single block.
func (idx *WalletTxIndex) PutEntries(wallet, chainID string, height uint64, entries []TxHistoryEntry) error {
	for i, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("marshal entry: %w", err)
		}
		key := entryKey(wallet, chainID, height, i)
		if err := idx.db.Put(key, data); err != nil {
			return fmt.Errorf("put entry: %w", err)
		}
	}
	return nil
}

// DeleteAbove removes all entries above the given height (for reorg rollback).
// It also adjusts the metadata count.
func (idx *WalletTxIndex) DeleteAbove(wallet, chainID string, maxHeight uint64) error {
	prefix := entryKeyPrefix(wallet, chainID)
	var toDelete [][]byte

	// Iterate all entries. Since keys are reverse-height, entries for heights
	// above maxHeight have revHeight < ^maxHeight, meaning they come first in
	// sorted order. We must iterate everything and check.
	err := idx.db.ForEach(prefix, func(key, value []byte) error {
		// Extract height from the key suffix (last 12 bytes of the unprefixed key).
		suffix := key[len(prefix):]
		if len(suffix) < 12 {
			return nil
		}
		revHeight := binary.BigEndian.Uint64(suffix[:8])
		height := ^revHeight
		if height > maxHeight {
			k := make([]byte, len(key))
			copy(k, key)
			toDelete = append(toDelete, k)
		}
		return nil
	})
	if err != nil {
		return err
	}

	for _, k := range toDelete {
		if err := idx.db.Delete(k); err != nil {
			return err
		}
	}

	// Update metadata count.
	if len(toDelete) > 0 {
		meta, _ := idx.GetMeta(wallet, chainID)
		meta.Count -= len(toDelete)
		if meta.Count < 0 {
			meta.Count = 0
		}
		meta.LastHeight = maxHeight
		return idx.setMeta(wallet, chainID, meta)
	}

	return nil
}

// Query retrieves paginated history entries for a wallet on a chain.
// Returns entries newest-first with total count.
func (idx *WalletTxIndex) Query(wallet, chainID string, limit, offset int) ([]TxHistoryEntry, int, error) {
	prefix := entryKeyPrefix(wallet, chainID)

	// Collect all matching entries. ForEach iteration order is not guaranteed
	// to be sorted (MemoryDB uses map), so we collect and sort.
	type kv struct {
		key   string
		value []byte
	}
	var all []kv

	err := idx.db.ForEach(prefix, func(key, value []byte) error {
		v := make([]byte, len(value))
		copy(v, value)
		all = append(all, kv{key: string(key), value: v})
		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	// Sort by key (reverse height ordering built into the key).
	sort.Slice(all, func(i, j int) bool {
		return all[i].key < all[j].key
	})

	total := len(all)

	// Apply pagination.
	if offset >= total {
		return []TxHistoryEntry{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := all[offset:end]

	entries := make([]TxHistoryEntry, 0, len(page))
	for _, kv := range page {
		var e TxHistoryEntry
		if err := json.Unmarshal(kv.value, &e); err != nil {
			continue // Skip corrupt entries.
		}
		entries = append(entries, e)
	}

	return entries, total, nil
}

// IndexBlocks scans blocks from startHeight to endHeight (inclusive) and
// stores classified entries for the given wallet addresses.
// classifyFn is the function that classifies a transaction as relevant to the
// wallet (returns nil if not relevant).
func (idx *WalletTxIndex) IndexBlocks(
	wallet, chainID string,
	ch *chain.Chain,
	startHeight, endHeight uint64,
	addrSet map[types.Address]bool,
	classifyFn func(transaction interface{}, txIdx int, addrSet map[types.Address]bool, blk interface{}) *TxHistoryEntry,
) (int, error) {
	meta, err := idx.GetMeta(wallet, chainID)
	if err != nil {
		return 0, err
	}

	added := 0
	for h := startHeight; h <= endHeight; h++ {
		blk, err := ch.GetBlockByHeight(h)
		if err != nil {
			continue
		}

		blockHash := blk.Hash().String()
		blockTime := blk.Header.Timestamp
		var blockEntries []TxHistoryEntry

		for txIdx, transaction := range blk.Transactions {
			entry := classifyFn(transaction, txIdx, addrSet, blk)
			if entry == nil {
				continue
			}
			entry.BlockHash = blockHash
			entry.Height = h
			entry.Timestamp = blockTime
			entry.Confirmed = true
			blockEntries = append(blockEntries, *entry)
		}

		if len(blockEntries) > 0 {
			if err := idx.PutEntries(wallet, chainID, h, blockEntries); err != nil {
				return added, err
			}
			added += len(blockEntries)
		}
	}

	// Update metadata.
	meta.LastHeight = endHeight
	meta.Count += added
	if err := idx.setMeta(wallet, chainID, meta); err != nil {
		return added, err
	}

	return added, nil
}

// ClearWallet removes all index data for a wallet on a given chain.
func (idx *WalletTxIndex) ClearWallet(wallet, chainID string) error {
	prefix := entryKeyPrefix(wallet, chainID)
	var toDelete [][]byte

	err := idx.db.ForEach(prefix, func(key, _ []byte) error {
		k := make([]byte, len(key))
		copy(k, key)
		toDelete = append(toDelete, k)
		return nil
	})
	if err != nil {
		return err
	}

	for _, k := range toDelete {
		if err := idx.db.Delete(k); err != nil {
			return err
		}
	}

	// Delete metadata.
	return idx.db.Delete(metaKey(wallet, chainID))
}

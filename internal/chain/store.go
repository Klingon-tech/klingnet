package chain

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/Klingon-tech/klingnet-chain/internal/storage"
	"github.com/Klingon-tech/klingnet-chain/pkg/block"
	"github.com/Klingon-tech/klingnet-chain/pkg/types"
)

// Key prefixes and state keys for the block store.
var (
	prefixBlock  = []byte("b/") // b/<hash(32)> -> block JSON
	prefixHeight = []byte("h/") // h/<height(8)> -> hash(32)
	prefixTx     = []byte("x/") // x/<txhash(32)> -> height(8) + blockHash(32)
	prefixUndo   = []byte("d/") // d/<hash(32)> -> undo data JSON
	keyTipHash          = []byte("s/tip")
	keyHeight           = []byte("s/height")
	keySupply           = []byte("s/supply")
	keyCumDifficulty    = []byte("s/cumdiff")
	keyReorgCheckpoint  = []byte("s/reorg")
)

// BlockStore persists blocks and chain metadata to a storage.DB.
type BlockStore struct {
	db storage.DB
}

// NewBlockStore creates a block store backed by the given database.
func NewBlockStore(db storage.DB) *BlockStore {
	return &BlockStore{db: db}
}

// StoreBlock stores a block by its hash only, without updating height or tx
// indexes. Use this for blocks that are not (yet) on the active chain.
func (bs *BlockStore) StoreBlock(blk *block.Block) error {
	data, err := json.Marshal(blk)
	if err != nil {
		return fmt.Errorf("block marshal: %w", err)
	}
	hash := blk.Hash()
	if err := bs.db.Put(blockKey(hash), data); err != nil {
		return fmt.Errorf("block put: %w", err)
	}
	return nil
}

// PutBlock stores a block and indexes it by hash, height, and tx hashes.
func (bs *BlockStore) PutBlock(blk *block.Block) error {
	data, err := json.Marshal(blk)
	if err != nil {
		return fmt.Errorf("block marshal: %w", err)
	}

	hash := blk.Hash()
	if err := bs.db.Put(blockKey(hash), data); err != nil {
		return fmt.Errorf("block put: %w", err)
	}

	if err := bs.db.Put(heightKey(blk.Header.Height), hash[:]); err != nil {
		return fmt.Errorf("height index put: %w", err)
	}

	// Index each transaction by hash → (height, blockHash).
	for _, t := range blk.Transactions {
		txHash := t.Hash()
		val := make([]byte, 8+types.HashSize)
		binary.BigEndian.PutUint64(val[:8], blk.Header.Height)
		copy(val[8:], hash[:])
		if err := bs.db.Put(txKey(txHash), val); err != nil {
			return fmt.Errorf("tx index put %s: %w", txHash, err)
		}
	}

	return nil
}

// GetBlock retrieves a block by its hash.
func (bs *BlockStore) GetBlock(hash types.Hash) (*block.Block, error) {
	data, err := bs.db.Get(blockKey(hash))
	if err != nil {
		return nil, fmt.Errorf("block get: %w", err)
	}
	var blk block.Block
	if err := json.Unmarshal(data, &blk); err != nil {
		return nil, fmt.Errorf("block unmarshal: %w", err)
	}
	return &blk, nil
}

// GetBlockByHeight retrieves a block by its height.
func (bs *BlockStore) GetBlockByHeight(height uint64) (*block.Block, error) {
	hashBytes, err := bs.db.Get(heightKey(height))
	if err != nil {
		return nil, fmt.Errorf("height index get: %w", err)
	}
	if len(hashBytes) != types.HashSize {
		return nil, fmt.Errorf("corrupt height index: got %d bytes, want %d", len(hashBytes), types.HashSize)
	}
	var hash types.Hash
	copy(hash[:], hashBytes)
	return bs.GetBlock(hash)
}

// HasBlock checks if a block exists by hash.
func (bs *BlockStore) HasBlock(hash types.Hash) (bool, error) {
	return bs.db.Has(blockKey(hash))
}

// SetTip stores the current chain tip hash, height, and supply.
func (bs *BlockStore) SetTip(hash types.Hash, height, supply uint64) error {
	if err := bs.db.Put(keyTipHash, hash[:]); err != nil {
		return fmt.Errorf("set tip hash: %w", err)
	}
	var heightBuf, supplyBuf [8]byte
	binary.BigEndian.PutUint64(heightBuf[:], height)
	if err := bs.db.Put(keyHeight, heightBuf[:]); err != nil {
		return fmt.Errorf("set tip height: %w", err)
	}
	binary.BigEndian.PutUint64(supplyBuf[:], supply)
	if err := bs.db.Put(keySupply, supplyBuf[:]); err != nil {
		return fmt.Errorf("set supply: %w", err)
	}
	return nil
}

// GetTip returns the current chain tip hash, height, and supply.
// Returns zero values if no tip is set (fresh chain).
func (bs *BlockStore) GetTip() (types.Hash, uint64, uint64, error) {
	hashBytes, err := bs.db.Get(keyTipHash)
	if err != nil {
		return types.Hash{}, 0, 0, nil // No tip yet.
	}
	if len(hashBytes) != types.HashSize {
		return types.Hash{}, 0, 0, fmt.Errorf("corrupt tip hash: got %d bytes", len(hashBytes))
	}

	heightBytes, err := bs.db.Get(keyHeight)
	if err != nil {
		return types.Hash{}, 0, 0, fmt.Errorf("tip height missing: %w", err)
	}
	if len(heightBytes) != 8 {
		return types.Hash{}, 0, 0, fmt.Errorf("corrupt tip height: got %d bytes", len(heightBytes))
	}

	var supply uint64
	supplyBytes, err := bs.db.Get(keySupply)
	if err == nil && len(supplyBytes) == 8 {
		supply = binary.BigEndian.Uint64(supplyBytes)
	}
	// Missing supply key is OK for backwards compat with old DBs.

	var hash types.Hash
	copy(hash[:], hashBytes)
	height := binary.BigEndian.Uint64(heightBytes)
	return hash, height, supply, nil
}

// GetTxLocation returns the block height and hash that contain the given transaction.
func (bs *BlockStore) GetTxLocation(txHash types.Hash) (uint64, types.Hash, error) {
	data, err := bs.db.Get(txKey(txHash))
	if err != nil {
		return 0, types.Hash{}, fmt.Errorf("tx index get: %w", err)
	}
	if len(data) != 8+types.HashSize {
		return 0, types.Hash{}, fmt.Errorf("corrupt tx index: got %d bytes, want %d", len(data), 8+types.HashSize)
	}
	height := binary.BigEndian.Uint64(data[:8])
	var blockHash types.Hash
	copy(blockHash[:], data[8:])
	return height, blockHash, nil
}

// DeleteTxIndex removes the transaction index entry for the given hash.
func (bs *BlockStore) DeleteTxIndex(txHash types.Hash) error {
	return bs.db.Delete(txKey(txHash))
}

func blockKey(hash types.Hash) []byte {
	key := make([]byte, len(prefixBlock)+types.HashSize)
	copy(key, prefixBlock)
	copy(key[len(prefixBlock):], hash[:])
	return key
}

func heightKey(height uint64) []byte {
	key := make([]byte, len(prefixHeight)+8)
	copy(key, prefixHeight)
	binary.BigEndian.PutUint64(key[len(prefixHeight):], height)
	return key
}

func txKey(hash types.Hash) []byte {
	key := make([]byte, len(prefixTx)+types.HashSize)
	copy(key, prefixTx)
	copy(key[len(prefixTx):], hash[:])
	return key
}

func undoKey(hash types.Hash) []byte {
	key := make([]byte, len(prefixUndo)+types.HashSize)
	copy(key, prefixUndo)
	copy(key[len(prefixUndo):], hash[:])
	return key
}

// PutUndo stores undo data for a block (used for reorgs).
func (bs *BlockStore) PutUndo(hash types.Hash, data []byte) error {
	if err := bs.db.Put(undoKey(hash), data); err != nil {
		return fmt.Errorf("put undo: %w", err)
	}
	return nil
}

// GetUndo retrieves undo data for a block.
func (bs *BlockStore) GetUndo(hash types.Hash) ([]byte, error) {
	data, err := bs.db.Get(undoKey(hash))
	if err != nil {
		return nil, fmt.Errorf("get undo: %w", err)
	}
	return data, nil
}

// DeleteUndo removes undo data for a block.
func (bs *BlockStore) DeleteUndo(hash types.Hash) error {
	return bs.db.Delete(undoKey(hash))
}

// SetCumulativeDifficulty persists the cumulative difficulty.
func (bs *BlockStore) SetCumulativeDifficulty(cumDiff uint64) error {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], cumDiff)
	return bs.db.Put(keyCumDifficulty, buf[:])
}

// GetCumulativeDifficulty retrieves the cumulative difficulty (0 if unset).
func (bs *BlockStore) GetCumulativeDifficulty() uint64 {
	data, err := bs.db.Get(keyCumDifficulty)
	if err != nil || len(data) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(data)
}

// PutReorgCheckpoint writes a marker indicating a reorg is in progress.
// If the node crashes during reorg, this marker triggers UTXO recovery on restart.
func (bs *BlockStore) PutReorgCheckpoint(forkHeight uint64) error {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], forkHeight)
	return bs.db.Put(keyReorgCheckpoint, buf[:])
}

// GetReorgCheckpoint returns the fork height and true if a reorg checkpoint exists.
func (bs *BlockStore) GetReorgCheckpoint() (uint64, bool) {
	data, err := bs.db.Get(keyReorgCheckpoint)
	if err != nil || len(data) != 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(data), true
}

// DeleteReorgCheckpoint removes the reorg-in-progress marker.
func (bs *BlockStore) DeleteReorgCheckpoint() error {
	return bs.db.Delete(keyReorgCheckpoint)
}

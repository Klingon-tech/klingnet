package storage

import (
	"errors"
	"strings"
)

// MemoryDB implements DB using an in-memory map.
type MemoryDB struct {
	data map[string][]byte
}

// NewMemory creates a new in-memory database.
func NewMemory() *MemoryDB {
	return &MemoryDB{
		data: make(map[string][]byte),
	}
}

// Get retrieves a value by key.
func (m *MemoryDB) Get(key []byte) ([]byte, error) {
	v, ok := m.data[string(key)]
	if !ok {
		return nil, errors.New("key not found")
	}
	return v, nil
}

// Put stores a key-value pair.
func (m *MemoryDB) Put(key, value []byte) error {
	m.data[string(key)] = value
	return nil
}

// Delete removes a key.
func (m *MemoryDB) Delete(key []byte) error {
	delete(m.data, string(key))
	return nil
}

// Has checks if a key exists.
func (m *MemoryDB) Has(key []byte) (bool, error) {
	_, ok := m.data[string(key)]
	return ok, nil
}

// ForEach iterates over all keys with the given prefix.
func (m *MemoryDB) ForEach(prefix []byte, fn func(key, value []byte) error) error {
	p := string(prefix)
	for k, v := range m.data {
		if strings.HasPrefix(k, p) {
			if err := fn([]byte(k), v); err != nil {
				return err
			}
		}
	}
	return nil
}

// Close closes the database.
func (m *MemoryDB) Close() error {
	return nil
}

// NewBatch creates a buffered batch that applies all writes on Commit.
func (m *MemoryDB) NewBatch() Batch {
	return &memoryBatch{db: m}
}

type memoryBatchOp struct {
	key    string
	value  []byte // nil means delete
}

type memoryBatch struct {
	db  *MemoryDB
	ops []memoryBatchOp
}

func (mb *memoryBatch) Put(key, value []byte) error {
	v := make([]byte, len(value))
	copy(v, value)
	mb.ops = append(mb.ops, memoryBatchOp{key: string(key), value: v})
	return nil
}

func (mb *memoryBatch) Delete(key []byte) error {
	mb.ops = append(mb.ops, memoryBatchOp{key: string(key), value: nil})
	return nil
}

func (mb *memoryBatch) Commit() error {
	for _, op := range mb.ops {
		if op.value == nil {
			delete(mb.db.data, op.key)
		} else {
			mb.db.data[op.key] = op.value
		}
	}
	return nil
}

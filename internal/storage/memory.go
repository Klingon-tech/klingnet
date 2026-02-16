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

package storage

import (
	"fmt"
	"strings"

	"github.com/dgraph-io/badger/v4"
)

// BadgerDB implements DB using Badger.
type BadgerDB struct {
	db *badger.DB
}

// NewBadger creates a new Badger database at the given path.
func NewBadger(path string) (*BadgerDB, error) {
	opts := badger.DefaultOptions(path)
	opts.Logger = nil // Disable badger's built-in logging.

	db, err := badger.Open(opts)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "Cannot acquire directory lock") ||
			strings.Contains(errMsg, "resource temporarily unavailable") {
			return nil, fmt.Errorf("database at %s is locked by another process (is another klingnetd instance running?): %w", path, err)
		}
		return nil, fmt.Errorf("open database at %s: %w", path, err)
	}
	return &BadgerDB{db: db}, nil
}

// Get retrieves a value by key. Returns an error if the key does not exist.
func (b *BadgerDB) Get(key []byte) ([]byte, error) {
	var val []byte
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		val, err = item.ValueCopy(nil)
		return err
	})
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("key not found")
	}
	if err != nil {
		return nil, fmt.Errorf("badger get: %w", err)
	}
	return val, nil
}

// Put stores a key-value pair.
func (b *BadgerDB) Put(key, value []byte) error {
	err := b.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, value)
	})
	if err != nil {
		return fmt.Errorf("badger put: %w", err)
	}
	return nil
}

// Delete removes a key.
func (b *BadgerDB) Delete(key []byte) error {
	err := b.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
	if err != nil {
		return fmt.Errorf("badger delete: %w", err)
	}
	return nil
}

// Has checks if a key exists.
func (b *BadgerDB) Has(key []byte) (bool, error) {
	var exists bool
	err := b.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get(key)
		if err == badger.ErrKeyNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		exists = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("badger has: %w", err)
	}
	return exists, nil
}

// ForEach iterates over all keys with the given prefix.
func (b *BadgerDB) ForEach(prefix []byte, fn func(key, value []byte) error) error {
	return b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := item.KeyCopy(nil)
			err := item.Value(func(val []byte) error {
				return fn(key, val)
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// Close closes the database.
func (b *BadgerDB) Close() error {
	return b.db.Close()
}

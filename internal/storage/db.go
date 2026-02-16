// Package storage provides database abstractions.
package storage

// DB is the interface for key-value storage.
type DB interface {
	Get(key []byte) ([]byte, error)
	Put(key, value []byte) error
	Delete(key []byte) error
	Has(key []byte) (bool, error)
	// ForEach iterates over all keys with the given prefix.
	// The callback receives a copy of the key and value.
	// Return a non-nil error from fn to stop iteration early.
	ForEach(prefix []byte, fn func(key, value []byte) error) error
	Close() error
}

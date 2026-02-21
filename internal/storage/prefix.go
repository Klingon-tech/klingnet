package storage

// PrefixDB wraps a DB and prepends a fixed prefix to all keys.
// This isolates sub-chain data within a single underlying database.
type PrefixDB struct {
	inner  DB
	prefix []byte
}

// NewPrefixDB creates a new PrefixDB wrapping inner with the given prefix.
func NewPrefixDB(inner DB, prefix []byte) *PrefixDB {
	p := make([]byte, len(prefix))
	copy(p, prefix)
	return &PrefixDB{inner: inner, prefix: p}
}

// prefixed returns key with the prefix prepended.
func (p *PrefixDB) prefixed(key []byte) []byte {
	out := make([]byte, len(p.prefix)+len(key))
	copy(out, p.prefix)
	copy(out[len(p.prefix):], key)
	return out
}

// Get retrieves a value by key.
func (p *PrefixDB) Get(key []byte) ([]byte, error) {
	return p.inner.Get(p.prefixed(key))
}

// Put stores a key-value pair.
func (p *PrefixDB) Put(key, value []byte) error {
	return p.inner.Put(p.prefixed(key), value)
}

// Delete removes a key.
func (p *PrefixDB) Delete(key []byte) error {
	return p.inner.Delete(p.prefixed(key))
}

// Has checks if a key exists.
func (p *PrefixDB) Has(key []byte) (bool, error) {
	return p.inner.Has(p.prefixed(key))
}

// ForEach iterates over all keys with the given prefix (within the PrefixDB namespace).
// The callback receives keys with the PrefixDB prefix stripped, so callers see only
// their logical keyspace.
func (p *PrefixDB) ForEach(prefix []byte, fn func(key, value []byte) error) error {
	fullPrefix := p.prefixed(prefix)
	return p.inner.ForEach(fullPrefix, func(key, value []byte) error {
		// Strip the PrefixDB prefix so the caller sees only its logical key.
		stripped := key[len(p.prefix):]
		return fn(stripped, value)
	})
}

// DeleteAll removes all keys under this PrefixDB's namespace from the inner DB.
func (p *PrefixDB) DeleteAll() error {
	// Collect all keys first to avoid modifying during iteration.
	var keys [][]byte
	err := p.inner.ForEach(p.prefix, func(key, _ []byte) error {
		k := make([]byte, len(key))
		copy(k, key)
		keys = append(keys, k)
		return nil
	})
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := p.inner.Delete(key); err != nil {
			return err
		}
	}
	return nil
}

// Close is a no-op — the outer DB manages its own lifecycle.
func (p *PrefixDB) Close() error {
	return nil
}

// NewBatch creates a batch that prepends the prefix to all keys, delegating
// to the inner DB's batch for atomic commits.
func (p *PrefixDB) NewBatch() Batch {
	batcher, ok := p.inner.(Batcher)
	if !ok {
		// Fallback: non-atomic batch using individual writes.
		return &prefixFallbackBatch{db: p}
	}
	return &prefixBatch{inner: batcher.NewBatch(), prefix: p.prefix}
}

type prefixBatch struct {
	inner  Batch
	prefix []byte
}

func (pb *prefixBatch) Put(key, value []byte) error {
	out := make([]byte, len(pb.prefix)+len(key))
	copy(out, pb.prefix)
	copy(out[len(pb.prefix):], key)
	return pb.inner.Put(out, value)
}

func (pb *prefixBatch) Delete(key []byte) error {
	out := make([]byte, len(pb.prefix)+len(key))
	copy(out, pb.prefix)
	copy(out[len(pb.prefix):], key)
	return pb.inner.Delete(out)
}

func (pb *prefixBatch) Commit() error {
	return pb.inner.Commit()
}

// prefixFallbackBatch buffers writes and applies them non-atomically
// when the inner DB doesn't support batching.
type prefixFallbackBatch struct {
	db  *PrefixDB
	ops []struct {
		key   []byte
		value []byte // nil means delete
	}
}

func (fb *prefixFallbackBatch) Put(key, value []byte) error {
	k := make([]byte, len(key))
	copy(k, key)
	v := make([]byte, len(value))
	copy(v, value)
	fb.ops = append(fb.ops, struct {
		key   []byte
		value []byte
	}{k, v})
	return nil
}

func (fb *prefixFallbackBatch) Delete(key []byte) error {
	k := make([]byte, len(key))
	copy(k, key)
	fb.ops = append(fb.ops, struct {
		key   []byte
		value []byte
	}{k, nil})
	return nil
}

func (fb *prefixFallbackBatch) Commit() error {
	for _, op := range fb.ops {
		if op.value == nil {
			if err := fb.db.Delete(op.key); err != nil {
				return err
			}
		} else {
			if err := fb.db.Put(op.key, op.value); err != nil {
				return err
			}
		}
	}
	return nil
}

package storage

import (
	"bytes"
	"testing"
)

// testDB runs the shared test suite against a DB implementation.
func testDB(t *testing.T, db DB) {
	t.Helper()

	t.Run("PutAndGet", func(t *testing.T) {
		err := db.Put([]byte("key1"), []byte("value1"))
		if err != nil {
			t.Fatalf("Put() error: %v", err)
		}

		val, err := db.Get([]byte("key1"))
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		if !bytes.Equal(val, []byte("value1")) {
			t.Errorf("Get() = %q, want %q", val, "value1")
		}
	})

	t.Run("GetNonexistent", func(t *testing.T) {
		_, err := db.Get([]byte("nonexistent"))
		if err == nil {
			t.Error("Get() for missing key should return error")
		}
	})

	t.Run("Has", func(t *testing.T) {
		db.Put([]byte("exists"), []byte("yes"))

		ok, err := db.Has([]byte("exists"))
		if err != nil {
			t.Fatalf("Has() error: %v", err)
		}
		if !ok {
			t.Error("Has() = false for existing key")
		}

		ok, err = db.Has([]byte("missing"))
		if err != nil {
			t.Fatalf("Has() error: %v", err)
		}
		if ok {
			t.Error("Has() = true for missing key")
		}
	})

	t.Run("Overwrite", func(t *testing.T) {
		db.Put([]byte("ow"), []byte("first"))
		db.Put([]byte("ow"), []byte("second"))

		val, err := db.Get([]byte("ow"))
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		if !bytes.Equal(val, []byte("second")) {
			t.Errorf("Get() after overwrite = %q, want %q", val, "second")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		db.Put([]byte("del"), []byte("value"))

		err := db.Delete([]byte("del"))
		if err != nil {
			t.Fatalf("Delete() error: %v", err)
		}

		ok, _ := db.Has([]byte("del"))
		if ok {
			t.Error("key should be gone after Delete()")
		}

		_, err = db.Get([]byte("del"))
		if err == nil {
			t.Error("Get() after Delete() should return error")
		}
	})

	t.Run("DeleteNonexistent", func(t *testing.T) {
		// Deleting a nonexistent key should not error.
		err := db.Delete([]byte("never-existed"))
		if err != nil {
			t.Errorf("Delete() nonexistent key error: %v", err)
		}
	})

	t.Run("EmptyValue", func(t *testing.T) {
		err := db.Put([]byte("empty"), []byte{})
		if err != nil {
			t.Fatalf("Put() empty value error: %v", err)
		}

		val, err := db.Get([]byte("empty"))
		if err != nil {
			t.Fatalf("Get() empty value error: %v", err)
		}
		if len(val) != 0 {
			t.Errorf("expected empty value, got %d bytes", len(val))
		}
	})

	t.Run("BinaryData", func(t *testing.T) {
		key := []byte{0x00, 0x01, 0xFF}
		value := make([]byte, 256)
		for i := range value {
			value[i] = byte(i)
		}

		err := db.Put(key, value)
		if err != nil {
			t.Fatalf("Put() binary error: %v", err)
		}

		got, err := db.Get(key)
		if err != nil {
			t.Fatalf("Get() binary error: %v", err)
		}
		if !bytes.Equal(got, value) {
			t.Error("binary roundtrip failed")
		}
	})

	t.Run("ForEach", func(t *testing.T) {
		db.Put([]byte("prefix/a"), []byte("1"))
		db.Put([]byte("prefix/b"), []byte("2"))
		db.Put([]byte("prefix/c"), []byte("3"))
		db.Put([]byte("other/x"), []byte("4"))

		var count int
		err := db.ForEach([]byte("prefix/"), func(key, value []byte) error {
			count++
			return nil
		})
		if err != nil {
			t.Fatalf("ForEach() error: %v", err)
		}
		if count != 3 {
			t.Errorf("ForEach(prefix/) count = %d, want 3", count)
		}
	})

	t.Run("ForEachEmpty", func(t *testing.T) {
		var count int
		err := db.ForEach([]byte("nonexistent/"), func(key, value []byte) error {
			count++
			return nil
		})
		if err != nil {
			t.Fatalf("ForEach() error: %v", err)
		}
		if count != 0 {
			t.Errorf("ForEach(nonexistent/) count = %d, want 0", count)
		}
	})
}

func TestMemoryDB(t *testing.T) {
	db := NewMemory()
	defer db.Close()
	testDB(t, db)
}

func TestBadgerDB(t *testing.T) {
	dir := t.TempDir()
	db, err := NewBadger(dir)
	if err != nil {
		t.Fatalf("NewBadger() error: %v", err)
	}
	defer db.Close()
	testDB(t, db)
}

func TestBadgerDB_Persistence(t *testing.T) {
	dir := t.TempDir()

	// Write data.
	db1, err := NewBadger(dir)
	if err != nil {
		t.Fatalf("NewBadger() error: %v", err)
	}
	db1.Put([]byte("persist"), []byte("data"))
	db1.Close()

	// Reopen and read.
	db2, err := NewBadger(dir)
	if err != nil {
		t.Fatalf("NewBadger() reopen error: %v", err)
	}
	defer db2.Close()

	val, err := db2.Get([]byte("persist"))
	if err != nil {
		t.Fatalf("Get() after reopen error: %v", err)
	}
	if !bytes.Equal(val, []byte("data")) {
		t.Errorf("persisted value = %q, want %q", val, "data")
	}
}

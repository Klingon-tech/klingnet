package storage

import (
	"fmt"
	"sort"
	"testing"
)

func TestPrefixDB_GetPutDelete(t *testing.T) {
	inner := NewMemory()
	db := NewPrefixDB(inner, []byte("ns1/"))

	// Put and Get.
	if err := db.Put([]byte("key1"), []byte("val1")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := db.Get([]byte("key1"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "val1" {
		t.Fatalf("Get = %q, want %q", got, "val1")
	}

	// Has.
	ok, err := db.Has([]byte("key1"))
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if !ok {
		t.Fatal("Has = false, want true")
	}

	// Delete.
	if err := db.Delete([]byte("key1")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	ok, err = db.Has([]byte("key1"))
	if err != nil {
		t.Fatalf("Has after delete: %v", err)
	}
	if ok {
		t.Fatal("Has after delete = true, want false")
	}
}

func TestPrefixDB_Isolation(t *testing.T) {
	inner := NewMemory()
	dbA := NewPrefixDB(inner, []byte("a/"))
	dbB := NewPrefixDB(inner, []byte("b/"))

	// Write to A.
	if err := dbA.Put([]byte("key"), []byte("fromA")); err != nil {
		t.Fatal(err)
	}
	// Write to B.
	if err := dbB.Put([]byte("key"), []byte("fromB")); err != nil {
		t.Fatal(err)
	}

	// A sees its own value.
	got, err := dbA.Get([]byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fromA" {
		t.Fatalf("A.Get = %q, want %q", got, "fromA")
	}

	// B sees its own value.
	got, err = dbB.Get([]byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fromB" {
		t.Fatalf("B.Get = %q, want %q", got, "fromB")
	}

	// A cannot see B's key.
	ok, err := dbA.Has([]byte("b/key"))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("A should not see B's raw key")
	}
}

func TestPrefixDB_ForEach(t *testing.T) {
	inner := NewMemory()
	db := NewPrefixDB(inner, []byte("sc/abc/"))

	// Put several keys with different sub-prefixes.
	db.Put([]byte("u/k1"), []byte("v1"))
	db.Put([]byte("u/k2"), []byte("v2"))
	db.Put([]byte("b/k3"), []byte("v3"))

	// ForEach with "u/" prefix should only return u/ keys.
	var keys []string
	err := db.ForEach([]byte("u/"), func(key, value []byte) error {
		keys = append(keys, string(key))
		return nil
	})
	if err != nil {
		t.Fatalf("ForEach: %v", err)
	}

	sort.Strings(keys)
	if len(keys) != 2 {
		t.Fatalf("ForEach returned %d keys, want 2", len(keys))
	}
	if keys[0] != "u/k1" || keys[1] != "u/k2" {
		t.Fatalf("ForEach keys = %v, want [u/k1 u/k2]", keys)
	}
}

func TestPrefixDB_ForEachStripsPrefix(t *testing.T) {
	inner := NewMemory()
	db := NewPrefixDB(inner, []byte("pre/"))

	db.Put([]byte("hello"), []byte("world"))

	var sawKey string
	db.ForEach(nil, func(key, value []byte) error {
		sawKey = string(key)
		return nil
	})

	if sawKey != "hello" {
		t.Fatalf("ForEach callback key = %q, want %q (prefix should be stripped)", sawKey, "hello")
	}
}

func TestPrefixDB_ForEachStopEarly(t *testing.T) {
	inner := NewMemory()
	db := NewPrefixDB(inner, []byte("p/"))

	for i := 0; i < 10; i++ {
		db.Put([]byte(fmt.Sprintf("k%d", i)), []byte("v"))
	}

	count := 0
	stopErr := fmt.Errorf("stop")
	err := db.ForEach(nil, func(key, value []byte) error {
		count++
		if count >= 3 {
			return stopErr
		}
		return nil
	})
	if err != stopErr {
		t.Fatalf("ForEach err = %v, want stopErr", err)
	}
	if count != 3 {
		t.Fatalf("ForEach called %d times, want 3", count)
	}
}

func TestPrefixDB_DeleteAll(t *testing.T) {
	inner := NewMemory()
	dbA := NewPrefixDB(inner, []byte("a/"))
	dbB := NewPrefixDB(inner, []byte("b/"))

	// Write to both namespaces.
	dbA.Put([]byte("k1"), []byte("v1"))
	dbA.Put([]byte("k2"), []byte("v2"))
	dbA.Put([]byte("k3"), []byte("v3"))
	dbB.Put([]byte("k1"), []byte("other"))

	// Delete all from A.
	if err := dbA.DeleteAll(); err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}

	// A should be empty.
	for _, k := range []string{"k1", "k2", "k3"} {
		ok, _ := dbA.Has([]byte(k))
		if ok {
			t.Fatalf("A still has %q after DeleteAll", k)
		}
	}

	// B should be untouched.
	got, err := dbB.Get([]byte("k1"))
	if err != nil {
		t.Fatalf("B.Get after A.DeleteAll: %v", err)
	}
	if string(got) != "other" {
		t.Fatalf("B.Get = %q, want %q", got, "other")
	}
}

func TestPrefixDB_DeleteAll_Empty(t *testing.T) {
	inner := NewMemory()
	db := NewPrefixDB(inner, []byte("empty/"))

	// DeleteAll on empty PrefixDB should not error.
	if err := db.DeleteAll(); err != nil {
		t.Fatalf("DeleteAll on empty: %v", err)
	}
}

func TestPrefixDB_CloseIsNoop(t *testing.T) {
	inner := NewMemory()
	db := NewPrefixDB(inner, []byte("x/"))

	db.Put([]byte("key"), []byte("val"))

	// Close the PrefixDB — should not affect inner.
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Inner should still have the data.
	got, err := inner.Get([]byte("x/key"))
	if err != nil {
		t.Fatalf("inner.Get after Close: %v", err)
	}
	if string(got) != "val" {
		t.Fatalf("inner.Get = %q, want %q", got, "val")
	}
}

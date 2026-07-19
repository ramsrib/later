package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nested", "items.jsonl")
	t.Setenv("LATER_STORE", path)
	store, err := Open(true)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestStoreAtomicRewriteAndBackup(t *testing.T) {
	store := tempStore(t)
	first := Item{ID: "first", Subject: "First", Scope: "global", NotBefore: time.Now().UTC(), CreatedAt: time.Now().UTC(), By: "shell"}
	if err := store.Update(func(items []Item) ([]Item, error) { return append(items, first), nil }); err != nil {
		t.Fatal(err)
	}
	second := Item{ID: "second", Subject: "Second", Scope: "global", NotBefore: time.Now().UTC(), CreatedAt: time.Now().UTC(), By: "shell"}
	if err := store.Update(func(items []Item) ([]Item, error) { return append(items, second), nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.tmpPath()); !os.IsNotExist(err) {
		t.Fatalf("temporary file remains after replace: %v", err)
	}
	items, skipped, err := store.Read()
	if err != nil || skipped != 0 || len(items) != 2 {
		t.Fatalf("current store: items=%d skipped=%d err=%v", len(items), skipped, err)
	}
	bak, err := os.Open(store.bakPath())
	if err != nil {
		t.Fatal(err)
	}
	defer bak.Close()
	backupItems, skipped, err := readItems(bak)
	if err != nil || skipped != 0 || len(backupItems) != 1 || backupItems[0].ID != "first" {
		t.Fatalf("backup store: items=%#v skipped=%d err=%v", backupItems, skipped, err)
	}
	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("store permissions are too broad: %o", info.Mode().Perm())
	}
}

func TestCorruptLineTolerance(t *testing.T) {
	store := tempStore(t)
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		t.Fatal(err)
	}
	valid1, _ := json.Marshal(Item{ID: "one", Scope: "global"})
	valid2, _ := json.Marshal(Item{ID: "two", Scope: "global"})
	data := append(append(append(valid1, '\n'), []byte("{not json}\n")...), append(valid2, '\n')...)
	if err := os.WriteFile(store.path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	items, skipped, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	if len(items) != 2 || items[0].ID != "one" || items[1].ID != "two" {
		t.Fatalf("valid records were not preserved: %#v", items)
	}
}

func TestOpenStoreRemovesStaleTemporaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "items.jsonl")
	t.Setenv("LATER_STORE", path)
	if err := os.WriteFile(path+".tmp", []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("stale temp was not removed: %v", err)
	}
}

func TestOpenStoreDoesNotRemoveActiveWritersTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.jsonl")
	t.Setenv("LATER_STORE", path)
	lock, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
	if err := os.WriteFile(path+".tmp", []byte("active writer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); err != nil {
		t.Fatalf("active temporary file was removed: %v", err)
	}
}

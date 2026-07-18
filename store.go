package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type Item struct {
	ID        string     `json:"id"`
	Subject   string     `json:"subject"`
	Body      string     `json:"body,omitempty"`
	Scope     string     `json:"scope"`
	NotBefore time.Time  `json:"not_before"`
	Recur     *string    `json:"recur"`
	CreatedAt time.Time  `json:"created_at"`
	DoneAt    *time.Time `json:"done_at"`
	By        string     `json:"by"`
}

type Store struct {
	Path string
}

func openStore(cleanup bool) (*Store, error) {
	path := os.Getenv("LATER_STORE")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("find home directory: %w", err)
		}
		path = filepath.Join(home, ".later", "items.jsonl")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve store path: %w", err)
	}
	s := &Store{Path: filepath.Clean(abs)}
	if cleanup {
		if err := s.cleanupStaleTemp(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) tmpPath() string  { return s.Path + ".tmp" }
func (s *Store) bakPath() string  { return s.Path + ".bak" }
func (s *Store) lockPath() string { return filepath.Join(filepath.Dir(s.Path), ".lock") }

func (s *Store) cleanupStaleTemp() error {
	if _, err := os.Stat(s.tmpPath()); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect temporary store: %w", err)
	}
	// A fixed same-directory temp name is required for predictable recovery,
	// so startup cleanup must distinguish a stale file from an active writer's
	// file. The nonblocking probe preserves the no-wait reader hot path.
	lock, err := os.OpenFile(s.lockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open lock for temporary cleanup: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil
		}
		return fmt.Errorf("probe lock for temporary cleanup: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
	if err := os.Remove(s.tmpPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale temporary store: %w", err)
	}
	return nil
}

func (s *Store) Read() ([]Item, int, error) {
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	return readItems(f)
}

func readItems(r io.Reader) ([]Item, int, error) {
	reader := bufio.NewReader(r)
	var items []Item
	skipped := 0
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) == 0 && errors.Is(err, io.EOF) {
			break
		}
		var item Item
		if decodeErr := json.Unmarshal(line, &item); decodeErr != nil {
			skipped++
		} else {
			items = append(items, item)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return items, skipped, err
		}
	}
	return items, skipped, nil
}

// Update serializes every mutation behind flock, then replaces the complete
// file. Readers intentionally take no lock: rename guarantees they see either
// the old generation or the new one, never a partially rewritten queue.
func (s *Store) Update(fn func([]Item) ([]Item, error)) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("create store directory: %w", err)
	}
	lock, err := os.OpenFile(s.lockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock store: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck

	items, _, err := s.Read()
	if err != nil {
		return fmt.Errorf("read store: %w", err)
	}
	items, err = fn(items)
	if err != nil {
		return err
	}
	if err := s.backup(); err != nil {
		return err
	}
	return s.replace(items)
}

func (s *Store) backup() error {
	src, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open store for backup: %w", err)
	}
	defer src.Close()
	dst, err := os.OpenFile(s.bakPath(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create backup: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return fmt.Errorf("copy backup: %w", err)
	}
	if err := dst.Sync(); err != nil {
		dst.Close()
		return fmt.Errorf("sync backup: %w", err)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("close backup: %w", err)
	}
	return nil
}

func (s *Store) replace(items []Item) error {
	f, err := os.OpenFile(s.tmpPath(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary store: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
		}
	}()
	enc := json.NewEncoder(f)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			return fmt.Errorf("encode store: %w", err)
		}
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync temporary store: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temporary store: %w", err)
	}
	if err := os.Rename(s.tmpPath(), s.Path); err != nil {
		return fmt.Errorf("replace store: %w", err)
	}
	if dir, err := os.Open(filepath.Dir(s.Path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	ok = true
	return nil
}

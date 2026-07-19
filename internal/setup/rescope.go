package setup

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ramsrib/later/internal/store"
)

func (a *app) rescope(args []string, queue *store.Store) error {
	fs := newFlagSet("rescope", `later rescope <old-path> <new-path>`, a.stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireArgs(fs, fs.Args(), 2, "an old path and a new path"); err != nil {
		return err
	}
	oldPath := strings.TrimSuffix(fs.Arg(0), string(filepath.Separator))
	newPath, err := canonicalPath(fs.Arg(1))
	if err != nil {
		return fmt.Errorf("resolve new path: %w", err)
	}
	count := 0
	err = queue.Update(func(items []store.Item) ([]store.Item, error) {
		for i := range items {
			if strings.HasPrefix(items[i].Scope, oldPath) {
				items[i].Scope = newPath + strings.TrimPrefix(items[i].Scope, oldPath)
				count++
			}
		}
		return items, nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Rescoped %d record(s).\n", count)
	return nil
}

func canonicalPath(value string) (string, error) {
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved), nil
	}
	return abs, nil
}

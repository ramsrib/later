package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type migrationCandidate struct {
	Item       Item
	Unresolved bool
}

func (a *app) migrate(args []string, store *Store) error {
	fsFlags := newFlagSet("migrate", `later migrate [--dry-run]`, a.stderr)
	dryRun := fsFlags.Bool("dry-run", false, "report imports without writing the v2 store")
	if err := fsFlags.Parse(args); err != nil {
		return err
	}
	if len(fsFlags.Args()) != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fsFlags.Args(), " "))
	}
	root, err := legacyProjectsPath()
	if err != nil {
		return err
	}
	candidates, sourceProblems, err := collectMigrationCandidates(root)
	if err != nil {
		return err
	}
	existing, _, err := store.Read()
	if err != nil {
		return err
	}
	existingIDs := make(map[string]bool, len(existing))
	for _, item := range existing {
		existingIDs[item.ID] = true
	}
	var pending []migrationCandidate
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if existingIDs[candidate.Item.ID] || seen[candidate.Item.ID] {
			continue
		}
		seen[candidate.Item.ID] = true
		pending = append(pending, candidate)
	}
	if *dryRun {
		a.printMigrationSummary(root, pending, len(candidates)-len(pending), sourceProblems, true)
		return nil
	}
	imported := 0
	if err := store.Update(func(items []Item) ([]Item, error) {
		ids := make(map[string]bool, len(items))
		for _, item := range items {
			ids[item.ID] = true
		}
		for _, candidate := range candidates {
			if ids[candidate.Item.ID] {
				continue
			}
			items = append(items, candidate.Item)
			ids[candidate.Item.ID] = true
			imported++
		}
		return items, nil
	}); err != nil {
		return err
	}
	a.printMigrationSummary(root, pending[:min(imported, len(pending))], len(candidates)-imported, sourceProblems, false)
	return nil
}

func legacyProjectsPath() (string, error) {
	if path := os.Getenv("LATER_CLAUDE_PROJECTS"); path != "" {
		return filepath.Abs(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

func collectMigrationCandidates(root string) ([]migrationCandidate, int, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read legacy projects: %w", err)
	}
	var all []migrationCandidate
	problems := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projectDir := filepath.Join(root, entry.Name())
		scope, ok := reconstructScope(entry.Name())
		if !ok {
			scope = entry.Name()
		}
		doneIDs, doneAt, err := readDoneFile(filepath.Join(projectDir, "mailbox.done"))
		if err != nil {
			problems++
		}
		files := []string{filepath.Join(projectDir, "mailbox")}
		staged, _ := filepath.Glob(filepath.Join(projectDir, "mailbox-staged", "*.msg"))
		files = append(files, staged...)
		for _, path := range files {
			records, err := parseLegacyFile(path, scope, doneIDs, doneAt)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				problems++
				continue
			}
			for _, item := range records {
				all = append(all, migrationCandidate{Item: item, Unresolved: !ok})
			}
		}
	}
	return all, problems, nil
}

func readDoneFile(path string) (map[string]bool, time.Time, error) {
	ids := map[string]bool{}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ids, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if id := strings.TrimSpace(line); id != "" {
			ids[id] = true
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	return ids, info.ModTime().UTC(), nil
}

func parseLegacyFile(path, scope string, doneIDs map[string]bool, doneAt time.Time) ([]Item, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var chunks []string
	var current strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(string(b)))
	flush := func() {
		if current.Len() != 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "From later@local") {
			flush()
			continue
		}
		current.WriteString(line)
		current.WriteByte('\n')
	}
	flush()
	var items []Item
	for _, chunk := range chunks {
		item, ok := parseLegacyRecord(chunk, scope)
		if !ok {
			continue
		}
		if doneIDs[item.ID] {
			value := doneAt
			item.DoneAt = &value
		}
		items = append(items, item)
	}
	return items, nil
}

func parseLegacyRecord(record, scope string) (Item, bool) {
	parts := strings.SplitN(record, "\n\n", 2)
	if len(parts) != 2 {
		return Item{}, false
	}
	headers := map[string]string{}
	for _, line := range strings.Split(parts[0], "\n") {
		name, value, ok := strings.Cut(line, ":")
		if ok {
			headers[strings.TrimSpace(name)] = strings.TrimSpace(value)
		}
	}
	id := headers["X-Later-Id"]
	subject := headers["X-Later-Subject"]
	notBefore, err1 := time.Parse(time.RFC3339, headers["X-Later-Scheduled-For"])
	createdAt, err2 := time.Parse(time.RFC3339, headers["X-Later-Created-At"])
	if id == "" || subject == "" || err1 != nil || err2 != nil {
		return Item{}, false
	}
	var recur *string
	if value := headers["X-Later-Recur"]; value != "" {
		value = strings.TrimPrefix(value, "+")
		recur = &value
	}
	return Item{ID: id, Subject: subject, Body: strings.TrimSuffix(parts[1], "\n"), Scope: scope, NotBefore: notBefore.UTC(), Recur: recur, CreatedAt: createdAt.UTC(), By: "claude"}, true
}

// The v1 encoding replaced both slashes and literal dashes with dashes. Walk
// the real filesystem and try the longest token grouping at each level first;
// this recovers names such as "recall-app" without inventing slash boundaries.
func reconstructScope(encoded string) (string, bool) {
	if !strings.HasPrefix(encoded, "-") {
		return "", false
	}
	tokens := strings.Split(strings.TrimPrefix(encoded, "-"), "-")
	var walk func(string, int) (string, bool)
	walk = func(parent string, index int) (string, bool) {
		if index == len(tokens) {
			return parent, true
		}
		for end := len(tokens); end > index; end-- {
			component := strings.Join(tokens[index:end], "-")
			candidate := filepath.Join(parent, component)
			info, err := os.Stat(candidate)
			if err != nil || !info.IsDir() {
				continue
			}
			if resolved, ok := walk(candidate, end); ok {
				return resolved, true
			}
		}
		return "", false
	}
	return walk(string(filepath.Separator), 0)
}

func (a *app) printMigrationSummary(root string, candidates []migrationCandidate, skipped, problems int, dryRun bool) {
	groups := map[string]int{}
	unresolved := 0
	for _, candidate := range candidates {
		groups[candidate.Item.Scope]++
		if candidate.Unresolved {
			unresolved++
		}
	}
	scopes := make([]string, 0, len(groups))
	for scope := range groups {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	mode := "Migration"
	if dryRun {
		mode = "Dry run"
	}
	fmt.Fprintf(a.stdout, "%s from %s: %d record(s) %s, %d already present/duplicate, %d unreadable source file(s).\n", mode, root, len(candidates), map[bool]string{true: "would be imported", false: "imported"}[dryRun], skipped, problems)
	for _, scope := range scopes {
		fmt.Fprintf(a.stdout, "  %s: %d\n", scope, groups[scope])
	}
	if unresolved != 0 {
		fmt.Fprintf(a.stdout, "  WARNING: %d record(s) have unresolved encoded scopes. Use `later rescope <encoded> <absolute-path>` to fix them.\n", unresolved)
	}
}

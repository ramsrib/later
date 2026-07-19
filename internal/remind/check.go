package remind

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ramsrib/later/internal/store"
)

func (a *app) add(args []string, queue *store.Store) error {
	fs := newFlagSet("add", `later add --subject S [--body B|-] (--at TIME | --in DUR) [--recur DUR] [--global] [--id ID]`, a.stderr)
	subject := fs.String("subject", "", "short reminder subject (required)")
	bodyValue := fs.String("body", "", "longer detail, or - to read stdin")
	atValue := fs.String("at", "", "RFC3339 or YYYY-MM-DD HH:MM local time")
	inValue := fs.String("in", "", "delay such as 30m, 4h, 3d, or 2w")
	recurValue := fs.String("recur", "", "repeat interval using the same duration syntax")
	global := fs.Bool("global", false, "make the reminder visible in every project")
	idValue := fs.String("id", "", "stable unique id (generated from subject by default)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*subject) == "" {
		return fmt.Errorf("--subject is required")
	}
	if (*atValue == "") == (*inValue == "") {
		return fmt.Errorf("exactly one of --at or --in is required")
	}
	now := a.now()
	var notBefore time.Time
	var err error
	if *atValue != "" {
		notBefore, err = parseWhen(*atValue, now)
	} else {
		var d time.Duration
		d, err = parseDuration(*inValue)
		notBefore = now.Add(d)
	}
	if err != nil {
		return err
	}
	var recur *string
	if *recurValue != "" {
		if _, err := parseDuration(*recurValue); err != nil {
			return fmt.Errorf("--recur: %w", err)
		}
		value := *recurValue
		recur = &value
	}
	body, err := parseBody(*bodyValue)
	if err != nil {
		return err
	}
	scope := "global"
	if !*global {
		scope, err = resolveScope()
		if err != nil {
			return fmt.Errorf("resolve project scope: %w", err)
		}
	}
	id := *idValue
	if id == "" {
		id, err = generatedID(*subject)
		if err != nil {
			return err
		}
	}
	if strings.ContainsAny(id, " \t\r\n") || id == "" {
		return fmt.Errorf("id must be non-empty and contain no whitespace")
	}
	item := store.Item{ID: id, Subject: *subject, Body: body, Scope: scope, NotBefore: notBefore.UTC(), Recur: recur, CreatedAt: now.UTC(), By: detectAgent()}
	if err := queue.Update(func(items []store.Item) ([]store.Item, error) {
		for _, existing := range items {
			if existing.ID == id {
				return nil, fmt.Errorf("id %q already exists", id)
			}
		}
		return append(items, item), nil
	}); err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, id)
	return nil
}

func generatedID(subject string) (string, error) {
	var words []string
	var current []rune
	flush := func() {
		if len(current) != 0 {
			words = append(words, string(current))
			current = nil
		}
	}
	for _, r := range strings.ToLower(subject) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current = append(current, r)
		} else {
			flush()
		}
	}
	flush()
	slug := strings.Join(words, "-")
	// Cap the slug well short of the subject: the id is printed in full on every
	// `check` line (it is the handle you type back), so a long one crowds out the
	// subject that tells you what the reminder is actually about.
	slugRunes := []rune(slug)
	if len(slugRunes) > 24 {
		slug = strings.Trim(string(slugRunes[:24]), "-")
	}
	if slug == "" {
		slug = "reminder"
	}
	random := make([]byte, 3)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return slug + "-" + hex.EncodeToString(random), nil
}

func dueItems(items []store.Item, scope string, now time.Time) []store.Item {
	var due []store.Item
	for _, item := range items {
		if item.DoneAt == nil && !now.Before(item.NotBefore) && (item.Scope == scope || item.Scope == "global") {
			due = append(due, item)
		}
	}
	sortItems(due, now)
	return due
}

func (a *app) check(args []string, queue *store.Store) error {
	fs := newFlagSet("check", `later check [--quiet-if-empty] [--json]`, a.stderr)
	quiet := fs.Bool("quiet-if-empty", false, "print nothing when no reminders are due")
	jsonOutput := fs.Bool("json", false, "emit due records as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	scope, err := resolveScope()
	if err != nil {
		return err
	}
	items, skipped, err := queue.Read()
	if err != nil {
		return err
	}
	if skipped != 0 {
		fmt.Fprintf(a.stderr, "later check: skipped %d corrupt store line(s); run later doctor\n", skipped)
	}
	due := dueItems(items, scope, a.now())
	if *jsonOutput {
		return writeJSON(a.stdout, due)
	}
	if len(due) == 0 {
		if !*quiet {
			fmt.Fprintln(a.stdout, "[later] no reminders due")
		}
		return nil
	}
	fmt.Fprintf(a.stdout, "[later] %d due:\n", len(due))
	limit := min(3, len(due))
	idWidth := widestID(due[:limit], 16)
	for _, item := range due[:limit] {
		scopeName := scopeLabel(item.Scope)
		when := relativeWhen(item.NotBefore, a.now())
		prefix := fmt.Sprintf("  %-*s %-12s %-12s ", idWidth, item.ID, truncate(scopeName, 12), truncate(when, 12))
		fmt.Fprintln(a.stdout, prefix+truncate(item.Subject, max(10, 100-len(prefix))))
	}
	if len(due) > limit {
		fmt.Fprintf(a.stdout, "  ...and %d more (later list)\n", len(due)-limit)
	}
	fmt.Fprintln(a.stdout, "  `later show <id>` for detail · `later done <id>` when handled")
	return nil
}

func (a *app) list(args []string, queue *store.Store) error {
	fs := newFlagSet("list", `later list [--all] [--here] [--json]`, a.stderr)
	all := fs.Bool("all", false, "include every project scope")
	here := fs.Bool("here", false, "only this project, excluding global reminders")
	jsonOutput := fs.Bool("json", false, "emit records as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *all && *here {
		return fmt.Errorf("--all and --here are mutually exclusive")
	}
	scope, err := resolveScope()
	if err != nil {
		return err
	}
	items, skipped, err := queue.Read()
	if err != nil {
		return err
	}
	if skipped != 0 {
		fmt.Fprintf(a.stderr, "later list: skipped %d corrupt store line(s)\n", skipped)
	}
	filtered := items[:0]
	for _, item := range items {
		if item.DoneAt != nil {
			continue
		}
		if *all || (*here && item.Scope == scope) || (!*here && (item.Scope == scope || item.Scope == "global")) {
			filtered = append(filtered, item)
		}
	}
	sortItems(filtered, a.now())
	if *jsonOutput {
		return writeJSON(a.stdout, filtered)
	}
	if len(filtered) == 0 {
		fmt.Fprintln(a.stdout, "No outstanding reminders.")
		return nil
	}
	idWidth := widestID(filtered, 16)
	fmt.Fprintf(a.stdout, "%-*s %-14s %-14s %s\n", idWidth, "ID", "SCOPE", "WHEN", "SUBJECT")
	for _, item := range filtered {
		fmt.Fprintf(a.stdout, "%-*s %-14s %-14s %s\n", idWidth, item.ID, truncate(scopeLabel(item.Scope), 14), truncate(relativeWhen(item.NotBefore, a.now()), 14), item.Subject)
	}
	return nil
}

func sortItems(items []store.Item, now time.Time) {
	sort.SliceStable(items, func(i, j int) bool {
		iOverdue := !now.Before(items[i].NotBefore)
		jOverdue := !now.Before(items[j].NotBefore)
		if iOverdue != jOverdue {
			return iOverdue
		}
		return items[i].NotBefore.Before(items[j].NotBefore)
	})
}

func scopeLabel(scope string) string {
	if scope == "global" {
		return scope
	}
	return filepath.Base(scope)
}

func relativeWhen(when, now time.Time) string {
	delta := now.Sub(when)
	if delta >= 24*time.Hour {
		return fmt.Sprintf("%dd overdue", int(delta/(24*time.Hour)))
	}
	if delta >= time.Hour {
		return fmt.Sprintf("%dh overdue", int(delta/time.Hour))
	}
	if delta >= time.Minute {
		return fmt.Sprintf("%dm overdue", int(delta/time.Minute))
	}
	if delta >= 0 {
		if sameLocalDay(when, now) {
			return "due today"
		}
		return "due now"
	}
	until := -delta
	if until >= 7*24*time.Hour {
		return fmt.Sprintf("in %dw", int(until/(7*24*time.Hour)))
	}
	if until >= 24*time.Hour {
		return fmt.Sprintf("in %dd", int(until/(24*time.Hour)))
	}
	if until >= time.Hour {
		return fmt.Sprintf("in %dh", int(until/time.Hour))
	}
	return fmt.Sprintf("in %dm", max(1, int(until/time.Minute)))
}

func sameLocalDay(x, y time.Time) bool {
	x = x.In(y.Location())
	return x.Year() == y.Year() && x.YearDay() == y.YearDay()
}

// widestID sizes the ID column to the longest id being printed. The id is the
// handle the reader must type back into `show`/`done`, so it is the one field
// that must never be elided — truncate the subject instead.
func widestID(items []store.Item, min int) int {
	w := min
	for _, it := range items {
		if n := len([]rune(it.ID)); n > w {
			w = n
		}
	}
	return w
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func writeJSON(w interface{ Write([]byte) (int, error) }, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func findItem(items []store.Item, id string) (store.Item, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return store.Item{}, false
}

func (a *app) show(args []string, queue *store.Store) error {
	fs := newFlagSet("show", `later show <id>`, a.stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireArgs(fs, fs.Args(), 1, "one reminder id"); err != nil {
		return err
	}
	items, _, err := queue.Read()
	if err != nil {
		return err
	}
	item, ok := findItem(items, fs.Arg(0))
	if !ok {
		return fmt.Errorf("id %q not found", fs.Arg(0))
	}
	fmt.Fprintf(a.stdout, "ID: %s\nSubject: %s\nBody: %s\nScope: %s\nNot before: %s\n", item.ID, item.Subject, item.Body, item.Scope, item.NotBefore.Format(time.RFC3339))
	if item.Recur == nil {
		fmt.Fprintln(a.stdout, "Recur: none")
	} else {
		fmt.Fprintf(a.stdout, "Recur: %s\n", *item.Recur)
	}
	fmt.Fprintf(a.stdout, "Created at: %s\n", item.CreatedAt.Format(time.RFC3339))
	if item.DoneAt == nil {
		fmt.Fprintln(a.stdout, "Done at: outstanding")
	} else {
		fmt.Fprintf(a.stdout, "Done at: %s\n", item.DoneAt.Format(time.RFC3339))
	}
	fmt.Fprintf(a.stdout, "By: %s\n", item.By)
	return nil
}

func advanceAfter(notBefore time.Time, recur time.Duration, now time.Time) time.Time {
	// Advancing the record itself avoids a fragile chain of re-arming jobs. A
	// long gap collapses missed occurrences into the first one strictly ahead.
	if notBefore.After(now) {
		return notBefore
	}
	missed := now.Sub(notBefore)/recur + 1
	return notBefore.Add(missed * recur)
}

func (a *app) done(args []string, queue *store.Store) error {
	fs := newFlagSet("done", `later done <id>`, a.stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireArgs(fs, fs.Args(), 1, "one reminder id"); err != nil {
		return err
	}
	id := fs.Arg(0)
	var message string
	now := a.now().UTC()
	err := queue.Update(func(items []store.Item) ([]store.Item, error) {
		for i := range items {
			if items[i].ID != id {
				continue
			}
			if items[i].DoneAt != nil {
				return nil, fmt.Errorf("id %q is already done", id)
			}
			if items[i].Recur != nil {
				d, err := parseDuration(*items[i].Recur)
				if err != nil {
					return nil, fmt.Errorf("stored recurrence for %q: %w", id, err)
				}
				items[i].NotBefore = advanceAfter(items[i].NotBefore, d, now)
				message = fmt.Sprintf("Advanced %s to %s", id, items[i].NotBefore.Format(time.RFC3339))
			} else {
				items[i].DoneAt = &now
				message = "Done: " + id
			}
			return items, nil
		}
		return nil, fmt.Errorf("id %q not found", id)
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, message)
	return nil
}

func (a *app) cancel(args []string, queue *store.Store) error {
	fs := newFlagSet("cancel", `later cancel <id>`, a.stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireArgs(fs, fs.Args(), 1, "one reminder id"); err != nil {
		return err
	}
	id := fs.Arg(0)
	var subject string
	err := queue.Update(func(items []store.Item) ([]store.Item, error) {
		for i, item := range items {
			if item.ID == id {
				subject = item.Subject
				return append(items[:i], items[i+1:]...), nil
			}
		}
		return nil, fmt.Errorf("id %q not found", id)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Cancelled %s: %s\n", id, subject)
	return nil
}

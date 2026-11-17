package remind

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ramsrib/later/internal/store"
)

func TestDueItemsRoutesProjectAndGlobal(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	done := now.Add(-time.Hour)
	items := []store.Item{
		{ID: "here-due", Scope: "/projects/here", NotBefore: now.Add(-time.Minute)},
		{ID: "global-due", Scope: "global", NotBefore: now.Add(-time.Hour)},
		{ID: "other-due", Scope: "/projects/other", NotBefore: now.Add(-time.Hour)},
		{ID: "here-future", Scope: "/projects/here", NotBefore: now.Add(time.Hour)},
		{ID: "here-done", Scope: "/projects/here", NotBefore: now.Add(-time.Hour), DoneAt: &done},
	}
	got := dueItems(items, "/projects/here", now)
	if len(got) != 2 {
		t.Fatalf("got %d due items, want 2: %#v", len(got), got)
	}
	// The older global item is more overdue and therefore sorts first.
	if got[0].ID != "global-due" || got[1].ID != "here-due" {
		t.Fatalf("unexpected due order: %s, %s", got[0].ID, got[1].ID)
	}
}

func TestAdvanceAfterCollapsesMissedOccurrences(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	start := now.Add(-24 * 24 * time.Hour)
	got := advanceAfter(start, 7*24*time.Hour, now)
	want := start.Add(4 * 7 * 24 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("advanceAfter = %s, want %s", got, want)
	}
	if !got.After(now) {
		t.Fatalf("advanced occurrence %s is not strictly after %s", got, now)
	}
}

func TestAdvanceAfterLeavesFutureOccurrenceAlone(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(time.Hour)
	if got := advanceAfter(future, 7*24*time.Hour, now); !got.Equal(future) {
		t.Fatalf("future occurrence changed from %s to %s", future, got)
	}
}

// Codex parses hook stdout strictly as JSON, so every --hook-json path — due
// and empty alike — has to stay parseable.
func TestCheckHookJSONEmitsParseableEnvelope(t *testing.T) {
	t.Setenv("LATER_STORE", filepath.Join(t.TempDir(), "items.jsonl"))
	queue, err := store.Open(true)
	if err != nil {
		t.Fatal(err)
	}
	run := func() map[string]any {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if err := Run("check", []string{"--hook-json"}, queue, &stdout, &stderr); err != nil {
			t.Fatalf("check --hook-json: %v (stderr=%s)", err, stderr.String())
		}
		got := map[string]any{}
		if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
			t.Fatalf("stdout is not JSON: %v (%s)", err, stdout.String())
		}
		return got
	}
	if got := run(); len(got) != 0 {
		t.Fatalf("empty queue emitted %#v, want {}", got)
	}
	if err := queue.Update(func(items []store.Item) ([]store.Item, error) {
		return append(items, store.Item{
			ID:        "ping",
			Subject:   "Ping the queue",
			Scope:     "global",
			NotBefore: time.Now().UTC().Add(-time.Minute),
			CreatedAt: time.Now().UTC(),
			By:        "shell",
		}), nil
	}); err != nil {
		t.Fatal(err)
	}
	specific, ok := run()["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatal("due reminder produced no hookSpecificOutput")
	}
	if specific["hookEventName"] != "UserPromptSubmit" {
		t.Fatalf("hookEventName = %v", specific["hookEventName"])
	}
	context, _ := specific["additionalContext"].(string)
	if !strings.Contains(context, "ping") || !strings.Contains(context, "1 due") {
		t.Fatalf("additionalContext missing the due reminder: %q", context)
	}
}

func addArgs(id, subject string, extra ...string) []string {
	return append([]string{"--global", "--in", "1m", "--id", id, "--subject", subject}, extra...)
}

func runCmd(t *testing.T, queue *store.Store, cmd string, args ...string) error {
	t.Helper()
	var stdout, stderr bytes.Buffer
	return Run(cmd, args, queue, &stdout, &stderr)
}

func onlyItem(t *testing.T, queue *store.Store, id string) store.Item {
	t.Helper()
	items, _, err := queue.Read()
	if err != nil {
		t.Fatal(err)
	}
	var found []store.Item
	for _, item := range items {
		if item.ID == id {
			found = append(found, item)
		}
	}
	if len(found) != 1 {
		t.Fatalf("got %d records for %q, want 1", len(found), id)
	}
	return found[0]
}

// A job files "x-broken" with a fixed id. Once a human marks it done, the next
// breakage has to be able to file it again, or the breakage goes unreported.
func TestAddReArmsDoneID(t *testing.T) {
	t.Setenv("LATER_STORE", filepath.Join(t.TempDir(), "items.jsonl"))
	queue, err := store.Open(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, queue, "add", addArgs("x-broken", "first")...); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, queue, "done", "x-broken"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, queue, "add", addArgs("x-broken", "second")...); err != nil {
		t.Fatalf("re-adding a done id: %v", err)
	}
	item := onlyItem(t, queue, "x-broken")
	if item.DoneAt != nil || item.Subject != "second" {
		t.Fatalf("re-armed item = %+v, want outstanding with subject second", item)
	}
}

func TestAddRefusesOutstandingIDWithoutReplace(t *testing.T) {
	t.Setenv("LATER_STORE", filepath.Join(t.TempDir(), "items.jsonl"))
	queue, err := store.Open(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, queue, "add", addArgs("drift", "first")...); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, queue, "add", addArgs("drift", "second")...); err == nil {
		t.Fatal("second add of an outstanding id succeeded, want refusal")
	}
	if item := onlyItem(t, queue, "drift"); item.Subject != "first" {
		t.Fatalf("subject = %q, want the first report kept", item.Subject)
	}
}

func TestAddReplaceOverwritesOutstandingKeepingCreatedAt(t *testing.T) {
	t.Setenv("LATER_STORE", filepath.Join(t.TempDir(), "items.jsonl"))
	queue, err := store.Open(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, queue, "add", addArgs("queue", "1 blocked", "--body", "old")...); err != nil {
		t.Fatal(err)
	}
	created := onlyItem(t, queue, "queue").CreatedAt
	if err := runCmd(t, queue, "add", addArgs("queue", "4 blocked", "--body", "new", "--replace")...); err != nil {
		t.Fatal(err)
	}
	item := onlyItem(t, queue, "queue")
	if item.Subject != "4 blocked" || item.Body != "new" {
		t.Fatalf("replaced item = %+v, want the new report", item)
	}
	if !item.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt = %s, want original %s", item.CreatedAt, created)
	}
	if err := runCmd(t, queue, "add", append(addArgs("fresh", "x"), "--replace")...); err != nil {
		t.Fatalf("--replace with no existing record: %v", err)
	}
}

func TestAddReplaceNeedsID(t *testing.T) {
	t.Setenv("LATER_STORE", filepath.Join(t.TempDir(), "items.jsonl"))
	queue, err := store.Open(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, queue, "add", "--in", "1m", "--subject", "x", "--replace"); err == nil {
		t.Fatal("--replace without --id succeeded")
	}
}

// A migrated or hand-edited store can hold several records for one id. A done
// one must not let an outstanding duplicate slip past the guard.
func TestAddCollapsesDuplicateRecords(t *testing.T) {
	t.Setenv("LATER_STORE", filepath.Join(t.TempDir(), "items.jsonl"))
	queue, err := store.Open(true)
	if err != nil {
		t.Fatal(err)
	}
	done := time.Now().UTC().Add(-time.Hour)
	if err := queue.Update(func(items []store.Item) ([]store.Item, error) {
		return append(items,
			store.Item{ID: "dup", Subject: "old", Scope: "global", NotBefore: done, CreatedAt: done, DoneAt: &done},
			store.Item{ID: "dup", Subject: "open", Scope: "global", NotBefore: done, CreatedAt: done},
		), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, queue, "add", addArgs("dup", "new")...); err == nil {
		t.Fatal("add over an outstanding duplicate succeeded without --replace")
	}
	if err := runCmd(t, queue, "add", addArgs("dup", "new", "--replace")...); err != nil {
		t.Fatal(err)
	}
	if item := onlyItem(t, queue, "dup"); item.Subject != "new" || item.DoneAt != nil {
		t.Fatalf("collapsed item = %+v", item)
	}
}

func TestWriteDueShowsNewestFirstAndFoldsStale(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	a := &app{now: func() time.Time { return now }}
	due := []store.Item{
		{ID: "rotten", Scope: "global", Subject: "old", NotBefore: now.Add(-40 * 24 * time.Hour)},
		{ID: "older", Scope: "global", Subject: "older", NotBefore: now.Add(-3 * 24 * time.Hour)},
		{ID: "newest", Scope: "global", Subject: "newest", NotBefore: now.Add(-time.Hour)},
	}
	var buf bytes.Buffer
	a.writeDue(&buf, due)
	out := buf.String()
	if strings.Contains(out, "rotten") {
		t.Fatalf("stale item listed by name:\n%s", out)
	}
	if !strings.Contains(out, "1 stale") {
		t.Fatalf("stale count missing:\n%s", out)
	}
	if strings.Index(out, "newest") > strings.Index(out, "older") {
		t.Fatalf("newest due should come first:\n%s", out)
	}
}

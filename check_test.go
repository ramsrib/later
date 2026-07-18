package main

import (
	"testing"
	"time"
)

func TestDueItemsRoutesProjectAndGlobal(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	done := now.Add(-time.Hour)
	items := []Item{
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

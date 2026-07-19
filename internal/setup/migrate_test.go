package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReconstructScopeWithDashedDirectoryName(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "company", "recall-app")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	encoded := strings.ReplaceAll(project, string(filepath.Separator), "-")
	got, ok := reconstructScope(encoded)
	if !ok {
		t.Fatalf("reconstructScope(%q) failed", encoded)
	}
	if got != project {
		t.Fatalf("reconstructScope(%q) = %q, want %q", encoded, got, project)
	}
}

func TestParseLegacyRecord(t *testing.T) {
	record := "X-Later-Id: deploy-check\n" +
		"X-Later-Subject: Check deploy\n" +
		"X-Later-Scheduled-For: 2026-07-01T10:00:00Z\n" +
		"X-Later-Created-At: 2026-06-01T10:00:00Z\n" +
		"X-Later-Recur: +2w\n\n" +
		"Longer body\n"
	item, ok := parseLegacyRecord(record, "/tmp/example-app")
	if !ok {
		t.Fatal("record did not parse")
	}
	if item.ID != "deploy-check" || item.Body != "Longer body" || item.Recur == nil || *item.Recur != "2w" || item.By != "claude" {
		t.Fatalf("unexpected parsed item: %#v", item)
	}
}

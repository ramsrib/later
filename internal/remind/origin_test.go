package remind

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ramsrib/later/internal/store"
)

// clearAgentEnv removes every agent variable the test process inherited, and
// points HOME at an empty directory, so a test run inside Claude Code or Codex
// sees only what the case sets and never the host's transcripts.
func clearAgentEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, pair := range os.Environ() {
		name, _, _ := strings.Cut(pair, "=")
		if strings.HasPrefix(name, "CODEX_") || strings.HasPrefix(name, "CLAUDE") {
			t.Setenv(name, "")
			os.Unsetenv(name)
		}
	}
}

func TestDetectOrigin(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		env  map[string]string
		want origin
	}{
		{"plain shell", nil, origin{by: "shell"}},
		{"claude session", map[string]string{"CLAUDE_CODE_SESSION_ID": "c-1"}, origin{"claude", "c-1", dir}},
		{"claude session, project dir wins over cwd", map[string]string{"CLAUDE_CODE_SESSION_ID": "c-1", "CLAUDE_PROJECT_DIR": "/proj"}, origin{"claude", "c-1", "/proj"}},
		{"claude with the codex plugin", map[string]string{"CLAUDE_CODE_SESSION_ID": "c-1", "CODEX_COMPANION_SESSION_ID": "x"}, origin{"claude", "c-1", dir}},
		{"codex thread", map[string]string{"CODEX_THREAD_ID": "t-1"}, origin{"codex", "t-1", dir}},
		{"codex started from a claude session", map[string]string{"CODEX_THREAD_ID": "t-1", "CLAUDE_CODE_SESSION_ID": "c-1"}, origin{"codex", "t-1", dir}},
		{"codex session id fallback", map[string]string{"CODEX_SESSION_ID": "s-1"}, origin{"codex", "s-1", dir}},
		{"older codex, sandbox marker only", map[string]string{"CODEX_SANDBOX_NETWORK_DISABLED": "1"}, origin{by: "codex"}},
		{"codex home in a plain shell", map[string]string{"CODEX_HOME": "/x"}, origin{by: "shell"}},
		{"codex plugin vars alone", map[string]string{"CODEX_COMPANION_SESSION_ID": "x"}, origin{by: "shell"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearAgentEnv(t)
			t.Chdir(dir)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			got := detectOrigin()
			want := tc.want
			if want.cwd == dir {
				// macOS t.TempDir sits behind the /var -> /private/var symlink.
				want.cwd, _ = os.Getwd()
			}
			if got != want {
				t.Fatalf("detectOrigin() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestResumeCommand(t *testing.T) {
	cases := []struct {
		item store.Item
		want string
	}{
		{store.Item{By: "claude", Session: "abc", Cwd: "/Users/me/proj"}, "cd /Users/me/proj && claude --resume abc"},
		{store.Item{By: "codex", Session: "t-1", Cwd: "/tmp/it's here"}, `cd '/tmp/it'\''s here' && codex resume t-1`},
		{store.Item{By: "claude", Session: "abc"}, "claude --resume abc"},
		{store.Item{By: "claude"}, ""},
		{store.Item{By: "shell", Session: "abc"}, ""},
	}
	for _, tc := range cases {
		if got := resumeCommand(tc.item); got != tc.want {
			t.Errorf("resumeCommand(%+v) = %q, want %q", tc.item, got, tc.want)
		}
	}
}

func TestAddRecordsOriginAndShowPrintsResume(t *testing.T) {
	t.Setenv("LATER_STORE", filepath.Join(t.TempDir(), "items.jsonl"))
	clearAgentEnv(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sess-42")
	t.Setenv("CLAUDE_PROJECT_DIR", "/work/repo")
	queue, err := store.Open(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, queue, "add", addArgs("resume-me", "Pick this up")...); err != nil {
		t.Fatal(err)
	}
	item := onlyItem(t, queue, "resume-me")
	if item.By != "claude" || item.Session != "sess-42" || item.Cwd != "/work/repo" {
		t.Fatalf("stored origin = %q %q %q", item.By, item.Session, item.Cwd)
	}
	var stdout, stderr bytes.Buffer
	if err := Run("show", []string{"resume-me"}, queue, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Session: sess-42\n") || !strings.Contains(stdout.String(), "Resume: cd /work/repo && claude --resume sess-42\n") {
		t.Fatalf("show output lacks the resume line:\n%s", stdout.String())
	}
}

func TestClaudeOriginUsesTranscriptCwd(t *testing.T) {
	clearAgentEnv(t)
	// A config directory with glob metacharacters is a literal path.
	config := filepath.Join(t.TempDir(), "claude[work]")
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sess-7")
	t.Chdir(t.TempDir()) // the shell has wandered away from the project
	// The project dir is a symlink, as when transcripts moved to another disk.
	project := filepath.Join(t.TempDir(), "moved")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(config, "projects"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(project, filepath.Join(config, "projects", "-work-repo")); err != nil {
		t.Fatal(err)
	}
	// A long run of records without a cwd (summaries after compaction) must
	// not hide the first one that has it.
	transcript := strings.Repeat("{\"type\":\"summary\"}\n", 80) + "{\"type\":\"user\",\"cwd\":\"/work/repo\"}\n{\"cwd\":\"/elsewhere\"}\n"
	if err := os.WriteFile(filepath.Join(project, "sess-7.jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := detectOrigin(); got.cwd != "/work/repo" {
		t.Fatalf("cwd = %q, want the transcript's first cwd /work/repo", got.cwd)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "missing")
	want, _ := os.Getwd()
	if got := detectOrigin(); got.cwd != want {
		t.Fatalf("no transcript: cwd = %q, want the working directory %q", got.cwd, want)
	}
}

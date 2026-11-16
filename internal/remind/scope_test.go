package remind

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestScopeOfLinkedWorktreeIsMainCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	wt := filepath.Join(repo, ".claude", "worktrees", "feature")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run(repo, "init", "-q")
	run(repo, "commit", "-q", "--allow-empty", "-m", "init")
	run(repo, "worktree", "add", "-q", wt)

	want, _ := canonicalPath(repo)
	t.Setenv("LATER_SCOPE", "")
	for name, env := range map[string]string{"claude project dir": wt, "cwd": ""} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CLAUDE_PROJECT_DIR", env)
			t.Chdir(filepath.Join(wt))
			got, err := resolveScope()
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("scope = %q, want main checkout %q", got, want)
			}
		})
	}
}

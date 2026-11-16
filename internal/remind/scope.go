package remind

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func resolveScope() (string, error) {
	if value, ok := os.LookupEnv("LATER_SCOPE"); ok && value != "" {
		// Explicit overrides are literal so tests and callers can deliberately
		// target a scope that does not currently exist.
		return value, nil
	}
	dir := os.Getenv("CLAUDE_PROJECT_DIR")
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = cwd
	}
	if root := mainCheckout(dir); root != "" {
		return canonicalPath(root)
	}
	return canonicalPath(dir)
}

// mainCheckout is the repository's main working tree for dir, or "" outside
// git. A linked worktree resolves to its main checkout: a reminder added in a
// worktree belongs to the repo, and scoping it to the worktree path stranded
// it once the worktree was removed (and hid the repo's reminders from it).
func mainCheckout(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		return ""
	}
	common, top := strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	return top // a bare repo or a separate git dir: the worktree is all we know
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

func detectAgent() string {
	for _, pair := range os.Environ() {
		name, _, _ := strings.Cut(pair, "=")
		if strings.HasPrefix(name, "CODEX_") {
			return "codex"
		}
	}
	if _, ok := os.LookupEnv("CLAUDE_PROJECT_DIR"); ok {
		return "claude"
	}
	if _, ok := os.LookupEnv("CLAUDE_CODE_SESSION_ID"); ok {
		return "claude"
	}
	return "shell"
}

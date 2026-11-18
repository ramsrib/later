package remind

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ramsrib/later/internal/store"
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

// origin is the agent session that ran `later add`.
type origin struct {
	by      string // claude, codex, or shell
	session string // the agent's own session id, when it exposes one
	cwd     string // where the session ran, for resuming it
}

// detectOrigin checks each agent's own session variable before anything
// inherited. A Codex run started from a Claude session still carries
// CLAUDE_CODE_SESSION_ID, and a Claude session with the Codex plugin carries
// CODEX_COMPANION_* variables that name no Codex session, which the old
// any-CODEX_-prefix test recorded as codex.
func detectOrigin() origin {
	cwd, _ := os.Getwd()
	if id := firstEnv("CODEX_THREAD_ID", "CODEX_SESSION_ID"); id != "" {
		return origin{by: "codex", session: id, cwd: cwd}
	}
	if id := os.Getenv("CLAUDE_CODE_SESSION_ID"); id != "" {
		// `claude --resume` finds a session only from its own project
		// directory, and the shell that runs `later add` may have cd'd
		// elsewhere. CLAUDE_PROJECT_DIR reaches hooks but not the Bash tool,
		// so the session's transcript is the reliable record.
		if dir := os.Getenv("CLAUDE_PROJECT_DIR"); dir != "" {
			cwd = dir
		} else if dir := claudeSessionCwd(id); dir != "" {
			cwd = dir
		}
		return origin{by: "claude", session: id, cwd: cwd}
	}
	if os.Getenv("CLAUDE_PROJECT_DIR") != "" {
		return origin{by: "claude"}
	}
	// Codex versions without a session variable still mark sandboxed shells.
	// No broader CODEX_ prefix test: a plain shell can carry CODEX_HOME, and a
	// Claude session carries the plugin's CODEX_COMPANION_* variables.
	if firstEnv("CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED") != "" {
		return origin{by: "codex"}
	}
	return origin{by: "shell"}
}

// claudeSessionCwd reads the working directory Claude Code recorded for a
// session in its transcript, or "" when the transcript cannot be found.
func claudeSessionCwd(id string) string {
	if id == "" || strings.ContainsAny(id, "/\\") || id == "." || id == ".." {
		return ""
	}
	base := os.Getenv("CLAUDE_CONFIG_DIR")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".claude")
	}
	// ReadDir, not Glob: the config directory is a literal path, and one
	// containing [ or * must not match a sibling directory.
	projects := filepath.Join(base, "projects")
	dirs, err := os.ReadDir(projects)
	if err != nil {
		return ""
	}
	var f *os.File
	for _, dir := range dirs { // no IsDir check: a project dir may be a symlink
		if f, err = os.Open(filepath.Join(projects, dir.Name(), id+".jsonl")); err == nil {
			break
		}
		f = nil
	}
	if f == nil {
		return ""
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadBytes('\n')
		var entry struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(line, &entry) == nil && entry.Cwd != "" {
			return entry.Cwd
		}
		if err != nil {
			return ""
		}
	}
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

// resumeCommand is the shell command that reopens the session that added
// item, or "" when no session was recorded.
func resumeCommand(item store.Item) string {
	var cmd string
	switch item.By {
	case "claude":
		cmd = "claude --resume " + shellQuote(item.Session)
	case "codex":
		cmd = "codex resume " + shellQuote(item.Session)
	}
	if item.Session == "" || cmd == "" {
		return ""
	}
	if item.Cwd != "" {
		cmd = "cd " + shellQuote(item.Cwd) + " && " + cmd
	}
	return cmd
}

func shellQuote(value string) string {
	safe := value != ""
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+:@%=,", r)) {
			safe = false
			break
		}
	}
	if safe {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

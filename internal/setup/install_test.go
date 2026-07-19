package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMergeHookFilePreservesKeysAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := `{
  "theme": "dark",
  "hooks": {
    "SessionStart": [{"hooks": [{"type": "command", "command": "existing"}]}],
    "UserPromptSubmit": [{"matcher": "x", "hooks": [{"type": "command", "command": "other"}]}]
  }
}`
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	added, err := mergeHookFile(path)
	if err != nil || !added {
		t.Fatalf("first merge: added=%v err=%v", added, err)
	}
	added, err = mergeHookFile(path)
	if err != nil || added {
		t.Fatalf("second merge: added=%v err=%v", added, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if b[len(b)-1] != '\n' {
		t.Fatal("merged JSON lacks trailing newline")
	}
	root := map[string]any{}
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	if root["theme"] != "dark" || !hasHook(root) {
		t.Fatalf("merge clobbered keys or missed hook: %s", b)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 640", info.Mode().Perm())
	}
}

func TestCodexHookTrustedRequiresOneMatchingStateKey(t *testing.T) {
	dir := t.TempDir()
	hooks := filepath.Join(dir, "hooks.json")
	config := filepath.Join(dir, "config.toml")
	content := "[hooks.state]\n\n[hooks.state.\"" + hooks + ":user_prompt_submit:0:0\"]\ntrusted_hash = \"sha256:abc\"\n"
	if err := os.WriteFile(config, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	trusted, err := codexHookTrusted(config, hooks)
	if err != nil || !trusted {
		t.Fatalf("trusted=%v err=%v", trusted, err)
	}
}

package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ramsrib/later/internal/store"
)

// Claude Code injects a hook's bare stdout as context. Codex parses stdout as
// JSON and fails the hook on anything else, so it gets the envelope form.
const (
	claudeHookCommand = "later check --quiet-if-empty"
	codexHookCommand  = "later check --hook-json"
)

func (a *app) install(args []string, _ *store.Store) error {
	fs := newFlagSet("install", `later install (--claude | --codex | --all)`, a.stderr)
	claude := fs.Bool("claude", false, "install the Claude Code UserPromptSubmit hook")
	codex := fs.Bool("codex", false, "install the Codex UserPromptSubmit hook")
	all := fs.Bool("all", false, "install both agent hooks")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	selected := 0
	for _, value := range []bool{*claude, *codex, *all} {
		if value {
			selected++
		}
	}
	if selected != 1 {
		return fmt.Errorf("choose exactly one of --claude, --codex, or --all")
	}
	if *claude || *all {
		path, err := claudeSettingsPath()
		if err != nil {
			return err
		}
		added, err := mergeHookFile(path, claudeHookCommand)
		if err != nil {
			return fmt.Errorf("Claude hook: %w", err)
		}
		if added {
			fmt.Fprintf(a.stdout, "Claude hook added to %s.\n", path)
		} else {
			fmt.Fprintf(a.stdout, "Claude hook already present in %s; no change.\n", path)
		}
	}
	if *codex || *all {
		path, err := codexHooksPath()
		if err != nil {
			return err
		}
		added, err := mergeHookFile(path, codexHookCommand)
		if err != nil {
			return fmt.Errorf("Codex hook: %w", err)
		}
		if added {
			fmt.Fprintf(a.stdout, "Codex hook written to %s.\n", path)
		} else {
			fmt.Fprintf(a.stdout, "Codex hook already present in %s; no config change.\n", path)
		}
		fmt.Fprintln(a.stdout, "Codex will silently ignore this hook until you approve its trust prompt in your next interactive Codex session. Run `later doctor` afterward to confirm it is trusted and active.")
	}
	return nil
}

func claudeSettingsPath() (string, error) {
	if path := os.Getenv("LATER_CLAUDE_SETTINGS"); path != "" {
		return filepath.Abs(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

func codexHooksPath() (string, error) {
	if path := os.Getenv("LATER_CODEX_HOOKS"); path != "" {
		return filepath.Abs(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "hooks.json"), nil
}

func codexConfigPath() (string, error) {
	if path := os.Getenv("LATER_CODEX_CONFIG"); path != "" {
		return filepath.Abs(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "config.toml"), nil
}

func mergeHookFile(path, hookCommand string) (bool, error) {
	root := map[string]any{}
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, &root); err != nil {
			return false, fmt.Errorf("parse %s: %w", path, err)
		}
		if root == nil {
			root = map[string]any{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	if hasHook(root, hookCommand) {
		return false, nil
	}
	// An earlier later wrote the other agent's command here; rewrite it in place
	// rather than leaving a second, wrong-shaped hook behind.
	if upgradeHookCommand(root, hookCommand) {
		if err := writeIndentedJSON(path, root); err != nil {
			return false, err
		}
		return true, nil
	}
	hooks, ok := root["hooks"].(map[string]any)
	if !ok {
		if _, exists := root["hooks"]; exists {
			return false, fmt.Errorf("%s: hooks must be a JSON object", path)
		}
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	event, ok := hooks["UserPromptSubmit"].([]any)
	if !ok {
		if _, exists := hooks["UserPromptSubmit"]; exists {
			return false, fmt.Errorf("%s: hooks.UserPromptSubmit must be a JSON array", path)
		}
		event = []any{}
	}
	entry := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": hookCommand}}}
	hooks["UserPromptSubmit"] = append(event, entry)
	if err := writeIndentedJSON(path, root); err != nil {
		return false, err
	}
	return true, nil
}

func hasHook(root map[string]any, hookCommand string) bool {
	found := false
	eachHookCommand(root, func(command map[string]any) {
		if command["command"] == hookCommand {
			found = true
		}
	})
	return found
}

// upgradeHookCommand rewrites any later-owned command that is not the one this
// agent needs, and reports whether it changed anything.
func upgradeHookCommand(root map[string]any, hookCommand string) bool {
	changed := false
	eachHookCommand(root, func(command map[string]any) {
		current, ok := command["command"].(string)
		if !ok || !isLaterHookCommand(current) {
			return
		}
		command["command"] = hookCommand
		changed = true
	})
	return changed
}

func isLaterHookCommand(command string) bool {
	return command == claudeHookCommand || command == codexHookCommand
}

func eachHookCommand(root map[string]any, visit func(map[string]any)) {
	hooks, ok := root["hooks"].(map[string]any)
	if !ok {
		return
	}
	event, ok := hooks["UserPromptSubmit"].([]any)
	if !ok {
		return
	}
	for _, rawEntry := range event {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			continue
		}
		commands, ok := entry["hooks"].([]any)
		if !ok {
			continue
		}
		for _, rawCommand := range commands {
			if command, ok := rawCommand.(map[string]any); ok {
				visit(command)
			}
		}
	}
}

func writeIndentedJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp := path + ".later.tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func hookPresentAt(path, hookCommand string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	root := map[string]any{}
	if err := json.Unmarshal(b, &root); err != nil {
		return false, err
	}
	return hasHook(root, hookCommand), nil
}

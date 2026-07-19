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

const hookCommand = "later check --quiet-if-empty"

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
		added, err := mergeHookFile(path)
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
		added, err := mergeHookFile(path)
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

func mergeHookFile(path string) (bool, error) {
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
	if hasHook(root) {
		return false, nil
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

func hasHook(root map[string]any) bool {
	hooks, ok := root["hooks"].(map[string]any)
	if !ok {
		return false
	}
	event, ok := hooks["UserPromptSubmit"].([]any)
	if !ok {
		return false
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
			command, ok := rawCommand.(map[string]any)
			if ok && command["command"] == hookCommand {
				return true
			}
		}
	}
	return false
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

func hookPresentAt(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	root := map[string]any{}
	if err := json.Unmarshal(b, &root); err != nil {
		return false, err
	}
	return hasHook(root), nil
}

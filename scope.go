package main

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
	if value := os.Getenv("CLAUDE_PROJECT_DIR"); value != "" {
		return canonicalPath(value)
	}
	if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		if root := strings.TrimSpace(string(out)); root != "" {
			return canonicalPath(root)
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return canonicalPath(cwd)
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

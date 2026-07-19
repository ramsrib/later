package setup

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ramsrib/later/internal/store"
)

func (a *app) doctor(args []string, queue *store.Store) int {
	fs := newFlagSet("doctor", `later doctor`, a.stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(a.stderr, "later doctor: %v\n", err)
		return 1
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintf(a.stderr, "later doctor: unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		return 1
	}
	failures := 0
	items, skipped, err := queue.Read()
	if err != nil {
		a.doctorLine(false, "store", "%v", err)
		failures++
	} else if _, err := os.Stat(queue.Path()); err != nil {
		// Not a failure: the store is created on first write, so its absence
		// means "nothing scheduled yet", which is the normal state for a fresh
		// install. Reporting it as broken would make every first run look wrong.
		a.doctorLine(true, "store", "no reminders scheduled yet (%s)", queue.Path())
	} else if skipped != 0 {
		a.doctorLine(false, "store", "readable, but skipped %d corrupt line(s)", skipped)
		failures++
	} else {
		a.doctorLine(true, "store", "%d record(s), 0 corrupt lines", len(items))
	}
	if _, err := os.Stat(queue.TemporaryPath()); err == nil {
		a.doctorLine(false, "stale .tmp", "present: %s (another command will remove it on startup)", queue.TemporaryPath())
		failures++
	} else if errors.Is(err, os.ErrNotExist) {
		a.doctorLine(true, "stale .tmp", "absent")
	} else {
		a.doctorLine(false, "stale .tmp", "%v", err)
		failures++
	}

	claudePath, _ := claudeSettingsPath()
	if present, err := hookPresentAt(claudePath); err != nil || !present {
		if err == nil {
			err = errors.New("command not found in UserPromptSubmit hooks")
		}
		a.doctorLine(false, "Claude hook", "%s: %v", claudePath, err)
		failures++
	} else {
		a.doctorLine(true, "Claude hook", "present in %s", claudePath)
	}

	codexPath, _ := codexHooksPath()
	codexPresent, codexErr := hookPresentAt(codexPath)
	if codexErr != nil || !codexPresent {
		if codexErr == nil {
			codexErr = errors.New("command not found in UserPromptSubmit hooks")
		}
		a.doctorLine(false, "Codex hook", "%s: %v", codexPath, codexErr)
		failures++
	} else {
		a.doctorLine(true, "Codex hook", "present in %s", codexPath)
	}
	if !codexPresent {
		a.doctorLine(false, "Codex trust", "cannot be trusted until the hook exists")
		failures++
	} else {
		configPath, _ := codexConfigPath()
		trusted, err := codexHookTrusted(configPath, codexPath)
		if err != nil || !trusted {
			if err == nil {
				err = errors.New("hook exists but is untrusted; Codex silently skips untrusted hooks. Approve it in an interactive Codex session")
			}
			a.doctorLine(false, "Codex trust", "%v", err)
			failures++
		} else {
			a.doctorLine(true, "Codex trust", "user_prompt_submit entry is trusted")
		}
	}

	overdue := 0
	threshold := a.now().Add(-30 * 24 * time.Hour)
	for _, item := range items {
		if item.DoneAt == nil && item.NotBefore.Before(threshold) {
			overdue++
		}
	}
	if overdue != 0 {
		a.doctorLine(false, "old overdue", "%d outstanding item(s) are more than 30 days overdue; hooks may not be consuming the queue", overdue)
		failures++
	} else {
		a.doctorLine(true, "old overdue", "0 outstanding items older than 30 days")
	}
	return min(failures, 125)
}

func (a *app) doctorLine(pass bool, name, format string, values ...any) {
	status := "PASS"
	if !pass {
		status = "FAIL"
	}
	fmt.Fprintf(a.stdout, "%s %-13s %s\n", status, name, fmt.Sprintf(format, values...))
}

func codexHookTrusted(configPath, hooksPath string) (bool, error) {
	b, err := os.ReadFile(configPath)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", configPath, err)
	}
	wantPath := strings.ToLower(hooksPath)
	for _, line := range strings.Split(strings.ToLower(string(b)), "\n") {
		line = strings.TrimSpace(line)
		// Codex records trust in a nested state-table key whose name includes
		// the source hooks file and normalized event name.
		if strings.HasPrefix(line, "[hooks.state.") && strings.Contains(line, wantPath) && strings.Contains(line, "user_prompt_submit") {
			return true, nil
		}
	}
	return false, nil
}

package setup

import (
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/ramsrib/later/internal/store"
)

type app struct {
	stdout io.Writer
	stderr io.Writer
	now    func() time.Time
}

// Run dispatches a setup or maintenance command.
func Run(name string, args []string, queue *store.Store, stdout, stderr io.Writer) error {
	a := &app{stdout: stdout, stderr: stderr, now: time.Now}
	switch name {
	case "install":
		return a.install(args, queue)
	case "migrate":
		return a.migrate(args, queue)
	case "rescope":
		return a.rescope(args, queue)
	default:
		return fmt.Errorf("unknown setup command %q", name)
	}
}

// Doctor runs all diagnostic checks and returns their process exit code.
func Doctor(args []string, queue *store.Store, stdout, stderr io.Writer) int {
	a := &app{stdout: stdout, stderr: stderr, now: time.Now}
	return a.doctor(args, queue)
}

func newFlagSet(name, usage string, out io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("later "+name, flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() {
		fmt.Fprintf(out, "Usage: %s\n\n%s\n\nOptions:\n", usage, commandDescriptions[name])
		fs.PrintDefaults()
	}
	return fs
}

var commandDescriptions = map[string]string{
	"install": "Merge the prompt hook into Claude Code and/or Codex settings.",
	"doctor":  "Diagnose the store, hooks, Codex trust, and stale reminders.",
	"migrate": "Import reminders from the Python v1 Claude project mailboxes.",
	"rescope": "Rewrite matching project-scope path prefixes.",
}

func requireArgs(fs *flag.FlagSet, got []string, n int, names string) error {
	if len(got) != n {
		fs.Usage()
		return fmt.Errorf("expected %s", names)
	}
	return nil
}

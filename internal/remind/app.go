package remind

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ramsrib/later/internal/store"
)

type app struct {
	stdout io.Writer
	stderr io.Writer
	now    func() time.Time
}

// Run dispatches a reminder command.
func Run(name string, args []string, queue *store.Store, stdout, stderr io.Writer) error {
	a := &app{stdout: stdout, stderr: stderr, now: time.Now}
	switch name {
	case "add":
		return a.add(args, queue)
	case "check":
		return a.check(args, queue)
	case "list":
		return a.list(args, queue)
	case "show":
		return a.show(args, queue)
	case "done":
		return a.done(args, queue)
	case "cancel":
		return a.cancel(args, queue)
	default:
		return fmt.Errorf("unknown reminder command %q", name)
	}
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
	"add":    "Schedule a message for a future agent session.",
	"check":  "Print reminders due in this project or globally (hook hot path).",
	"list":   "List outstanding reminders, ordered by urgency.",
	"show":   "Show the complete stored record for one reminder.",
	"done":   "Complete a one-shot reminder or advance a recurring reminder.",
	"cancel": "Permanently remove a reminder.",
}

func requireArgs(fs *flag.FlagSet, got []string, n int, names string) error {
	if len(got) != n {
		fs.Usage()
		return fmt.Errorf("expected %s", names)
	}
	return nil
}

func parseBody(value string) (string, error) {
	if value != "-" {
		return value, nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read body from stdin: %w", err)
	}
	return strings.TrimSuffix(string(b), "\n"), nil
}

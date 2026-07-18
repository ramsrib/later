package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const version = "2.0.0"

type app struct {
	stdout io.Writer
	stderr io.Writer
	now    func() time.Time
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	a := &app{stdout: stdout, stderr: stderr, now: time.Now}
	if len(args) == 0 {
		a.printHelp(stderr)
		return 2
	}

	// Doctor must see a stale temporary file in order to diagnose it. Every
	// other command removes one left by an interrupted writer at startup.
	cleanup := args[0] != "doctor" && !(args[0] == "migrate" && containsArg(args[1:], "--dry-run"))
	store, err := openStore(cleanup)
	if err != nil {
		if args[0] == "check" {
			fmt.Fprintf(stderr, "later check: %v\n", err)
			return 0
		}
		fmt.Fprintf(stderr, "later: %v\n", err)
		return 1
	}

	var code int
	switch args[0] {
	case "help", "--help", "-h":
		a.printHelp(stdout)
	case "version", "--version":
		fmt.Fprintln(stdout, version)
	case "add":
		code = a.command(args[0], args[1:], store, a.add)
	case "check":
		// A reminder helper must never prevent the user's prompt from reaching
		// the agent, including when flags or the store are broken.
		if err := a.check(args[1:], store); err != nil && !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(stderr, "later check: %v\n", err)
		}
		code = 0
	case "list":
		code = a.command(args[0], args[1:], store, a.list)
	case "show":
		code = a.command(args[0], args[1:], store, a.show)
	case "done":
		code = a.command(args[0], args[1:], store, a.done)
	case "cancel":
		code = a.command(args[0], args[1:], store, a.cancel)
	case "install":
		code = a.command(args[0], args[1:], store, a.install)
	case "doctor":
		code = a.doctor(args[1:], store)
	case "migrate":
		code = a.command(args[0], args[1:], store, a.migrate)
	case "rescope":
		code = a.command(args[0], args[1:], store, a.rescope)
	default:
		fmt.Fprintf(stderr, "later: unknown command %q\n\n", args[0])
		a.printHelp(stderr)
		code = 2
	}
	return code
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

type commandFunc func([]string, *Store) error

func (a *app) command(name string, args []string, store *Store, fn commandFunc) int {
	if err := fn(args, store); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(a.stderr, "later %s: %v\n", name, err)
		return 1
	}
	return 0
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
	"add":     "Schedule a message for a future agent session.",
	"check":   "Print reminders due in this project or globally (hook hot path).",
	"list":    "List outstanding reminders, ordered by urgency.",
	"show":    "Show the complete stored record for one reminder.",
	"done":    "Complete a one-shot reminder or advance a recurring reminder.",
	"cancel":  "Permanently remove a reminder.",
	"install": "Merge the prompt hook into Claude Code and/or Codex settings.",
	"doctor":  "Diagnose the store, hooks, Codex trust, and stale reminders.",
	"migrate": "Import reminders from the Python v1 Claude project mailboxes.",
	"rescope": "Rewrite matching project-scope path prefixes.",
}

func (a *app) printHelp(w io.Writer) {
	fmt.Fprintln(w, `later stores messages for a future Claude Code or Codex session.

Usage:
  later <command> [options]

Commands:
  add       Schedule a reminder
  check     Print reminders currently due (used by hooks)
  list      List outstanding reminders
  show      Show one reminder in full
  done      Handle a reminder (recurring items advance)
  cancel    Permanently remove a reminder
  install   Merge agent prompt hooks
  doctor    Diagnose silent failures
  migrate   Import the Python v1 mailbox layout
  rescope   Rewrite project path prefixes

Run "later <command> --help" for command-specific help.`)
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

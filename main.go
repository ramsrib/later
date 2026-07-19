package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ramsrib/later/internal/remind"
	"github.com/ramsrib/later/internal/setup"
	"github.com/ramsrib/later/internal/store"
)

// Stamped at build time by the release script: -X main.version=v0.1.0.
// "dev" is what a plain `go build` reports.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printHelp(stderr)
		return 2
	}

	// Doctor must see a stale temporary file in order to diagnose it. Every
	// other command removes one left by an interrupted writer at startup.
	cleanup := args[0] != "doctor" && !(args[0] == "migrate" && containsArg(args[1:], "--dry-run"))
	queue, err := store.Open(cleanup)
	if err != nil {
		if args[0] == "check" {
			fmt.Fprintf(stderr, "later check: %v\n", err)
			return 0
		}
		fmt.Fprintf(stderr, "later: %v\n", err)
		return 1
	}

	switch args[0] {
	case "help", "--help", "-h":
		printHelp(stdout)
		return 0
	case "version", "--version":
		fmt.Fprintln(stdout, version)
		return 0
	case "check":
		// A reminder helper must never prevent the user's prompt from reaching
		// the agent, including when flags or the store are broken.
		if err := remind.Run(args[0], args[1:], queue, stdout, stderr); err != nil && !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(stderr, "later check: %v\n", err)
		}
		return 0
	case "add", "list", "show", "done", "cancel":
		return command(args[0], remind.Run(args[0], args[1:], queue, stdout, stderr), stderr)
	case "install", "migrate", "rescope":
		return command(args[0], setup.Run(args[0], args[1:], queue, stdout, stderr), stderr)
	case "doctor":
		return setup.Doctor(args[1:], queue, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "later: unknown command %q\n\n", args[0])
		printHelp(stderr)
		return 2
	}
}

func command(name string, err error, stderr io.Writer) int {
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return 0
	}
	fmt.Fprintf(stderr, "later %s: %v\n", name, err)
	return 1
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func printHelp(w io.Writer) {
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

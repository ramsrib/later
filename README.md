# later

`later` is a durable local reminder queue for coding-agent sessions. It lets Claude Code or Codex leave a message for a future session in the same project—or in every project—and surfaces the message the next time a live session receives a prompt after it is due.

It is deliberately not a notification service or a job runner. Nothing runs in the background, and nothing tries to wake a sleeping laptop. A prompt hook reads the queue; if a reminder's time has arrived, the hook adds a short pointer to the active agent context. The full body remains on disk until someone asks to see it.

## Install

With Homebrew:

```sh
brew install ramsrib/tap/later
```

Or directly with Go 1.24 or newer:

```sh
go install github.com/ramsrib/later@latest
```

Then install one or both prompt hooks:

```sh
later install --claude
later install --codex
# or: later install --all
```

Hook installation merges into existing settings instead of replacing them. Claude Code uses `~/.claude/settings.json`; Codex uses `~/.codex/hooks.json`.

The two agents get different commands. Claude Code injects a hook's bare stdout as context, so it runs `later check --quiet-if-empty`. Codex parses hook stdout strictly as JSON and fails the hook on anything else — `hook returned invalid user prompt submit JSON output` — so it runs `later check --hook-json`, which wraps the same summary in the `UserPromptSubmit` envelope. Installing over a hooks file that carries the other command rewrites it in place.

Codex requires interactive trust approval for a new hook. Until it is approved, Codex silently skips the hook even though the configuration is present. Start an interactive Codex session, approve the hook, then run:

```sh
later doctor
```

## Quick start

Schedule a project reminder:

```sh
later add --subject "Check the rollout metrics" --in 3h
```

Add detail from standard input and use an exact local time:

```sh
printf '%s\n' 'Compare error rate and p95 latency with the previous release.' |
  later add --subject "Review rollout" --body - --at '2026-08-03 09:30'
```

Schedule a global recurring reminder:

```sh
later add --subject "Review open maintenance work" --in 1w --recur 1w --global
```

When an item appears, inspect and handle it:

```sh
later show review-rollout-a1b2c3
later done review-rollout-a1b2c3
```

`done` completes a one-shot item. For a recurring item it advances the same record to the first occurrence strictly after the current time, collapsing any missed occurrences. `cancel` permanently removes either kind.

### Stable ids, for scripts that report a condition

A script that checks something on a schedule (a CI queue, free disk, a broken credential) wants one reminder per condition, not one per run. Give it a fixed `--id`:

- `add --id X` while `X` is outstanding is refused, so repeat runs collapse into the first report.
- `add --id X --replace` overwrites the outstanding `X` with the new record, keeping only its original creation time. Everything else comes from this call, omitted options included: leave out `--global` or `--recur` and the replacement is project-scoped or one-shot. Use it when the latest report supersedes the earlier one, such as a queue whose contents change daily. Pass a short `--in` (say `1m`): a frequent job replacing with `--in 2h` pushes the due time back on every run and never comes due.
- Once `X` is done, `add --id X` re-arms it. A handled condition that comes back is reported again.
- When the script sees the condition clear, `later done X` retires a one-shot `X` (ignore the error if there is nothing to retire). A recurring one only advances; `cancel` ends it.

## Commands

```text
later add --subject S [--body B|-] (--at TIME | --in DUR) [--recur DUR] [--global] [--id ID [--replace]]
later check [--quiet-if-empty] [--json] [--hook-json]
later list [--all] [--here] [--json]
later show <id>
later done <id>
later cancel <id>
later install (--claude | --codex | --all)
later doctor
later migrate [--dry-run]
later rescope <old-path> <new-path>
```

Durations are a positive integer plus one unit: `m`, `h`, `d`, or `w`. Months and years are intentionally unsupported because their lengths are irregular. `--at` accepts RFC3339 or `YYYY-MM-DD HH:MM` in local time.

`list` shows outstanding items for the current project plus global items. Use `--here` to exclude global items or `--all` for a cross-project view. `check` is the quiet, failure-tolerant hook path; it always exits successfully so a broken reminder store cannot block an agent prompt.

`doctor` checks store parsing, interrupted writes, both hook configurations, Codex trust state, and reminders overdue by more than 30 days. `migrate --dry-run` previews import from the earlier Claude mailbox layout; migration is safe to repeat because existing IDs are skipped.

Run `later <command> --help` for the options and purpose of any command.

## Scope and storage

Project scope is resolved consistently under both agents: an explicit `LATER_SCOPE`; otherwise `CLAUDE_PROJECT_DIR` (or the current directory) mapped to its repository's main checkout, so a reminder added in a linked worktree belongs to the repo and outlives the worktree; outside Git, the directory itself. `--global` makes an item visible everywhere.

Records live in `~/.later/items.jsonl`, one JSON object per line. Set `LATER_STORE` to use another file. Writes create the parent directory on demand.

## Why it works this way

There is no scheduler. Due status is simply `now >= not_before` when a prompt hook reads the queue. Sleep, reboot, and long gaps between sessions therefore make reminders late but do not make them disappear.

JSON Lines keeps the queue transparent, portable, and easy to recover without introducing a database dependency. Writers use an advisory lock, preserve one backup generation, fsync a complete temporary file, and atomically rename it into place. Readers take no lock, so the per-prompt hot path never waits on a writer. A corrupt line is skipped without hiding the rest of the queue.

Delivery is at least once: a due reminder continues to surface until it is marked done, advanced, or cancelled. That is preferable to silently losing a reminder after a best-effort delivery attempt.

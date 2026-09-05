# M9 — Open in $EDITOR (v1.3)

## Goal

The user can open the highlighted entry in their editor with `o` from Home, the Package main
context and Issues, without leaving stower; when the editor exits the TUI resumes and
re-scans, so a save that replaced a symlink with a regular file shows up in Issues right away.

## Depends on

M8.

## Scope

- `internal/config`: `Editor() ([]string, error)` resolving `$VISUAL`, then `$EDITOR`, then
  `vi` if either is found in `PATH`. The value is split into argv with a small quote-aware
  splitter (`"code --wait"`, `'nvim -u "~/x"'` style) and never passed through a shell. Table
  tests for precedence, quoting, unset variables and a missing binary.
- `internal/tui/app.go`: key `o` in Home opens the highlighted file or directory in the target
  (managed entries open their target path, so the editor follows the symlink into the repo,
  which is what the user expects); in the Package main context `o` opens the repository copy
  (`Entry.PackagePath`), and `shift+o` opens the target path; in Issues `o` opens the repository
  copy and `shift+o` the target copy, so both sides of a `replaced` entry are reachable.
  Directories are passed as-is (editors like nvim and VS Code accept a directory).
- Suspend and resume through Bubble Tea's process execution (`tea.ExecProcess` or the v2
  equivalent) so the alternate screen is released while the editor runs and restored after.
  The returned message triggers `refreshCmd`; if the editor exited non-zero, show the exit code
  in the flash line, not an error popup.
- `o` is disabled while an operation runs or a popup is open. The key bar and the `?` popup
  list it for the three contexts.
- Tests: config splitter tests; a model test that injects a fake exec function recording the
  argv and path for each context; a test that a missing editor produces a flash line and no
  process start.

## Out of scope

Editing inside the TUI, opening multiple entries at once, diff-in-editor for `replaced`
entries, a configurable key or editor in a stower config file.

## Acceptance

- With `EDITOR="code --wait"`, `o` on `~/.zshrc` in Home starts `code --wait <target>/.zshrc`
  and the TUI is fully restored after the editor exits.
- `o` on a `replaced` issue opens the repository copy, `shift+o` the target copy.
- Unsetting both variables and having no `vi` in `PATH` shows
  `no editor: set $EDITOR or $VISUAL` in the flash line.
- After the editor exits, Packages and Issues reflect any change the editor made to the link
  state.

## Verification

```
gofmt -l .
go vet ./...
go build ./...
go test ./...
```

## Notes

- The atomic-save case from the user's real `~/.claude/settings.json` (editor writes tmp +
  rename, turning the symlink into a regular file) is the reason for the automatic re-scan;
  add a test that simulates it by replacing a link with a file inside the fake exec callback and
  asserts the entry is reported `replaced` afterwards.
- Record how Bubble Tea v2 names the exec-process API and message in the pinned version.

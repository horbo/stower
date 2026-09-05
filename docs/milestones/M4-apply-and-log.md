# M4 — Apply and log

## Goal

The user can apply the staged plan: confirm, watch the live log of moves and stow output,
see per-package success or rollback, and end up with files moved into the repository and
linked back into the target.

## Depends on

M3.

## Scope

- `popups/confirm.go`: generic confirmation popup with title, body lines, an "undo" hint line,
  `y` / `enter` confirm, `n` / `esc` cancel. Reused by M5 and M6.
- `main/log.go`: viewport-backed live log fed by `dotfiles.Event` values, rendering
  `✔` / `✘` / `↩` prefixes and indented stow output, auto-scroll while running, manual scroll
  after completion, per-package summary at the end.
- Execution wiring in `app.go`: `enter` in Staged → Confirm (summary: N items, M packages,
  blocked entries excluded, undo hint "restore the package with r") → run
  `dotfiles.Execute` in a goroutine started by a `tea.Cmd`, forwarding events as messages →
  main switches to Log for the duration → on completion successful packages are removed from
  staging, failed ones stay staged with the failure reason shown in the Staged panel → every
  panel refreshes (`ListPackages`, `WalkPackage`, `ManagedBy` badges).
- Keys are ignored while an operation runs, except `ctrl+c`, which cancels the context; a
  cancelled operation still completes its rollback before the log reports it.
- `popups/error.go`: title and scrollable stderr for unexpected errors (for example the
  cross-device check), `enter` / `esc` closes.
- `panels/status.go`: shows `running…` with a spinner while an operation is in progress.

## Out of scope

Restore, doctor, fixes, git commit popup, the `x` context menu, subcommands.

## Acceptance

- Manual scenario below ends with `~/.bar` and `~/.config/foo` as symlinks into the
  repository, the Staged panel empty, Packages showing `misc` and `foo` with `✔`.
- A forced conflict (create `$FAKE/dotfiles/misc/dot-bar` by hand before applying) leaves
  `~/.bar` untouched, shows the rollback in the log, and keeps the entry staged with a reason.
- Log reducer tests: event sequence → rendered lines, including rollback ordering.

## Verification

```
gofmt -l .
go vet ./...
go build ./...
go test ./...
FAKE=$(mktemp -d); mkdir -p "$FAKE/.config/foo" "$FAKE/dotfiles"
echo x > "$FAKE/.bar"; echo y > "$FAKE/.config/foo/conf"
go run ./cmd/stower --target "$FAKE" --dotfiles "$FAKE/dotfiles"
ls -la "$FAKE" "$FAKE/.config"; find "$FAKE/dotfiles" -type f
```

## Notes

- M3 built the staging model, the Staged panel and `main/stagedplan.go`; `app.go` routes main
  contexts through `mainContext()` / `mainPanel()` by `m.focus`. Add the Log context there.
  Popups are a `popup` enum in `app.go` (`popupNone`, `popupKeys`, `popupAssign`); add
  `popupConfirm` and `popupError` the same way. Panels needing all keys implement
  `CapturesInput() bool`.
- `dotfiles.Execute` takes an `events chan<- Event` and never closes it; bridge it to Bubble Tea
  with a goroutine started from a `tea.Cmd` that forwards each event as a message and sends a
  final done message when `Execute` returns.
- Optional if time allows, otherwise leave for later: move the Home entry inspection
  (`HasNestedGit` + counting) off the UI goroutine into a `tea.Cmd`; see DESIGN.md
  "Known performance debt".

- Report how long the refresh after an operation takes on a target with a few hundred
  top-level entries and whether it needs to move off the UI goroutine.

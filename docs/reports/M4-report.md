# M4 implementation report

Completed in `/private/tmp/stower-workspace.QIrf2v`, copied with Git history and all uncommitted changes from the original workspace. No commits were created. `STATUS.md` retains the copied coordinating-session state.

## Changes

- Connected confirmation, asynchronous execution, ordered event delivery, live log, cancellation, spinner, and error popup rendering.
- Successful moves leave staging; blocked entries in the same package remain. Failed packages retain their reasons. Package health and Home managed badges refresh after completion.
- Preserved the existing `tui.New(paths, stowVersion)` interface. Added execution wiring in `execution.go`.
- Cancellation is checked before stow steps and after restow. Rollback events are delivered even after cancellation, and completion follows the entire event stream.
- Added reducer, scrolling, confirmation, cancellation, conflict, partial-success, blocked-entry, real-stow integration, and refresh timing tests.

## Verification

`gofmt -l .`, `git diff --check`, `go vet ./...`, and `go build ./...` produced no output and exited successfully.

`go test ./...` decisive output:

```text
ok  	github.com/horbo/stower/internal/dotfiles	1.138s
ok  	github.com/horbo/stower/internal/stow	(cached)
ok  	github.com/horbo/stower/internal/tui	6.910s
ok  	github.com/horbo/stower/internal/tui/components	(cached)
ok  	github.com/horbo/stower/internal/tui/main	(cached)
```

Manual terminal scenario used a compiled binary with explicit `--target /private/tmp/stower-manual-3t90o5x6 --dotfiles /private/tmp/stower-manual-3t90o5x6/dotfiles`. Staged `.bar` into `misc`, expanded `.config`, staged `foo` into `foo`, then confirmed Apply. The UI showed the spinner and live output, followed by:

```text
✔ foo
✔ misc
(nothing staged)
✔ 2 packages applied: foo, misc
```

Filesystem inspection confirmed:

```text
.bar -> dotfiles/misc/dot-bar
foo -> ../dotfiles/foo/dot-config/foo
/private/tmp/stower-manual-3t90o5x6/dotfiles/foo/dot-config/foo/conf
/private/tmp/stower-manual-3t90o5x6/dotfiles/misc/dot-bar
```

Real-stow integration tests also verified conflict rollback with an independently successful package, intact source contents, retained staging, and refreshed managed badges.

Race detection also passed with `go test -race ./internal/tui/... ./internal/dotfiles`:

```text
ok  	github.com/horbo/stower/internal/tui	8.402s
ok  	github.com/horbo/stower/internal/tui/components	2.382s
ok  	github.com/horbo/stower/internal/tui/main	2.097s
ok  	github.com/horbo/stower/internal/dotfiles	2.449s
```

## Notes and limitations

Refresh measurement:

```text
500-entry refresh: package scan=157.292µs, UI refresh=675.917µs
```

This small local target does not justify moving refresh off the UI goroutine. Large expanded trees and the existing uncapped nested Git inspection remain performance debt; optional asynchronous Home inspection was deferred.

The runner buffers each stow subprocess's output until that subprocess returns. Cancellation waits for the current subprocess, then rolls back; it does not forcibly terminate stow. The event consumer must continue draining until execution returns so rollback events can be delivered reliably.

A destination conflict known before confirmation is excluded as a blocked entry. A destination created after confirmation is detected during execution and retained with a failure reason. Stow conflicts after moves produce rollback events. This follows the design's exclusion of blocked entries rather than attempting a known-conflicting move solely to display rollback.

Restore, doctor, Git actions, and commits remain outside M4. No additional dependencies were added.

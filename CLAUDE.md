# stower

TUI frontend for GNU Stow that manages a `--dotfiles`-style dotfiles repository:
browse `$HOME`, stage files and directories into packages, move them into the repo
and link them back with stow, restore them, inspect link health, commit to git.

## Stack

- Go, latest stable from Homebrew. Single static binary.
- `charmbracelet/bubbletea`, `charmbracelet/bubbles`, `charmbracelet/lipgloss`.
  Versions are pinned in `go.mod` by M0; do not add any other third-party dependency
  without asking.
- GNU Stow >= 2.4 is the linking backend, invoked as a subprocess. stower never creates
  or removes symlinks itself.
- git is invoked as a subprocess.

## Rules

- English only: identifiers, UI strings, docs, commit messages.
- No code comments. The only exception is godoc on exported identifiers in packages
  that already document their exported API.
- Never run stower, its tests, or manual checks against the real `$HOME` or `~/dotfiles`.
  Every test and manual scenario uses temporary directories passed via `--target`
  and `--dotfiles`.
- Never edit `docs/milestones/STATUS.md`. The coordinating session owns it.
- Do not commit unless explicitly asked. Commit messages are a subject line only.
- Keep the domain layer (`internal/dotfiles`, `internal/stow`, `internal/doctor`,
  `internal/gitx`) free of any TUI import.
- Always call stow with explicit `-d <dotfiles> -t <target>` and `--dotfiles`; never rely
  on the working directory.
- Validate external input (paths, package names). Never interpolate untrusted values into
  a shell string; use `exec.Command` with argument slices.

## Layout

```
cmd/stower/            entry point: flags, stow detection, TUI or subcommand
internal/config/       Paths resolution, flags, environment
internal/dotfiles/     mapping, scanning, plans, transactional execution, validation
internal/stow/         stow subprocess runner and output parsing
internal/doctor/       link health states and fixes
internal/gitx/         git subprocess wrapper
internal/tui/          Bubble Tea application: layout, panels, main contexts, popups, components
docs/DESIGN.md         authoritative design: domain rules, architecture, flows, TUI
docs/milestones/       one file per milestone, README.md for format and process, STATUS.md for state
```

## Verification

Run all of these before reporting a milestone as done and quote the output:

```
gofmt -l .          # must print nothing
go vet ./...
go build ./...
go test ./...
```

Manual checks use a throwaway home:

```
FAKE=$(mktemp -d)
mkdir -p "$FAKE/.config/foo" && echo x > "$FAKE/.bar"
go run ./cmd/stower --target "$FAKE" --dotfiles "$FAKE/dotfiles"
```

# M0 — Bootstrap

## Goal

`stower --version` builds and runs, configuration resolves target and dotfiles paths, and a
missing stow binary produces a clear error. No TUI, no domain logic yet.

## Depends on

Nothing. Go is already installed by the coordinator (`brew install go`).

## Scope

- `go.mod` with module path `github.com/kamilhorbowicz/stower` (renamed to
  `github.com/horbo/stower` during the M2 review once the GitHub repository path was decided;
  M0's own commit still uses the original path). Go version matching the
  installed toolchain.
- Dependencies: `charmbracelet/bubbletea`, `charmbracelet/bubbles`, `charmbracelet/lipgloss`.
  Use the major versions named in `Notes`. Pin exact versions in `go.mod`; run `go mod tidy`.
  A minimal `internal/tui/app.go` that imports bubbletea and returns a trivial `tea.Model` is
  allowed so the dependency stays in `go.mod`; it is not wired to `main` yet.
- `cmd/stower/main.go`: flags `--dotfiles` (default `~/dotfiles`, overridden by env
  `STOWER_DOTFILES`, overridden by the flag), `--target` (default `$HOME`), `--version`.
  Precedence: flag > env > default. `version` is a `var version = "dev"` set through
  `-ldflags "-X main.version=…"`. `stower --version` prints `stower <version>` to stdout.
  Without `--version` the program resolves config, detects stow, prints
  `stower: TUI not implemented yet` to stderr and exits 0 (placeholder until M2).
- `internal/config`: `Paths{Target, Dotfiles string}` with `Resolve(flagDotfiles, flagTarget
  string, env func(string) string) (Paths, error)`: expands a leading `~`, makes both absolute,
  canonicalises with `filepath.EvalSymlinks` when the path exists, and rejects a dotfiles path
  that is not inside or equal to a directory the process can access. `StowBinary() (path,
  version string, err error)` via `exec.LookPath("stow")` and `stow --version` parsing
  (`stow (GNU Stow) version 2.4.1` → `2.4.1`). The error for a missing binary is
  `stow not found in PATH; install it with: brew install stow`.
- `.gitignore`: `/stower`, `/dist/`, `.DS_Store`.
- `Makefile` with `build` (`go build -ldflags "-X main.version=$(VERSION)" -o stower
  ./cmd/stower`, VERSION from `git describe --tags --always --dirty`), `test`, `check`
  (`gofmt -l .` must be empty, `go vet`, `go build`, `go test`).

## Out of scope

Any TUI rendering, any file operation, stow invocations other than `--version`, git
integration, subcommands.

## Acceptance

- `go run ./cmd/stower --version` prints `stower dev`.
- `STOWER_DOTFILES=/tmp/x go run ./cmd/stower --dotfiles /tmp/y` resolves dotfiles to `/tmp/y`
  (flag wins); without the flag it resolves to `/tmp/x`.
- With `PATH` set to a directory without stow the program exits 1 and prints the
  `brew install stow` message to stderr.
- Unit tests in `internal/config`: precedence, `~` expansion, symlink canonicalisation using a
  temporary directory, version string parsing, missing binary error.
- `make check` passes.

## Verification

```
gofmt -l .
go vet ./...
go build ./...
go test ./...
go run ./cmd/stower --version
PATH=/usr/bin:/bin go run ./cmd/stower; echo "exit=$?"
```

## Notes

- Decided by the coordinator on 2026-09-05 from proxy.golang.org: use the v2 majors,
  `github.com/charmbracelet/bubbletea/v2` (latest v2.0.9),
  `github.com/charmbracelet/lipgloss/v2` (latest v2.0.6),
  `github.com/charmbracelet/bubbles/v2` (latest v2.2.1). Go toolchain is 1.27.1. If a v2
  module does not resolve with `go get`, stop and report instead of falling back to v1.
- Report the exact Go version and the exact pinned versions of the three dependencies.

# M6 — Git

## Goal

Every successful operation offers a commit with a sensible subject, packages show their
uncommitted state, and a missing dotfiles directory is created and initialised on first run.
This completes v1.

## Depends on

M5.

## Scope

- `internal/gitx/git.go`: `IsRepo(dir) bool`, `Init(dir) error`, `DirtyPaths(dir, pkg string)
  ([]string, error)` from `git status --porcelain -- <pkg>`, `Porcelain(dir, pkgs ...string)
  ([]string, error)`, `AddAndCommit(dir string, pkgs []string, subject string) error` running
  `git add -A -- <pkgs>` then `git commit -m <subject>`; `Available() bool` via `LookPath`.
  All calls use argument slices. Tests on a temporary repository: init, dirty detection,
  commit, empty-commit rejection, subject with special characters.
- `popups/commit.go`: porcelain listing for the touched packages, `textinput` with the default
  subject (`stower: add zsh (3 files)`, `stower: add git, ghostty (3 files)`,
  `stower: remove zsh`, `stower: fix claude/settings.json`, `stower: restow`), `enter` commits,
  `s` skips, `esc` cancels; shown after successful adopt, restore, fix and restow when the
  dotfiles directory is a repository; `c` in Status or Packages opens it for every dirty
  package.
- `popups/firstrun.go`: shown at start when the dotfiles directory does not exist:
  checkboxes `create <dotfiles>` (mandatory), `git init`, `add .gitignore` (content:
  `.DS_Store`); `enter` applies, `esc` quits. When the directory exists without `.git`, a
  smaller variant offers `git init` once per run and remembers a decline for the session.
- `panels/packages.go`: `*` suffix for packages with dirty paths; `panels/status.go`:
  `git N*` or `git clean`, `no git` when the directory is not a repository.
- Default subject generation as a pure function with table tests.

## Out of scope

Push, branches, commit bodies, security warnings, per-entry restore, brew tap or release
tooling.

## Acceptance

- Manual: fresh `$FAKE` without `dotfiles` → first-run popup creates and initialises it;
  adopting `~/.bar` ends with the commit popup, `enter` produces one commit whose subject is
  `stower: add misc (1 file)`; `git -C "$FAKE/dotfiles" log --oneline` shows it.
- Declining `git init` keeps stower working with `no git` in Status and no commit popups.
- gitx tests pass; subject tests pass.

## Verification

```
gofmt -l .
go vet ./...
go build ./...
go test ./...
FAKE=$(mktemp -d); echo x > "$FAKE/.bar"
go run ./cmd/stower --target "$FAKE" --dotfiles "$FAKE/dotfiles"
git -C "$FAKE/dotfiles" log --oneline; git -C "$FAKE/dotfiles" status --short
```

## Notes

- Report whether `git commit` needs `user.name` / `user.email` configured in the test
  environment and how the tests handle it (set them per test repository, never globally).

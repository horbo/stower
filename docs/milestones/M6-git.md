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

- State of the code after M4+M5 (implemented externally, reviewed and committed as one commit):
  popups are a `popup` enum in `internal/tui/app.go` (`popupNone`, `popupKeys`, `popupAssign`,
  `popupConfirm`, `popupError`, `popupFix`); add `popupCommit` and `popupFirstRun` the same way.
  `popups.Confirm.Open(action, title, body []string, undoHint)` is generic; confirmed actions are
  dispatched in `startConfirmed(action)` in `internal/tui/maintenance.go` (`actionApply`,
  `actionRestore`, `actionFix`, `actionRestow`). Long-running work goes through
  `beginOperation(title, plan, work)` in `internal/tui/execution.go`, which owns the Log context,
  the spinner in Status and the final `refreshCmd`. Hook the Commit popup into the completion
  path (`finishExecution`) when the summary has at least one success and the dotfiles directory
  is a git repository.
- `internal/doctor` exists (`Inspect`, `InspectPackage`, `Fix`, `RestowPackages`, `Diff`);
  `panels.Packages` items already carry doctor health, add the `*` dirty marker next to it.
  `panels.Status` renders one line; extend it with the git summary.
- `cmd/stower/main.go` already parses the `status` and `restow` subcommands; the first-run popup
  belongs in the TUI path only, the subcommands must keep working without git.
- Reports from external agents live in `docs/reports/`; an Opus agent reports in the conversation.

- Report whether `git commit` needs `user.name` / `user.email` configured in the test
  environment and how the tests handle it (set them per test repository, never globally).

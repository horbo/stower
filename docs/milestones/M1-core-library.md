# M1 — Core library

## Goal

The domain layer can plan and execute adopt and restore operations transactionally against a
temporary target and dotfiles directory, driven by the real stow binary, with no TUI.

## Depends on

M0.

## Scope

All in `internal/dotfiles` and `internal/stow`, per the "Domain rules" and "Flows" sections of
`docs/DESIGN.md`.

- `mapping.go`: `TargetToPackage(pkg, relTarget string) string` and
  `PackageToTarget(relPkg string) string` implementing the first-component-only `dot-` rule,
  plus `PackageOf(relDotfiles string) string`. Table-driven tests for both directions, including
  deeper literal dots (`.config/nvim/.gitignore`), non-dot top-level entries, and round trips.
- `scan.go`: `ListPackages(paths) ([]string, error)` (top-level directories, skipping names
  starting with `.`); `WalkPackage(paths, pkg) ([]Entry, error)` returning link points with
  `Entry{PkgRel, TargetRel, IsDir bool, State}` where `State` is one of `Linked`, `Unlinked`,
  `Conflict`, descending into unfolded real directories and stopping at link points;
  `ManagedBy(paths, targetPath string) (pkg string, ok bool)` resolving relative symlinks
  against the link's parent and canonicalising both sides with `EvalSymlinks`.
- `stow/runner.go`: `Runner{Bin, Dotfiles, Target string}` with `DryRunRestow(pkg)`,
  `Restow(pkgs ...string)`, `Unstow(pkg)`, each returning `Result{Args []string, Stdout,
  Stderr string, Conflicts []string, Err error}`. Always pass `--dotfiles -v -d -t`. Parse
  conflict lines from stderr. Use `exec.Command` with argument slices only.
- `plan.go`: `Staging map[string]string` (absolute target path → package);
  `BuildAdoptPlan(paths, staging) AdoptPlan` grouped by package with `Move{From, To}`,
  `ExpectedLinks`, `Warnings` (nested `.git` with a `RemoveNestedGit` toggle), `Blocked`
  entries with reasons; `BuildRestorePlan(paths, pkg) RestorePlan` from `WalkPackage`, blocked
  when any entry is `Conflict`.
- `validate.go`: the rules listed in DESIGN.md "Adopt" step 3, including package name
  validation, symlink rejection, descendant de-duplication, and the same-device check
  (`syscall.Stat_t.Dev`, unix build tag, with a function that takes two `os.FileInfo` so it can
  be unit-tested without real devices).
- `exec.go`: `Execute(ctx, plan, runner, events chan<- Event) Summary` for adopt and
  `ExecuteRestore(...)` for restore, one transaction per package, move journal, rollback on
  dry-run conflict or stow failure, removal of created empty directories. `Event` variants:
  `StepStarted`, `OutputLine`, `StepDone`, `StepFailed`, `Rollback`, `PackageDone`,
  `PackageFailed`. Never leave a package half-moved when the function returns.

Integration tests use `t.TempDir()` for both target and dotfiles and the real stow binary,
skipping with `t.Skip` when `exec.LookPath("stow")` fails:

1. adopt a single file `~/.bar` into package `misc` → `misc/dot-bar`, `~/.bar` is a symlink to it;
2. adopt a directory `~/.config/foo` into `foo` → `foo/dot-config/foo`, `~/.config/foo` is a
   folded directory link;
3. adopt a single file `~/.claude/settings.json` into `claude` while `~/.claude` stays a real
   directory holding the link;
4. restore each of the above; files are back, package directory removed;
5. rollback: point `Runner.Bin` at a shell script in the temp dir that exits 1 with a conflict
   message; after `Execute` the target files are untouched and no package directory remains;
6. `WalkPackage` states for linked, unlinked and conflict entries;
7. `ManagedBy` with relative link targets exactly as stow writes them.

## Out of scope

TUI, doctor states beyond `Linked` / `Unlinked` / `Conflict`, git, subcommands, on-disk journal.

## Acceptance

- All tests above pass with stow 2.4.1 installed and are skipped cleanly without it.
- `go test -race ./internal/...` passes.
- No package in `internal/dotfiles` or `internal/stow` imports anything from `internal/tui`
  or the Charm modules.

## Verification

```
gofmt -l .
go vet ./...
go build ./...
go test ./...
go test -race ./internal/...
```

## Notes

- Observe and report how `stow -D` behaves when a link point was replaced by a regular file
  (does it skip, warn, or exit non-zero?). Write a test that documents the observed behaviour.
- Report whether stow 2.4.1 translates `dot-` below the first component when given
  `--dotfiles`, since DESIGN.md relies on first-component-only mapping for restore. If it
  does translate deeper levels, note the implication (restore of a package created by hand with
  deeper `dot-` names) without changing the mapping.

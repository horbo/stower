# M12 — Unowned links (v1.4)

## Goal

The doctor reports a symlink that resolves to the right package entry but that stow will not
recognise as its own (absolute, wrong relative depth, or written through a symlinked dotfiles
path) as `unowned`, and `f fix` lets stow recreate the link in its own relative form instead of
failing later with `existing target is not owned by stow`.

## Depends on

M11.

## Background

Stow decides ownership textually (`Stow.pm`, `find_stowed_path`): the link destination must be
relative, and `join_paths(parent(target), link_dest)` must start with
`abs2rel(realpath(stow_dir), realpath(target)) + "/"`. An absolute destination is rejected
outright. stower today classifies links by inode (`pointsAt`, `internal/dotfiles/scan.go`), so
these links show as `ok` and the failure only surfaces as `stow.ErrConflict` during restow.

Verified with stow 2.4.1 in a throwaway home (`-d $FAKE/dotfiles -t $FAKE --dotfiles -R`):

| manual link | result |
|---|---|
| `.zshrc -> /abs/path/dotfiles/zsh/dot-zshrc` | `existing target is not owned by stow` |
| `.zshrc -> dotfiles/zsh/dot-zshrc` while `$FAKE/dotfiles` is a symlink to `$FAKE/Projects/dotfiles` | same error, stow expects `Projects/dotfiles/zsh/dot-zshrc` |
| `.config/git/config -> ../dotfiles/git/dot-config/git/config` (wrong depth, dangling) | same error |
| `.config/git/config -> ../../dotfiles/git/dot-config/git/config` | exit 0 |

## Scope

1. `internal/dotfiles/scan.go`
   - Export `ResolvesTo(link, want string) bool` (rename of `pointsAt`, same body).
   - Add `StowOwns(paths config.Paths, pkg string, entry Entry) bool` implementing stow's rule:
     `os.Readlink` of the target; error or absolute destination → false; `stowRel` =
     `filepath.Rel(EvalSymlinks(paths.Target), EvalSymlinks(paths.Dotfiles))` (a missing root →
     false); `filepath.Join(filepath.Dir(entry.TargetRel), dest)` must equal
     `filepath.Join(stowRel, pkg, entry.PkgRel)`.
   - `walkPackageDir`: an entry is `Linked` only when `ResolvesTo && StowOwns`, otherwise
     `Conflict`. Restore plans therefore block on unowned links (`plan.go` conflict check), which
     is required because `stow -D` skips links it does not own.
   - `ManagedBy` stays inode-based.
   - Tests in `scan_test.go`: table for `StowOwns` covering an absolute link, a correct relative
     link at depth 1 and 2, wrong depth, a link written through a symlinked dotfiles directory
     (create `<tmp>/Projects/dotfiles`, symlink `<tmp>/dotfiles` to it, pass the resolved path
     as `Paths.Dotfiles`), and a dotfiles directory outside the target (`stowRel` starting with
     `..`). Extend `TestWalkPackageStates` with an absolute link expected as `Conflict`.

2. `internal/doctor/status.go`
   - `const Unowned State = "unowned"`; `Glyph` falls through to `✘`.
   - Symlink branch of `InspectPackage`: `Linked` → `OK`; else if
     `dotfiles.ResolvesTo(target, entry.PackagePath(paths, pkg))` → `Unowned`,
     `Entry.State = Conflict`, `Fixable = true`, `Detail` =
     `link resolves to the package entry but stow will not own it: <readlink text>`; else
     `Foreign` with `Detail` = `symlink points elsewhere: <readlink text>` plus ` (dangling)` when
     `os.Stat` on the target fails.

3. `internal/doctor/fix.go`
   - `const Relink Action = "relink"`. `Fix` accepts `Relink` only for `Unowned`; every other
     combination keeps the `invalid fix` error.
   - `relink`: `safeParents(paths.Target, target)`, `dotfiles.CheckSameDevice`, backup directory
     `os.MkdirTemp(paths.Dotfiles, ".stower-backup-")`, `os.Rename(target, saved)` (moves the
     link itself), `DryRunRestow`, `Restow`, `os.RemoveAll(backup)`. Rollback like `replace`:
     `Unstow` if the restow was attempted, rename the link back, `RestowExcluding(pkg, excluded)`.
     Do not call `ValidateStagingPath` (it rejects symlinks by design). Preferred shape: extract
     the shared backup / dry-run / restow / rollback core of `replace` into one helper used by
     both flows; the `KeepTarget` `movedTarget` branch stays in `replace`.
   - Never use `stow --adopt`.
   - Tests: relink success (link becomes a relative stow link, backup gone, `stow -R` exits 0);
     relink rollback with a fake runner whose `Restow` fails (original link back at the target,
     backup gone, other links restowed via `RestowExcluding`, pattern of
     `TestFixRollbackPreservesOtherLinks`); `TestInspectStates` extended with an absolute link →
     `unowned` and a dangling link → `foreign` with the new detail text.

4. `internal/tui`
   - `maintenance.go` `openFix`: `Unowned` goes through `confirmPopup` like `Missing` and
     `Unnormalized`, `m.fixAction = doctor.Relink`, title `Fix unowned`, lines
     `<pkg>/<entry>` and `relink`, undo hint `restore the entry to move the file back`.
   - `main/package.go` summary state order: add `doctor.Unowned` after `doctor.Foreign`.
   - Model test: an `unowned` issue selected in Issues, `f` opens the confirm popup with action
     `relink`, confirming runs `doctor.Fix` and the issue disappears after refresh.

5. `cmd/stower`: `status` prints the new state automatically. Add `Detail` as a fifth column only
   if it stays readable; record the decision in the report.

6. Docs
   - `docs/DESIGN.md` doctor state table: add the `unowned` row (condition: symlink resolves to
     the right entry but not in stow's relative form; fix: relink via `.stower-backup-*`, restow,
     delete the backup), reword the `foreign` row to mention the readlink text, list `unowned` in
     the Issues panel row, and add one paragraph explaining stow's textual ownership rule and why
     `Linked` needs both the inode and the textual check.
   - `README.md`: mention `unowned` wherever the doctor states are listed.

## Out of scope

Fixing dangling `foreign` links, `stow --adopt`, `.stow` marker directories, changing
`ManagedBy`, a new popup.

## Acceptance

- `stower status` on a throwaway home with `.zshrc -> <absolute path>` prints
  `zsh  dot-zshrc  …  unowned` and exits 1; after `f` and confirm in the TUI `.zshrc` is a
  relative link created by stow and `stow -R zsh` exits 0.
- The same for a link written through a symlinked dotfiles directory
  (`$FAKE/dotfiles -> Projects/dotfiles`, `--dotfiles $FAKE/dotfiles`).
- `.config/git/config -> ../dotfiles/...` (wrong depth, dangling) is `foreign`, the detail shows
  the readlink text and `(dangling)`, no fix is offered.
- A correct relative link stays `ok`.
- A restore plan for a package with an unowned entry is blocked.
- Relink rollback leaves the original link at the target and no backup directory.

## Verification

```
gofmt -l .          # must print nothing
go vet ./...
go build ./...
go test ./...
```

Manual scenario, never the real `$HOME`:

```
FAKE=$(mktemp -d); FAKE=$(cd "$FAKE" && pwd -P)
mkdir -p "$FAKE/Projects/dotfiles/zsh" "$FAKE/Projects/dotfiles/git/dot-config/git"
echo zsh > "$FAKE/Projects/dotfiles/zsh/dot-zshrc"
echo git > "$FAKE/Projects/dotfiles/git/dot-config/git/config"
ln -s Projects/dotfiles "$FAKE/dotfiles"
ln -s "$FAKE/Projects/dotfiles/zsh/dot-zshrc" "$FAKE/.zshrc"
mkdir -p "$FAKE/.config/git"
ln -s ../dotfiles/git/dot-config/git/config "$FAKE/.config/git/config"
go run ./cmd/stower --target "$FAKE" --dotfiles "$FAKE/dotfiles" status
go run ./cmd/stower --target "$FAKE" --dotfiles "$FAKE/dotfiles"
ls -l "$FAKE/.zshrc"
stow -d "$FAKE/dotfiles" -t "$FAKE" --dotfiles -R zsh; echo "exit=$?"
```

## Notes

- Confirm that `filepath.Join` with a `stowRel` starting with `..` matches stow's `join_paths`
  for the tested cases; record any divergence.
- Record whether `Detail` fits as a column in `stower status`.
- Changing the `foreign` detail text touches `TestInspectStates`; update the expectations, do
  not weaken them.

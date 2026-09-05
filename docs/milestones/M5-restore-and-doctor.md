# M5 — Restore and doctor

## Goal

The user can restore a whole package back into the target, see every link health problem in
the Issues panel with a fix action, restow everything with one key, and run `stower status`
and `stower restow` without the TUI.

## Depends on

M4.

## Scope

- `internal/doctor/status.go`: `Inspect(paths) ([]Issue, []PackageReport, error)` refining
  `dotfiles.WalkPackage` results into the states `ok`, `missing`, `replaced`, `foreign`,
  `unnormalized` as defined in `docs/DESIGN.md` "Doctor"; `Fix` actions `KeepTarget`,
  `KeepRepo`, `Normalize`, `Restow` implemented on top of `dotfiles` and `stow`. Tests build
  each state in a temporary target: `replaced` by replacing a stow-made symlink with a regular
  file, `foreign` by pointing a link elsewhere, `unnormalized` by creating `.zshenv` at the top
  of a package, `missing` by deleting a link.
- `main/restoreplan.go` and wiring: `r` in Packages shows the plan from
  `dotfiles.BuildRestorePlan`; `enter` opens Confirm (undo hint: stage the files again); a
  blocked plan disables confirm and names the Issues entry to fix first; execution runs
  `dotfiles.ExecuteRestore` through the Log context.
- `panels/issues.go`: list of `doctor.Issue` with package, entry and state glyph; `f` opens
  Fix; `D` shows the diff.
- `popups/fix.go`: for `replaced` three options Keep TARGET / Keep REPO / Cancel with one
  line each explaining the effect; `unnormalized` and `missing` skip the popup and run directly
  through Confirm.
- Diff in `main/package.go` and Issues: `git diff --no-index --no-color <repo> <target>`
  rendered in a viewport; when git is missing show `git not available for diff`.
- `R` in Status and Packages: Confirm, then restow every package through the Log context.
- Packages glyphs now come from `doctor` states; Status shows `N issues`.
- Subcommands in `cmd/stower`: `stower status` prints a plain table PACKAGE / ENTRY / TARGET
  / STATE and exits 1 when any issue exists, 0 otherwise; `stower restow` restows every
  package and streams stow output to stdout. Both honour `--target` / `--dotfiles`.

## Out of scope

Git commit popup, first-run popup, per-entry restore, the `x` context menu, security warnings.

## Acceptance

- Manual: after M4's scenario, `r` on `misc` restores `~/.bar` as a regular file and removes
  `dotfiles/misc`; replacing `~/.config/foo/conf` is not an issue (it lives inside a folded
  link) but replacing the `~/.config/foo` link with a real directory shows `replaced` in
  Issues, and Keep TARGET moves the directory into the repository and relinks it.
- `stower status` exit codes are correct in both states; `stower restow` on a package with a
  `missing` link recreates it.
- Doctor tests cover every state and every fix.

## Verification

```
gofmt -l .
go vet ./...
go build ./...
go test ./...
FAKE=$(mktemp -d); mkdir -p "$FAKE/.config/foo" "$FAKE/dotfiles"
echo x > "$FAKE/.bar"; echo y > "$FAKE/.config/foo/conf"
go run ./cmd/stower --target "$FAKE" --dotfiles "$FAKE/dotfiles"   # adopt both, then quit
rm "$FAKE/.bar"; echo z > "$FAKE/.bar"                              # replaced
go run ./cmd/stower status --target "$FAKE" --dotfiles "$FAKE/dotfiles"; echo "exit=$?"
```

## Notes

- `unnormalized` also covers a deeper package component that starts with `dot-` (report only,
  no fix), per DESIGN.md "Path mapping". M1 verified stow translates `dot-` at every level.
- M1 verified that `stow -D` silently skips a link point replaced by a regular file (exit 0,
  no conflict line). The restore plan already blocks on `Conflict`; never bypass that block.
- Apply what M1 reported about `stow -D` on replaced links: if stow exits non-zero there, the
  restore plan must stay blocked and the log must show the reason.
- Report whether `git diff --no-index` exit code 1 (differences found) is handled as success.

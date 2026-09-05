# M7 — Entry restore (v1.1)

## Goal

The user can restore a single link point of a package back into the target without
restoring the whole package; the rest of the package stays linked.

## Depends on

M6.

## Scope

- Carry-over from M6 (small): when the First run popup initialises a repository, commit the
  created files right away with the subject `stower: init`, so Status reads `git clean`
  afterwards. Test in `internal/tui/git_test.go`.

- `internal/dotfiles/plan.go`: `BuildEntryRestorePlan(paths, pkg string, pkgRels []string)
  RestorePlan` restricted to the given link points. Blocked when any selected entry is in
  `Conflict`, when a selected `pkgRel` is not a link point of the package (for example a file
  inside a folded directory link), or when the doctor reports it as anything other than `ok`
  or `missing`. `RemoveDir` is set only when the selection covers every link point of the
  package, so removing the last entry behaves exactly like a full restore.
- `internal/dotfiles/exec.go`: `ExecuteRestore` handles a partial plan: `stow -D <pkg>`, move
  the selected entries back (`MkdirAll` for parents), then relink the remainder with
  `Runner.RestowExcluding(pkg, selected)`. Rollback on any failure: reverse the moves, then a
  plain `stow -R`. Extend the `dotfiles.Runner` interface with `RestowExcluding`; the fake
  runner in TUI tests must implement it.
- `internal/doctor`: `BuildRestorePlan` gains the same entry-level variant so the doctor's
  extra blocks (foreign, replaced, unnormalized) apply to partial plans too.
- TUI: in the Package main context (focused with `enter`), `r` on a highlighted entry opens a
  Restore plan for that entry; `space` toggles a multi-selection of entries and `r` then
  restores the selection. The Restore plan context shows which entries move back and which
  stay linked. Confirm popup body names the entries and states whether the package directory
  will be removed. Execution runs through `beginOperation`, followed by the Commit popup with
  subject `stower: remove <pkg>/<entry>` for one entry or `stower: remove N entries from <pkg>`.
- Tests: plan tests for the blocked cases above; integration test with real stow restoring one
  of three entries and asserting the other two are still symlinks; integration test restoring
  the last remaining entry removes the package directory; TUI test driving the multi-selection.

## Out of scope

Restoring entries below a folded directory link (that would require unfolding the directory
first), security warnings, push.

## Acceptance

- Restoring `dot-zshrc` from a package with three link points leaves `.zprofile` and
  `.p10k.zsh` linked and `dotfiles/zsh` present; `stower status` reports `ok`.
- Restoring all three one by one removes `dotfiles/zsh` after the last one.
- A partial plan containing a `replaced` entry is blocked and points to Issues.
- All existing tests still pass.

## Verification

```
gofmt -l .
go vet ./...
go build ./...
go test ./...
go test -race ./internal/...
```

## Notes

- `RestowExcluding` is already implemented and tested against stow 2.4.1 (M5 review): the
  last path component is matched by its raw `dot-` name, intermediate components by their
  translated `.` names. Reuse it, do not reimplement the ignore patterns.
- Decide whether `r` on a package in the Packages side panel keeps meaning "whole package"
  (recommended) while entry restore lives only in the Package main context.

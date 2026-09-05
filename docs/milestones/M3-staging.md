# M3 — Staging

## Goal

The user can browse the target tree in the Home panel, stage files and directories into
existing or new packages, see them grouped in the Staged panel, and read the resulting adopt
plan in the main panel. Nothing is moved yet.

## Depends on

M2.

M2 built `main/package.go` (package `mainpanel`) as the only main-panel context; `syncMain`
in `app.go` always shows the highlighted package regardless of which side panel has focus,
because Home/Staged/Issues had no context yet. This milestone must make the main panel follow
the focused side panel per DESIGN.md "Main panel contexts": Home focused shows `main/homeentry.go`
for the highlighted tree node, Staged focused shows `main/stagedplan.go`, Packages focused keeps
showing `main/package.go`. Route the choice in `app.go`'s `syncMain`/`focusedPanel` by `m.focus`,
not by which panel last had a selection.

## Scope

- `components/tree.go`: lazy-loading tree over a `Loader func(path) ([]Node, error)`, cursor,
  `→` / `enter` expand, `←` collapse (or jump to parent when already collapsed), `j/k` and
  arrows, `g/G`, `/` filter (case-insensitive substring on the visible flattened list),
  per-node `Badge string`, `Selectable bool`, `Expandable bool`. Unit tests for flatten,
  expand / collapse, filter and cursor clamping with a fake loader.
- `panels/home.go`: tree rooted at the target, hidden entries shown, dotfiles directory
  excluded, directories first then files, both sorted case-insensitively. Managed entries
  (`dotfiles.ManagedBy`) get a dim `[pkg]` badge, `Selectable=false`, `Expandable=false`.
  Staged entries get an accent `→ pkg` badge. `space` opens Assign for the highlighted
  selectable node; `u` unstages it.
- `popups/assign.go`: list of existing packages plus a `new package:` `textinput`; `enter`
  confirms, `esc` cancels; name validated by `dotfiles.ValidatePackageName` with an inline
  error line.
- `dotfiles.ValidateStagingPath` gains one rule: reject an entry whose first component
  relative to the target starts with `dot-` (see DESIGN.md "Path mapping"). Table test.
- Staging model in `app.go`: `dotfiles.Staging`, add / remove, descendant de-duplication
  (staging a directory drops staged descendants and refuses staging a descendant of a staged
  directory, with a one-line message shown in the key bar area for a few seconds).
- `panels/staged.go`: entries grouped by package, `u` unstage, `e` rename the package group
  (reuses the Assign input), counter `N items · M packages`.
- `main/homeentry.go`: for the highlighted Home node: kind, size (files) or file count
  (directories, counted lazily with a cap so huge directories do not block), warnings
  (nested `.git`), first 20 lines of a text file or the directory listing, `Would become:` and
  `Expected link:` from the mapping, staging state.
- `main/stagedplan.go`: renders `dotfiles.BuildAdoptPlan`: moves grouped by package,
  expected links, the exact stow command line, warnings with toggles (`x` toggles
  `remove .git after move` on the highlighted warning), blocked entries with reasons.
- `panels/status.go`: shows `N staged` when staging is non-empty.

## Out of scope

Executing the plan, Confirm popup, Log context, restore, doctor, git, the `x` context menu.

## Acceptance

- Staging `~/.bar` into new package `misc` and `~/.config/foo` into `foo` shows both in Staged
  with the right `Would become` paths; staging `~/.config/foo/file` afterwards is refused.
- Managed entries cannot be staged and cannot be expanded.
- A directory containing `.git` shows the warning and the toggle in the plan.
- A staged path whose destination already exists in the package is shown as blocked.
- Tree tests and staging de-duplication tests pass.

## Verification

```
gofmt -l .
go vet ./...
go build ./...
go test ./...
FAKE=$(mktemp -d); mkdir -p "$FAKE/.config/foo/.git" "$FAKE/dotfiles/zsh"
echo x > "$FAKE/.bar"; echo y > "$FAKE/.config/foo/conf"; echo a > "$FAKE/dotfiles/zsh/dot-zshrc"
stow --dotfiles -d "$FAKE/dotfiles" -t "$FAKE" zsh
go run ./cmd/stower --target "$FAKE" --dotfiles "$FAKE/dotfiles"
```

## Notes

- Directory sizes: report the cap chosen for lazy counting and the observed latency on a
  directory with a few thousand entries.
- Report whether the filter should apply across unexpanded nodes; the current design filters
  only the visible flattened list.

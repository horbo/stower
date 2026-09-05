# M2 — TUI skeleton

## Goal

Running `stower` opens a responsive lazygit-style layout showing the repository read-only:
Status and Packages panels, a Package main context, key bar, key list popup, screen modes,
portrait and accordion behaviour. No mutation of any kind.

## Depends on

M1.

## Scope

Everything under `internal/tui`, per "TUI design" in `docs/DESIGN.md`.

- `layout.go`: pure `Compute(w, h int, focus PanelID, mode ScreenMode) Layout` returning
  rectangles for each side panel, the main panel and the key bar, plus `TooSmall bool`.
  Implements landscape (`w >= 100`, side `clamp(w/3, 32, 48)`), portrait (`w < 100`, focused
  side panel on top at 40% height, tab strip in the bottom bar), accordion (below 6 rows per
  panel the unfocused panels collapse to one title line), screen modes normal / half /
  fullscreen, and `TooSmall` below `60×16`. Table-driven tests at `60×16`, `59×16`, `80×24`,
  `100×30`, `70×18`, `200×50`, for each mode, asserting that rectangles tile the screen without
  overlap or gaps.
- `panel.go`: `Panel` interface `{ SetSize(w, h int); Update(tea.Msg) tea.Cmd; View() string;
  Title() string; Counter() string; Keys() []key.Binding }` and `PanelID` constants
  `Status, Packages, Home, Staged, Issues`.
- `components/frame.go`: rounded border with the title embedded in the top edge on the left,
  the counter on the right, accent colour when focused, collapsed variant that renders only
  the top edge. Tests assert exact width for several widths and title truncation with `…`.
- `styles.go`: lipgloss styles; rely on lipgloss colour profile detection so `NO_COLOR` works.
- `panels/status.go`: one line `<dotfiles> → <target>  stow <version>`; git information is
  added in M6.
- `panels/packages.go`: list of packages from `dotfiles.ListPackages`, glyph `✔` when every
  link point is `Linked`, `✘` otherwise, cursor movement `j/k`, arrows, `g/G`.
- `panels/home.go`, `panels/staged.go`, `panels/issues.go`: placeholders that render their
  title and a single dim line (`not implemented yet`), so the layout is complete.
- `main/package.go`: table ENTRY / TARGET / STATE built from `dotfiles.WalkPackage` for the
  highlighted package, a summary line, vertical scrolling; `enter` moves focus into main,
  `esc` moves it back.
- `app.go`: root model holding panels, focus, screen mode, layout, optional popup; key
  dispatch: `1-4` and `0` select panels, `tab` cycles, `+` / `_` change screen mode, `?` opens
  the key popup, `q` and `ctrl+c` quit, `esc` closes a popup; data refresh on start and on `R`
  (re-scan only; restow comes in M5). Key bar shows the focused panel's `Keys()` and the global
  ones, truncated to width; in portrait it shows the tab strip instead.
- `popups/keys.go`: scrollable list of every binding for the focused panel plus global keys.
- `cmd/stower/main.go`: without `--version` start the TUI with `tea.WithAltScreen()`.

## Out of scope

Tree component, staging, any popup other than Keys, any operation that changes files, git,
doctor states, subcommands.

## Acceptance

- `go run ./cmd/stower --target "$FAKE" --dotfiles "$FAKE/dotfiles"` against a throwaway
  target with two hand-made packages shows both packages and their link states.
- Resizing the terminal between landscape and portrait re-lays out without artefacts; a
  `50×10` terminal shows only `terminal too small`.
- `layout` tests cover every listed size and mode; `frame` tests cover widths 20, 40, 80.
- No file under `internal/tui` performs writes, renames or stow calls.

## Verification

```
gofmt -l .
go vet ./...
go build ./...
go test ./...
FAKE=$(mktemp -d); mkdir -p "$FAKE/dotfiles/zsh" "$FAKE/dotfiles/misc"
echo a > "$FAKE/dotfiles/zsh/dot-zshrc"; echo b > "$FAKE/dotfiles/misc/dot-bar"
stow --dotfiles -d "$FAKE/dotfiles" -t "$FAKE" zsh
go run ./cmd/stower --target "$FAKE" --dotfiles "$FAKE/dotfiles"
```

Expected in the manual run: `zsh` shows `✔`, `misc` shows `✘` (unlinked), the Package
context lists `dot-bar → <target>/.bar  unlinked`.

## Notes

- Import the Charm modules as `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`,
  `charm.land/lipgloss/v2`; see `go.mod`. Bubble Tea v2: `View() tea.View`, `tea.KeyPressMsg`.
- Replace the placeholder `internal/tui/app.go` from M0 entirely.

- Record whether the pinned lipgloss major supports layer composition for dimmed popup
  backgrounds; if not, popups will render without dimming from M3 on.
- Report any bubbles component that does not fit (for example `table` column sizing) and
  what was used instead.

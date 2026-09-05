# M8 — Mouse support (v1.2)

## Goal

The user can drive the TUI with the mouse in addition to the keyboard: click to focus a
panel and select a row, scroll with the wheel, click options in popups. Keyboard behaviour is
unchanged, and mouse mode can be turned off to keep terminal text selection.

## Depends on

M7.

## Scope

- Enable mouse reporting on the Bubble Tea v2 view alongside `AltScreen`, gated by a
  `--no-mouse` flag in `cmd/stower/main.go` and the environment variable `STOWER_NO_MOUSE=1`.
  Flag and variable are resolved in `internal/config` next to the existing paths.
- `internal/tui/app.go`: handle `tea.MouseClickMsg`, `tea.MouseWheelMsg` and, if needed for
  hover feedback, `tea.MouseMotionMsg`. Map the click position to a panel through
  `Layout.Rects()`; a click into a side panel moves focus there (same effect as `0-4`), a click
  into main moves focus into main (same effect as `enter` where allowed), a click on the key
  bar tab strip in portrait switches panels. Clicks inside a collapsed accordion panel expand it.
- Row selection: extend `Panel` with `Click(x, y int) tea.Cmd` implemented by the list-like
  panels (Packages, Staged, Issues), the tree (Home) and the main contexts that have a cursor
  (Package entries). Each translates the local Y coordinate to a row index using its own scroll
  offset and moves the cursor there; a click on the already highlighted row acts as `enter`
  (expand in Home, focus main in Packages). Wheel scrolls the focused panel by three rows, or
  the panel under the pointer when that is simpler to implement consistently.
- Popups: clicks on option lines in Confirm, Fix, Assign, Commit and First run select that
  option; a click outside the popup closes it like `esc`. Popup hit-testing uses the popup
  rect already computed in `popupRect()`.
- Key bar: clicking a `key description` pair sends that key.
- Tests: pure hit-testing functions (`panelAt(layout, x, y)`, `rowAt(offset, y)`) with table
  tests; model tests that feed synthetic mouse messages and assert focus, cursor and popup
  state; a test that `--no-mouse` leaves mouse reporting disabled on the view.

## Out of scope

Drag and drop (for example dragging a Home entry onto a package), text selection inside
panels, double-click semantics, resizing the side column with the mouse.

## Acceptance

- Clicking a package in Packages highlights it and updates the main context; clicking the same
  row again focuses main. Clicking a tree row in Home selects it; clicking the highlighted
  directory expands or collapses it.
- Wheel scrolls Home, the Package entries table and the Log without moving keyboard focus
  unexpectedly.
- Clicking `Keep TARGET` in the Fix popup behaves exactly like the keyboard choice.
- With `--no-mouse` the terminal's native text selection works and no mouse message changes
  the model.
- All existing tests still pass; `NO_COLOR` and portrait layouts are unaffected.

## Verification

```
gofmt -l .
go vet ./...
go build ./...
go test ./...
go test -race ./internal/tui/...
```

## Notes

- Lip Gloss v2's compositor exposes `Hit` for layer hit-testing (M2 report); prefer the
  `Layout.Rects()` mapping, which is already the single source of truth, and use `Hit` only for
  popups if it simplifies the code.
- Terminals differ in whether wheel events arrive as button 4/5 presses or dedicated wheel
  messages; test on the user's terminal and record what Bubble Tea v2 delivers.

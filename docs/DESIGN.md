# stower design

stower is a lazygit-style TUI frontend for GNU Stow. It manages a dotfiles repository that
follows the `stow --dotfiles` convention: every top-level directory of the repository is a
package, and inside a package the tree mirrors `$HOME`, with the leading `.` of top-level
entries replaced by the `dot-` prefix.

Terminology used throughout: **target** is the directory stow links into (normally `$HOME`),
**dotfiles** is the repository directory (normally `~/dotfiles`), **package** is a top-level
directory inside dotfiles. The Go module is `github.com/horbo/stower`.

## Domain rules

### Path mapping

| target path                  | package `P` path                |
|------------------------------|---------------------------------|
| `~/.zshrc`                   | `P/dot-zshrc`                   |
| `~/.config/nvim/`            | `P/dot-config/nvim/`            |
| `~/.claude/settings.json`    | `P/dot-claude/settings.json`    |
| `~/bin/tool`                 | `P/bin/tool`                    |

The `dot-` prefix applies **only to the first path component** relative to the package.
Deeper names keep a literal leading dot. This matches the existing `update.sh` behaviour and
is deliberate: stow folds directories that do not yet exist in the target, so deeper levels are
linked as a whole and never translated. The mapping is bijective, so restoring a package needs
no state file: `P/dot-config/nvim` maps back to `~/.config/nvim` unambiguously.

Two findings from M1 (verified against stow 2.4.1):

- stow itself translates `dot-` at **every** level when given `--dotfiles`
  (`pkg/dot-config/dot-x` links to `~/.config/.x`). Packages created by stower never contain a
  deeper `dot-` name, so nothing diverges for them. A hand-written package with a deeper
  `dot-` name is reported by the doctor as `unnormalized` (report only, no automatic fix).
- A target entry whose first component literally starts with `dot-` (`~/dot-foo`) would map
  to `pkg/dot-foo` and be linked back as `~/.foo`. Staging such an entry is rejected.

### Managed detection

An entry in the target is *managed* when it is a symlink whose resolved destination lies
inside the dotfiles directory. stow writes relative link targets (`dotfiles/zsh/dot-zshrc`,
`../dotfiles/nvim/dot-config/nvim`), so the destination must be resolved relative to the
link's parent directory and then canonicalised with `filepath.EvalSymlinks` on both sides.
Never compare link text. The package name is the first component of the path relative to
dotfiles.

### Link points

Walking a package tree, each entry maps to a target path with one of these outcomes:

- target is a symlink to this entry: a **link point** (a file, or a folded directory). Do not
  descend.
- target is a real directory (unfolded, e.g. `~/.claude` holding per-file links): descend.
- target does not exist: **unlinked**. Restow fixes it.
- target is a regular file or directory, or a symlink elsewhere: **conflict**.

Link points are the unit of restore and of doctor reporting.

### stow invocations

Always with explicit `-d` and `-t`, never relying on the working directory:

```
stow --dotfiles -n -v -R -d <dotfiles> -t <target> <pkg>   dry run, parse conflicts
stow --dotfiles    -v -R -d <dotfiles> -t <target> <pkg>   restow
stow --dotfiles    -v -D -d <dotfiles> -t <target> <pkg>   unstow
```

stow reports conflicts on stderr with lines such as `WARNING! stowing X would cause
conflicts:` followed by `* existing target is not owned by stow: …`. A non-zero exit code
with such lines is a conflict; any other non-zero exit is an error.

## Architecture

```
cmd/stower/main.go          flags, stow detection, start TUI or run a subcommand
internal/config/            Paths{Target, Dotfiles}, flags --dotfiles/--target, env STOWER_DOTFILES
internal/dotfiles/
  mapping.go                TargetToPackage / PackageToTarget, table-tested both ways
  scan.go                   ListPackages, WalkPackage (link points), ManagedBy
  plan.go                   AdoptPlan (staging -> moves), RestorePlan (package -> moves back)
  exec.go                   transactional execution: move journal, rollback, event stream
  validate.go               staging and plan validation rules
internal/stow/runner.go     stow subprocess, exit codes, conflict parsing
internal/doctor/status.go   link health states and fix actions
internal/gitx/git.go        IsRepo, Init, DirtyPaths, AddAndCommit
internal/tui/
  app.go                    root model: focus, popup, key dispatch, data refresh
  layout.go                 pure layout from terminal size, focus and screen mode
  panel.go                  Panel interface { SetSize; Update; View; Title; Keys }
  panels/status.go          [0] Status
  panels/packages.go        [1] Packages
  panels/home.go            [2] Home (target tree)
  panels/staged.go          [3] Staged
  panels/issues.go          [4] Issues
  main/*.go                 main-panel contexts: package, homeentry, stagedplan, restoreplan, log
  popups/*.go               assign, menu, confirm, fix, commit, keys, error, firstrun
  components/tree.go        lazy tree with cursor, fold, filter, badges, selectable flag
  components/frame.go       rounded frame with title in the top edge and a counter on the right
  styles.go                 lipgloss styles, NO_COLOR respected
```

The domain layer (`dotfiles`, `stow`, `doctor`, `gitx`) has no TUI imports and is tested on
temporary directories. `exec.go` emits events (step started, output line, step done, step
failed, rollback) on a channel; the TUI renders them live in the Log context.

Event channel contract (changed in M4): sends on `events` block until received and do **not**
select on `ctx.Done()`, so rollback events are delivered even after cancellation. The consumer
must keep draining until `Execute` / `ExecuteRestore` / `doctor.Fix` returns; the TUI does this
with a forwarding goroutine and a buffered message channel. Cancellation is checked between
steps and never kills a running stow subprocess.

## Flows

### Adopt

1. In the Home panel the user browses the target tree. Hidden entries are shown, the dotfiles
   directory is excluded, directories load lazily. Managed entries carry a dim `[zsh]` badge,
   cannot be selected and cannot be expanded (their content is already inside the repo); the
   same dim badge also covers a dangling symlink that still resolves lexically to a package
   entry.
2. `space` on an entry opens the Assign popup: pick an existing package or type a new name.
   The entry joins the session staging (`map[targetPath]package`) and shows a `→ git` badge.
   Several packages can be staged at once. The entry appears immediately; the Staged panel
   shows a spinner and `Scanning…` while its plan is prepared in the background. Browsing,
   staging and unstaging remain available during the scan.
3. Validation, at staging time and again when building the plan:
   - path outside the target, or inside dotfiles: rejected;
   - path is a symlink (managed or foreign): rejected;
   - a directory and one of its descendants both staged: only the directory stays, with a
     message in the key bar;
   - destination inside the package already exists: the entry is `blocked` in the plan;
   - package name must match `[A-Za-z0-9._-]+` and must not start with `.`;
   - a staged directory containing a nested `.git`: warning, with a plan toggle
     `remove .git after move`, off by default (git would otherwise treat it as an embedded
     repository). This scan happens only after staging, never in the Home preview. A scan
     error blocks the affected entry and is displayed with its reason; refresh retries it;
   - first path component of the staged entry starts with `dot-`: rejected (not reversible
     under `--dotfiles`);
   - target and dotfiles on different devices (`Stat_t.Dev`): hard error before any move.
4. The Staged panel and the Staged plan main context show, grouped by package: moves
   (`~/.zshrc → zsh/dot-zshrc`, directories with file counts), expected links (computed from
   the mapping, because a real stow dry run is only possible after the move), the stow command,
   warnings and blocked entries. In the Staged side panel a blocked entry keeps its path row,
   which stays selectable and can be unstaged, and shows `✘ <reason>` on the line below it.
   Execution failures are shown once per package, under the package header.
5. `enter` on a ready plan revalidates it in the background and opens Confirm only when the
   current result arrives. Applying while a scan is pending shows `Scan in progress` and
   does not queue execution. Changing staging during revalidation cancels the Apply intent.
   After confirmation execution runs **one transaction per package**; a failure in the
   second package does not undo the first. Per package:
   1. `MkdirAll` for package directories, remembering which ones were created;
   2. `os.Rename` each entry, appending every move to a journal;
   3. `stow -n -v -R <pkg>`: on conflict, roll back the journal, remove the created
      directories, show stderr in the log;
   4. `stow -v -R <pkg>`: on error, same rollback;
   5. only now, when `remove .git after move` was toggled, delete nested `.git` directories
      inside the moved entries (deleting earlier would make the rollback lossy); a failure
      here is reported but does not undo the adoption;
   6. on success, offer a commit and refresh every panel.

### Restore

1. `r` on a package shows the Restore plan: every link point with its target path and state
   (`linked`, `unlinked`, `conflict`), then `remove empty <dotfiles>/<pkg>`, plus a warning
   when the package has uncommitted changes. Any conflict blocks the plan and points to the
   Issues panel.
2. Confirm, then: `stow -v -D <pkg>`; `os.Rename` each link point back to its target path
   (`MkdirAll` for the parent); remove now-empty package directories and the package itself.
   Rollback on error: reverse the moves and `stow -R`.
3. Open question for M1: how `stow -D` behaves when a link was replaced by a regular file.
   The plan blocks on conflicts anyway; record the observed behaviour in the milestone report.

### Doctor

Each link point of each package has one state:

| state        | condition                                              | fix                                                       |
|--------------|--------------------------------------------------------|-----------------------------------------------------------|
| ok           | symlink points at the right package entry              | none                                                      |
| missing      | target path does not exist                             | restow the package                                        |
| replaced     | regular file or directory where a link should be       | popup: Keep TARGET (move it into the package, overwrite, restow) / Keep REPO (delete the target copy, restow) / Cancel |
| foreign      | symlink pointing somewhere else; the detail shows the readlink text and marks a dangling link | report only               |
| unowned      | symlink resolves to the right package entry but not in stow's relative form | relink: move the link into a `.stower-backup-*` directory inside dotfiles, restow, delete the backup |
| unnormalized | top-level package entry starts with `.` instead of `dot-` | rename like `update.sh` does, skipped when the destination exists |

Detecting a replaced **directory** is a heuristic, because nothing records whether stow folded
it: a real directory in the target is `replaced` only when no correct link exists anywhere below
it and at least one regular file sits where a link is expected; otherwise the doctor descends
and reports the individual entries. An ordinary empty unfolded directory is therefore never
offered as a destructive repair. `Keep TARGET` / `Keep REPO` move the losing copy to a
`.stower-backup-*` directory inside dotfiles first and delete it only after a successful restow;
a failed rollback keeps the backup and reports its path. Every single-entry fix restows the
package with the other conflicting entries of that package (everything except `ok` and
`missing`) passed to stow as `--ignore`, so a conflicting sibling never blocks the fix while
missing siblings are still linked by the same restow; the rollback restow ignores the fixed
entry as well.

Stow decides ownership textually, not by inode (`Stow.pm`, `find_stowed_path`): a link is
owned by stow only when the destination is relative and
`join_paths(parent(target), link_dest)` starts with
`abs2rel(realpath(dotfiles), realpath(target))`. An absolute destination is rejected outright,
and so is a relative one written at the wrong depth or through a symlinked dotfiles path.
Such a link resolves to the correct package entry, so an inode check calls it healthy,
while `stow -R` fails with `existing target is not owned by stow` and `stow -D` silently skips
it. `dotfiles.Linked` therefore requires both checks: `ResolvesTo` (inode) and `StowOwns`
(stow's textual rule). Everything else is a conflict, which also blocks restore plans on the
affected entries. `ManagedBy` stays inode-based: it answers which package owns a path in the
Home panel, not whether stow will accept the link.

Problems appear in the Issues panel; the full per-package table appears in the Package main
context. `R` restows every package, which is the exact equivalent of `update.sh`.
The non-interactive subcommands `stower status` and `stower restow` expose the same two
operations for scripting and verification.

### Git

- Missing dotfiles directory: the First run popup offers `create <dotfiles>`, `git init`,
  `add .gitignore`. An existing directory without `.git` gets a `git init` offer that can be
  declined; stower then works without git features.
- After a successful adopt, restore or fix the Commit popup lists `git status --porcelain`
  for the touched packages and proposes a subject-only message: `stower: add zsh (3 files)`,
  `stower: remove zsh`, `stower: fix claude/settings.json`. Execution:
  `git -C <dotfiles> add -A -- <pkg>` (also captures deletions) then `git commit -m`.
- Packages shows `*` next to packages with uncommitted changes; Status shows the total of
  dirty top-level entries (packages and anything else at the root, so a modified `README` is
  not invisible), `git clean`, or `no git`.
- M6 decisions: `IsRepo` is strict (the dotfiles directory must be the work-tree root, so a
  dotfiles directory nested in another repository reads as `no git`); one whole-repo
  `git status --porcelain --untracked-files=all` per refresh feeds both markers; the Commit
  popup has an edit mode (`e` / `i`) because `s` skip would otherwise be typed into the subject;
  an `Update` operation renders the manual `c` commit as `stower: update zsh (2 files)`.
- Known small issue (carry-over to M7): after first run with `.gitignore` checked, the file
  stays untracked because the automatic commit adds only packages; Status shows `git 1*` until
  a manual `c`. Fix: first run commits `stower: init` when git init is chosen.
- No push in v1.

## TUI design

One persistent lazygit-style layout instead of screen navigation: a column of side panels on
the left, a main panel on the right showing the context of the highlighted item, a
context-sensitive key bar at the bottom, centred popups. UI text is English. The glyphs
`✔ ✘ ⚠` carry state independently of colour, and `NO_COLOR` is respected. The focused panel
has an accent-coloured border and a counter in its title (`1 of 6`). Colours: managed entries
dim, staged entries accent, ok green, replaced red, warning yellow.

### Side panels

| #   | panel    | content                                                        | main keys                                           |
|-----|----------|----------------------------------------------------------------|-----------------------------------------------------|
| [0] | Status   | `~/dotfiles → ~  stow 2.4.1  git 1*`, one line, fixed height    | `c commit`, `R restow all`                          |
| [1] | Packages | state glyph, name, `*` when dirty in git                        | `enter` focus main, `r restore`, `R restow`, `c commit` |
| [2] | Home     | target tree; dim `[zsh]` badge = managed (not selectable, not expandable), accent `→ git` = staged | `space stage`, `u unstage`, `←→ fold`, `/ filter` |
| [3] | Staged   | session staging grouped by package; `✘ reason` under a blocked path, per-package line for execution failures; empty shows `nothing staged` in the title counter | `enter apply`, `u unstage`, `e rename package`      |
| [4] | Issues   | doctor problems only: replaced / missing / foreign / unowned / unnormalized; empty shows `no issues` in the title counter | `f fix`, `D diff`                       |

Global keys: `1-4` switch panels (`0` Status), `tab` next panel, `?` full key list, `+` / `-`
screen modes (`_` remains an alias for `-`), `R` restow all packages (with Confirm), `ctrl+r`
re-scan without touching the filesystem, `q` quit, `esc` closes a popup, returns focus from
main to the side panel, or clears the Home filter. A finished operation log closes with either
`enter` or `esc`, restoring the panel and focus that were active before the operation started.
`x` toggles `remove .git after move` while the Staged plan is shown; the `x` context menu is
post-v1 and will absorb that toggle as one of its items.

Panels never mutate shared state directly: they return a `tea.Cmd` emitting a request message
(`StageRequestMsg`, `UnstageRequestMsg`, `UnstageGroupMsg`, `RenameGroupMsg`) and the root model
owns the staging map. A panel that needs every key (a filter input, a text field) implements
`CapturesInput() bool`; the root hands it all input while that returns true.

### Main panel contexts

The main panel content follows the focused panel and its highlighted item:

- **Package: X**: table ENTRY / TARGET / STATE of every link point, a summary line, and for a
  problematic entry the details (size and mtime of both versions) plus `git diff --no-index`.
  `enter` moves focus into main so actions apply per entry (`f fix`, `space mark`,
  `r restore entries`). `space` marks the highlighted link point with `✓` and the summary line
  counts the marks; `r` opens the Restore plan for the marked entries, or for the highlighted
  one when nothing is marked.
- **Home: ~/path**: kind, size, directory listing or head of
  the file, `Would become: <pkg>/dot-config/ghostty/`, `Expected link: …`. File counting stops
  at `mainpanel.CountCap` (2000) and shows `2000+ files`. Preview reads and file counts run
  in the background with a local spinner; Home does not search for nested `.git` entries.
  A symlink shows the raw `readlink` target on its own line below a status line: a link
  resolving into a package gets a green `✔ already in package X` (a yellow
  `⚠ already in package X (dangling link)` when it is also broken), a broken link that resolves
  to no package gets `⚠ dangling link`, and only a link that leads outside the repo altogether
  gets `a symlink cannot be staged`. Symlinks are always non-selectable and non-expandable,
  since staging rejects every symlink anyway. The `/` filter matches only visible (expanded)
  rows by design; expanding a directory clears the filter so its newly revealed children stay
  visible, while collapsing a node or expanding an already-expanded one leaves the filter
  untouched. A deep search would be a separate asynchronous mode.
- **Staged plan**: moves grouped by package, expected links, stow command, `✘ blocked` entries
  with reasons, toggles such as `[x] remove .git after move`. The toggle is **per package**,
  driven by the package highlighted in the Staged side panel and toggled with `x` (M3 decision:
  a per-warning cursor would need a second cursor inside main with no key left to drive it).
  `›` marks the highlighted package in the plan. Changing the toggle updates the existing
  plan without another filesystem scan.
- **Restore plan: X**: `zsh/dot-zshrc → ~/.zshrc  ✔ linked` per link point,
  `Then: remove empty ~/dotfiles/zsh`, dirty-git warning; blocked variant
  `✘ ~/.zshrc is a regular file → fix in Issues first`.
- **Log**: during an operation the main panel becomes a live command log (`✔ mv …`, indented
  stow output, `✘` failures, `↩ rollback` steps) with a per-package summary. While the operation
  runs, `ctrl+c cancel` is the only key; once it finishes, `enter` or `esc` returns to the panel
  and focus that were active before the operation started.

### Responsiveness

Layout is recomputed on every `tea.WindowSizeMsg`; the root model hands sizes to panels.

- **Landscape** (`W >= 100`): side column `clamp(W/3, 32, 48)`, main takes the rest. Status is
  a fixed 3 rows; the other four panels split the remaining height evenly. An empty Staged or
  Issues panel is collapsed to its title line regardless of focus, and the height it frees goes
  to the remaining panels. When a panel would still get fewer than 6 rows the column switches to
  **accordion**: the focused panel takes the remainder and the others collapse to their title
  line only, except that focus on an empty panel expands the first non-empty panel instead.
  Packages never collapses.
- **Portrait** (`W < 100`): a deliberate departure from lazygit, which stacks every side panel.
  Only the focused side panel is visible on top (40% of the height), main below it, and the
  other panels are reachable through a tab strip in the bottom bar:
  `[0]Status [1]Packages [2]Home [3]Staged [4]Issues`.
- **Screen modes** `+` / `-` (`_` remains an alias for `-`): normal → half (side column takes
  50% of the width) → fullscreen (only the focused panel; useful for the log and diffs in main).
- The key bar truncates to the width (full list under `?`). Panel text is truncated with `…`,
  main scrolls vertically. Below `60×16` a single line `terminal too small` is shown.

Landscape mockup, Packages focused:

```
╭─[0] Status ─────────────────────────╮╭─Package: claude ─────────────────────────────────────────╮
│ ~/dotfiles → ~  stow 2.4.1  git 1*  ││ ENTRY                       TARGET                  STATE │
╰─────────────────────────────────────╯│ dot-claude/settings.json    ~/.claude/settings.json ✘ replaced
╭─[1] Packages ─────────────── 1 of 6 ╮│ dot-claude/CLAUDE.md        ~/.claude/CLAUDE.md     ✔     │
│ ✘ claude *                          ││ dot-claude/skills/          ~/.claude/skills        ✔ dir │
│ ✔ nvim                              ││ … 6 more                                                  │
│ ✔ zsh                               ││                                                           │
╰─────────────────────────────────────╯│ ✘ ~/.claude/settings.json is a regular file, not a symlink│
╭─[2] Home ───────────────────────────╮│   TARGET  4.1 KB  2026-09-05 14:52                        │
│ ▾ ~                                 ││   REPO    3.9 KB  2026-09-05 12:10                        │
│   ▸ .config/                        ││ @@ -12,3 +12,4 @@                                         │
│     .zshrc                   [zsh]  ││ +  "fastMode": true,                                      │
╰─────────────────────────────────────╯│                                                           │
╭─[3] Staged ─────────────────────────╮│                                                           │
│ (nothing staged)                    ││                                                           │
╰─────────────────────────────────────╯│                                                           │
╭─[4] Issues ──────────────────── 1 ──╮│                                                           │
│ ✘ claude  settings.json  replaced   ││                                                           │
╰─────────────────────────────────────╯╰───────────────────────────────────────────────────────────╯
 enter focus main  r restore  R restow  c commit  x menu  ? keys  1-4 panels  + mode  q quit
```

Portrait mockup, `70×18`, Home focused:

```
╭─[2] Home ────────────────────────────────────────── 14 of 52 ───╮
│   ▾ .config/                                                    │
│       ▸ nvim/                                          [nvim]   │
│       ▸ gh/                                            → git    │
│     ▸ ghostty/                                                  │
╰─────────────────────────────────────────────────────────────────╯
╭─Home: ~/.config/gh ─────────────────────────────────────────────╮
│ directory · 2 files · staged → git (new package)                │
│ Will move to:  git/dot-config/gh/                               │
│ Expected link: ~/.config/gh → ../dotfiles/git/dot-config/gh      │
│ config.yml                                                      │
│ hosts.yml                                                       │
╰─────────────────────────────────────────────────────────────────╯
 [0]Status [1]Packages [2]Home [3]Staged [4]Issues   space x ? +
```

### Popups

Centred, width `min(W-4, 70)`, height at most `H-4` with scrolling, dimmed background when
the rendering stack supports layer composition.

- **Assign** (`space` in Home): list of existing packages plus a `new package:` input.
- **Menu** (`x`): actions for the highlighted item with their keys.
- **Confirm** (`enter` in Staged, `r` in Packages, `f` in Issues, `R` restow all): summary and
  how to undo, `y` / `n`.
- **Fix** for `replaced`: Keep TARGET / Keep REPO / Cancel. `unnormalized` renames without
  asking, `missing` restows, `unowned` relinks; the last three go through Confirm.
- **Commit** after a successful operation: `git status --porcelain` for the touched packages,
  message input with a default subject, `enter` / `s` skip / `esc`.
- **Keys** (`?`), **Error** (title plus stderr in a viewport), **First run** (missing dotfiles:
  checkboxes `create <dotfiles>`, `git init`, `add .gitignore`).
- Missing stow binary: message on stderr with `brew install stow`, the TUI does not start.

Operation flow: Confirm → main becomes Log → Commit popup → every panel refreshes.

### Implementation notes

- `Panel { SetSize; Update; View; Title; Keys }`; the root model computes the layout in one
  place (`layout.go`) and the main panel is a set of contexts chosen by focus and selection.
- From bubbles: `list`, `table`, `viewport`, `textinput`, `help`, `spinner`. Custom:
  `components/tree.go` (lazy load, fold, filter, badges, selectable flag) and
  `components/frame.go` (lipgloss does not draw a title inside the border; the top edge is
  assembled by hand).
- Popups: the root model holds an optional popup that captures keys. Background dimming uses
  lipgloss v2 layer composition; if the pinned v2 API turns out not to support it cleanly,
  popups render without dimming and the milestone report says so.
- Module paths are `charm.land/{bubbletea,bubbles,lipgloss}/v2` (pinned in M0: v2.0.9,
  v2.2.1, v2.0.6). `github.com/charmbracelet/x/ansi` is already in the dependency closure and
  may be imported for `ansi.Truncate` and width measurement.
- Text truncation and wrapping to width is done explicitly (`ansi.Truncate` from
  `charmbracelet/x/ansi` or an equivalent already pulled in by the pinned dependencies).
- `layout.go` exposes pure functions `(W, H, focus, mode) → rectangles`, table-tested at
  `60×16`, `80×24`, `100×30`, `70×18`, `200×50`.
- Adopt planning runs in a `tea.Cmd` over a staging snapshot. Every staging change advances
  its version, cancels obsolete work and replaces the pending snapshot. At most one plan
  worker and one latest pending request exist; only the current version can update the plan
  or open Apply confirmation. `BuildAdoptPlanContext` and `HasNestedGitContext` check
  cancellation during directory traversal; the original functions wrap them for callers
  that do not need cancellation. The domain remains independent of Bubble Tea.
- Home's initial load, refresh and lazy child loading use asynchronous commands and retain
  the existing sort order. The tree keeps one in-flight load per path, identified by path and
  request generation: collapsing a folder cancels its load, a reload cancels every load, and a
  late or stale result neither reopens a collapsed folder nor replaces newer children. The
  initial load goes through the same refresh path as `ctrl+r`. Preview work is limited to one
  active request and the latest pending request, cancelling on selection changes; a result
  is applied only when its path and generation match the current selection. Tree badges are
  recalculated when staging or loaded data changes, not on cursor movement; the managed
  package travels on the node (`Node.Managed`) and is rendered by the Home panel.
- Bubbles spinners indicate local background work. Commands return messages; only `Update`
  applies results to model state. Spinner ticks stop when no work remains. Scan results are
  not persisted or reused as a long-lived cache.

## Known performance debt

Nested `.git` detection still traverses staged directories without a visited-entry budget,
but now runs in a cancellable background plan worker, never on Home cursor movement. Large
trees can therefore take time to prepare; Apply stays unavailable until the current scan
finishes. Cancellation is cooperative between filesystem operations and cannot interrupt an
individual blocked filesystem call.

Home previews, capped file counts and child directory reads also run in the background.
Reading and sorting a very wide directory still costs time and memory proportional to its
entries, and rebuilding visible tree rows or badges still costs time proportional to loaded
data. Cursor movement avoids those scans and badge updates. Performance scenarios live in
`internal/tui/perf_test.go` (benchmarks for cursor movement over a flat 5000-entry directory
and a deep subtree, plus a guard test asserting that a cursor move calls neither the tree
loader nor the entry inspection synchronously) and `internal/dotfiles/plan_bench_test.go`
(the plan scan alone on the deep subtree). They use temporary directories only and are not
run by `go test` without `-bench`. No persistent scan cache or new dependency is introduced.

## v1.1

Restoring single link points of a package (M7): `stow -D`, move the selected entries back,
relink the rest with `RestowExcluding`. Entry restore is offered inside the Package main
context; `r` in the Packages side panel keeps restoring the whole package. A selection that
covers every link point removes the package directory and is exactly a full restore. The plan
blocks an entry that is not a link point of the package (a file below a folded directory link,
for example), and the doctor blocks the whole plan while any entry of the package is neither
`ok` nor `missing`, selected or not: the relink would abort on that entry anyway.

## v1.2

Mouse support (M8): click to focus and select, wheel to scroll, clicks in popups, gated by
`--no-mouse` / `STOWER_NO_MOUSE` so terminal text selection stays available. Hit-testing goes
through `Layout.Rects()` and a `Click(x, y)` method on panels that own a cursor. A click into
an unfocused panel moves the cursor and focus but never activates (`enter`); a click on the
already highlighted row of a focused panel does. The wheel scrolls the panel under the pointer
by three rows without changing focus. Popups whose choice starts an operation without a further
confirmation (Fix, First run) take two clicks: the first highlights the option, the second
performs it; a click outside a popup closes it like `esc`, except First run where `esc` quits.

## v1.3

Open in `$EDITOR` (M9): `o` opens the highlighted entry (target path in Home, repository copy
in the Package context and Issues, `shift+o` for the other side), the TUI suspends the
alternate screen while the editor runs and re-scans on return. `$VISUAL` wins over `$EDITOR`,
the value is split quote-aware and never run through a shell.

## Release

Distribution (M10): a `v*` tag drives goreleaser from GitHub Actions, which builds darwin and
linux binaries for amd64 and arm64, publishes the GitHub Release and updates the formula in
the tap `horbo/homebrew-tap`, so `brew install horbo/tap/stower` works. The maintainer
procedure, the one-time token setup and the recovery steps are in `docs/RELEASING.md`.

## Out of scope for v1.3

Security warnings (`~/.ssh`, `~/.gnupg`, `~/.aws`, cache and runtime directories, `.gitignore`
suggestions), an on-disk journal for crash recovery
during moves, signed or notarised binaries, Linux packages, Windows builds, teatest coverage
for the TUI, the `x` context menu.

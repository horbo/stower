# M5 implementation report

Implemented in `/private/tmp/stower-workspace.QIrf2v` on top of the completed M4 work. The original workspace was not edited. No commits were created, and `STATUS.md` retains the coordinating session's copied state.

## Implemented

- Doctor inspection with `ok`, `missing`, `replaced`, `foreign`, and `unnormalized` states, package reports, and issue details.
- Restore plans from Packages with `r`, confirmation with Enter, conflict blocking, execution through the shared log, cancellation, and panel refresh.
- Issues selection with `f` for fixes and `D` for diffs. Package main also supports entry selection, fixes, and diffs. Status shows issue counts and Packages reflects doctor health.
- Keep TARGET and Keep REPO use a temporary backup. Failed or cancelled fixes restore the originals; rollback after restow also restores unaffected package links through stow, excluding the original problem entries. A rollback failure retains its backup and reports the location.
- Missing links use restow; top-level literal dot names can be normalized with collision protection and rollback. Foreign links and nested `dot-` components remain report only.
- `R` in Status or Packages confirms restowing all packages. The old refresh behavior remains available with `R` in other side panels.
- `stower status` prints PACKAGE / ENTRY / TARGET / STATE and exits 1 for issues, 0 for a healthy target. It does not require stow or git to be installed.
- `stower restow` processes every package, forwards stow output to stdout as it arrives, and reports failures with a nonzero exit status. Both subcommands accept explicit target and dotfiles paths.
- Diff runs asynchronously in the TUI with external diff helpers disabled. Git exit code 1 means differences found and is handled as success. Missing git displays `git not available for diff`.

## Verification

`gofmt -l .`, `git diff --check`, `go vet ./...`, and `go build ./...` produced no output and exited 0.

`go test ./...`:

```text
ok  	github.com/horbo/stower/cmd/stower	0.606s
ok  	github.com/horbo/stower/internal/config	(cached)
ok  	github.com/horbo/stower/internal/doctor	2.301s
ok  	github.com/horbo/stower/internal/dotfiles	5.137s
ok  	github.com/horbo/stower/internal/stow	6.008s
ok  	github.com/horbo/stower/internal/tui	8.192s
ok  	github.com/horbo/stower/internal/tui/components	(cached)
ok  	github.com/horbo/stower/internal/tui/main	2.204s
```

`go test -race ./...`:

```text
ok  	github.com/horbo/stower/cmd/stower	4.663s
ok  	github.com/horbo/stower/internal/config	3.719s
ok  	github.com/horbo/stower/internal/doctor	4.162s
ok  	github.com/horbo/stower/internal/dotfiles	6.041s
ok  	github.com/horbo/stower/internal/stow	6.871s
ok  	github.com/horbo/stower/internal/tui	12.604s
ok  	github.com/horbo/stower/internal/tui/components	(cached)
ok  	github.com/horbo/stower/internal/tui/main	4.962s
```

Tests cover every doctor state and fix, normalized-name collisions, report-only cases, stale issues, traversal rejection, cancellation, rollback preserving both versions and unaffected links, missing links inside unfolded directories, restore blocking, actual GNU Stow execution, CLI results, diff exit code 1, missing git, and TUI wiring.

After the full checks, a small path-cleaning correction was verified with the doctor tests, and the final binary was rebuilt.

## Manual terminal scenario

Used the compiled binary with explicit `--target /private/tmp/stower-manual-3t90o5x6 --dotfiles /private/tmp/stower-manual-3t90o5x6/dotfiles`, continuing M4's throwaway fixture.

- Selected `misc`, pressed `r`, Enter, and `y`. The log displayed `✔ 1 package restored: misc`; `.bar` became a regular file with its original contents and `dotfiles/misc` disappeared.
- Replaced the `.config/foo` link with a real directory containing a changed `conf`. Status exited 1 and printed:

```text
PACKAGE  ENTRY           TARGET                                           STATE
foo      dot-config/foo  /private/tmp/stower-manual-3t90o5x6/.config/foo  replaced
```

- Refreshed Issues, opened Fix, and chose Keep TARGET. The log displayed `✔ 1 package fixed: foo`. The repository contained the replacement text, `.config/foo` was a symlink again, Issues was empty, and status exited 0 with `ok`.
- Removed that link, confirmed status exited 1 with `missing`, and ran the restow subcommand. It exited 0 and streamed:

```text
LINK: .config/foo => ../dotfiles/foo/dot-config/foo
```

- Status then exited 0 with `ok` again.

## Notes

There is no historical record of directory folding. A real content directory with matching regular replacement entries and no remaining correct links below it is reported as a directory-level replacement. Directories with correct links remain unfolded; directories with only missing expected files report those missing entries instead. This avoids interpreting an ordinary empty unfolded directory as a destructive repair request.

The existing restore conflict block remains in force, including when stow itself would silently skip replaced links. Doctor adds blocks for report-only mapping problems. Cancellation waits for the current stow subprocess before rollback, as in M4.

Git initialization, dirty markers, commit popups, and first-run setup remain M6. The existing asynchronous Home-inspection performance work remains deferred. No new third-party dependencies were added.

## Binary and demo

Binary: `/private/tmp/stower-workspace.QIrf2v/stower`.

Fresh demo: `/private/tmp/stower-demo.4my9_fs8`, containing `.bar` and `.config/foo/conf` ready to stage, plus an already linked `shell` package managing `.zshrc`.

```sh
/private/tmp/stower-workspace.QIrf2v/stower \
  --target /private/tmp/stower-demo.4my9_fs8 \
  --dotfiles /private/tmp/stower-demo.4my9_fs8/dotfiles
```

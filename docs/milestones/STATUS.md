# Status

| ID | Milestone           | Status | Started | Finished | Notes |
|----|---------------------|--------|---------|----------|-------|
| M0 | Bootstrap           | done | 2026-09-05 | 2026-09-05 | go 1.27.1; charm.land/{bubbletea v2.0.9, bubbles v2.2.1, lipgloss v2.0.6} |
| M1 | Core library        | done | 2026-09-05 | 2026-09-05 | 64 tests incl. stow integration; stow translates dot- at every level, stow -D skips replaced links |
| M2 | TUI skeleton        | done | 2026-09-05 | 2026-09-05 | styles/mainpanel subpackages, AltScreen via view.AltScreen, lipgloss v2 Compositor for layers; module renamed to github.com/horbo/stower |
| M3 | Staging             | done | 2026-09-05 | 2026-09-05 | tree component, Assign popup, Home/Staged contexts; .git toggle per package; HasNestedGit uncapped (debt) |
| M4 | Apply and log       | done | 2026-09-05 | 2026-09-05 | implemented by Codex after the Opus run hit the session limit; report in docs/reports/M4-report.md; emitter no longer selects on ctx.Done (see DESIGN) |
| M5 | Restore and doctor  | done | 2026-09-05 | 2026-09-05 | implemented by Codex; report in docs/reports/M5-report.md; review added RestowExcluding tests and split R (restow all) from ctrl+r (rescan) |
| M6 | Git                 | queued |         |          |       |

Statuses: `queued` → `in-progress` → `done`; `blocked` with the reason in Notes.
Dates are ISO `YYYY-MM-DD`.

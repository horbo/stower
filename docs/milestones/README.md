# Milestones

Implementation is split into milestones that build on each other. Each milestone is
implemented by one agent run, reviewed by the coordinating session, then committed as a
single commit named after the milestone (`M1: core library`).

## Files

- `STATUS.md`: the only source of truth for what is queued, in progress, done or blocked.
  Only the coordinating session edits it. Agents never touch it.
- `M<n>-<slug>.md`: one file per milestone with the fixed structure below.
- `../reports/M<n>-report.md`: optional written report from an implementation run when the
  agent cannot report in the conversation (for example an external agent). Never edit a
  milestone file to record results; results go to STATUS.md notes and, if needed, a report.

## Milestone file structure

```
# M<n> — <Title>
## Goal            one sentence: what the user can do once this milestone is done
## Depends on      previous milestone ids
## Scope           files and packages to create or change, with references to docs/DESIGN.md
## Out of scope    what is deliberately left for later; do not implement it
## Acceptance      checkable criteria: commands, behaviours, edge cases
## Verification    exact commands to run, plus the manual scenario on a throwaway target
## Notes           open questions to resolve and record in the report, decisions left open
```

## Agent contract

1. Read `CLAUDE.md`, `docs/DESIGN.md`, this file and the milestone file.
2. Implement exactly the `Scope`. Nothing from `Out of scope`. When something in scope turns
   out to be impossible or to contradict the design, implement everything else and say so
   explicitly in the report.
3. Run every command in `Verification` and quote the decisive output verbatim.
4. Do not commit. Do not edit `STATUS.md`.
5. Finish with a report: what was done, test output, what was skipped and why, answers to the
   questions in `Notes`, open questions for the coordinator.

## Review contract (coordinating session)

1. Set the milestone to `in-progress` in `STATUS.md` with the start date.
2. Spawn the agent (Opus) with the contract above.
3. On completion run `gofmt -l .`, `go vet ./...`, `go build ./...`, `go test ./...`
   independently, read the diff against `Acceptance` and the rules in `CLAUDE.md`.
4. Send fixes back to the same agent instead of starting a new one.
5. When green: `done` with the finish date in `STATUS.md`, one commit, next milestone.

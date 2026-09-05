# M11 — License and third-party notices

## Goal

The repository and every released archive carry an MIT license for stower and the license
texts of all bundled third-party code, so the Homebrew formula and the GitHub releases are
legally distributable.

## Depends on

M10.

## Scope

- `LICENSE`: MIT, copyright holder `Kamil Horbowicz`, year 2026.
- `THIRD_PARTY_NOTICES.md`: one section per module that ends up in the binary, generated
  from `go list -deps -m -f '{{.Path}} {{.Version}}' ./cmd/stower` (direct and indirect,
  excluding the standard library and the main module). Each section names the module path,
  version, license identifier and quotes the license text verbatim from the module cache
  (`$(go env GOMODCACHE)/<module>@<version>/LICENSE*`). Modules without a license file are
  listed with `license file not found` so the coordinator can decide.
- `tools/notices.sh` (or a Go program under `tools/notices` if shell gets awkward): regenerates
  `THIRD_PARTY_NOTICES.md` deterministically. Validate every path it touches; no `eval`, no
  unquoted expansions. Add a `make notices` target and a `make check-notices` target that
  fails when the committed file differs from a fresh generation; wire `check-notices` into
  `make check`.
- `.goreleaser.yaml`: add `LICENSE` and `THIRD_PARTY_NOTICES.md` to the archive `files` list,
  set `license: "MIT"` on the `brews` entry.
- `README.md`: a short `License` section pointing at both files.
- `docs/RELEASING.md`: one line that `make notices` must be run after any dependency change.

## Out of scope

Changing dependencies, SPDX headers in source files, a NOTICE for stow or git (they are
runtime dependencies invoked as subprocesses and are not redistributed), CLA or contribution
guidelines.

## Acceptance

- `LICENSE` is the unmodified MIT text with the holder and year filled in.
- `THIRD_PARTY_NOTICES.md` lists every module reported by `go list -deps -m ./cmd/stower`
  other than the main module and the standard library, with its license text.
- `make check-notices` passes on a clean tree and fails after editing the committed file.
- `make check` still passes.

## Verification

```
gofmt -l .
go vet ./...
go build ./...
go test ./...
make notices && git status --short THIRD_PARTY_NOTICES.md
make check-notices
```

## Notes

- Charm modules (`charm.land/*`, `github.com/charmbracelet/*`) are MIT; transitive modules
  such as `golang.org/x/*` are BSD-3-Clause and `github.com/mattn/*` are MIT. Confirm each one
  from the actual license file, do not assume.
- Decide whether `go list -deps` (packages actually linked) or `go mod graph` (everything in
  go.sum) is the right source; the recommendation is `-deps` on `./cmd/stower`, because only
  linked code is redistributed.

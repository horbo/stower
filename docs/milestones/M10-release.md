# M10 — Release pipeline and Homebrew tap

## Goal

A maintainer can publish a release by pushing a `v*` tag: GitHub Actions builds the binaries
with goreleaser, creates the GitHub Release and updates the formula in the Homebrew tap
`horbo/homebrew-tap`, so users install stower with `brew install horbo/tap/stower`.

## Depends on

M8. Independent of M9.

## Scope

- `.goreleaser.yaml` (goreleaser v2 schema): one build of `./cmd/stower`, `CGO_ENABLED=0`,
  `GOOS` darwin and linux, `GOARCH` amd64 and arm64, `-trimpath`, ldflags
  `-s -w -X main.version={{.Version}}`, `mod_timestamp` for reproducible builds. Archives as
  `tar.gz` named `stower_<version>_<os>_<arch>` containing the binary, `README.md` and
  `LICENSE` if one exists (do not create a license file; note its absence in the report).
  Checksums file. Changelog from commit subjects, excluding `docs:` and `test:` prefixes and
  merge commits. `brews` entry: `name: stower`, repository `horbo/homebrew-tap`, branch
  `main`, token from `{{ .Env.HOMEBREW_TAP_TOKEN }}`, directory `Formula`, homepage
  `https://github.com/horbo/stower`, description matching README, `depends_on "stow"`
  (runtime, not build), `install` does `bin.install "stower"`, `test` runs
  `stower --version` and asserts the version string. Commit author `stower-release-bot`
  or similar, commit message `stower <version>`. `skip_upload: auto` so pre-release tags
  (`v0.1.0-rc1`) do not touch the tap.
- `.github/workflows/release.yml`: trigger on push of tags matching `v*`, `permissions:
  contents: write`, `actions/checkout@v4` with `fetch-depth: 0`, `actions/setup-go@v5` with
  `go-version-file: go.mod`, `goreleaser/goreleaser-action@v6` with `version: "~> v2"` and
  `args: release --clean`; env `GITHUB_TOKEN` from `secrets.GITHUB_TOKEN` and
  `HOMEBREW_TAP_TOKEN` from `secrets.HOMEBREW_TAP_TOKEN`. Pin every action to a major tag.
- `.github/workflows/ci.yml`: on push to `main` and on pull requests, `ubuntu-latest` and
  `macos-latest`, install stow (`apt-get install -y stow` / `brew install stow`), run
  `make check`. Keep it minimal; no caching tricks beyond what `setup-go` does by default.
- `Makefile`: add `snapshot` target running `goreleaser release --snapshot --clean` into
  `dist/` (already gitignored). Do not install goreleaser; if it is not on `PATH`, the target
  prints a one-line hint and exits 1.
- `cmd/stower/main.go`: keep `var version = "dev"`; confirm the ldflags path `main.version`
  matches. Add a test in `cmd/stower` only if none covers `--version` yet.
- `README.md`: `brew install horbo/tap/stower` becomes the first installation option, with
  `brew upgrade stower` mentioned; keep `go install` and source builds below it.
- `docs/RELEASING.md`: step-by-step for the maintainer: one-time setup (create the public
  empty repository `horbo/homebrew-tap`, create a fine-grained personal access token limited
  to that repository with `Contents: read and write`, store it as the `HOMEBREW_TAP_TOKEN`
  secret on `horbo/stower`), the release procedure (`make check`, `git tag -a v0.1.0 -m
  v0.1.0`, `git push origin v0.1.0`), how to test locally with `make snapshot`, and how to
  recover when the tap push fails (rerun the workflow, or `goreleaser release --clean`
  locally with both tokens exported).
- `docs/DESIGN.md`: the post-v1 line that lists "goreleaser and a Homebrew tap" as an idea
  gets updated to point at `docs/RELEASING.md`.

## Out of scope

Creating the `horbo/homebrew-tap` repository, creating tokens or secrets, pushing tags,
signing or notarising binaries, Linux packages (deb/rpm), Windows builds, nfpm, cosign,
a Homebrew core submission.

## Acceptance

- `goreleaser check` passes on `.goreleaser.yaml` when goreleaser is available locally; if it
  is not, the report says so and the YAML is at least validated against the documented v2
  schema by careful reading.
- `make snapshot` (when goreleaser is available) produces `dist/stower_*_darwin_arm64.tar.gz`
  and the extracted binary prints the snapshot version with `--version`.
- `go build -ldflags "-X main.version=1.2.3" ./cmd/stower && ./stower --version` prints
  `stower 1.2.3`.
- Both workflow files are valid YAML and reference only pinned major action tags.
- README installation order: brew, go install, source.
- All existing tests still pass.

## Verification

```
gofmt -l .
go vet ./...
go build ./...
go test ./...
goreleaser check            # if goreleaser is on PATH
make snapshot               # if goreleaser is on PATH; inspect dist/
```

## Notes

- goreleaser v2 renamed several keys (`brews[].repository` instead of `tap`, `directory`
  instead of `folder`). Use the v2 names; check the goreleaser version on PATH if present.
- Decide and record whether the formula should `depends_on "git"`: git is optional at
  runtime in stower, so the recommendation is to leave it out and mention it in the caveats.
- Record the exact goreleaser version the config was validated against, if any.

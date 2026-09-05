# Releasing

A release is published by pushing a `v*` tag. GitHub Actions runs goreleaser, which builds
the binaries, creates the GitHub Release and updates the formula in the Homebrew tap so that
`brew install horbo/tap/stower` serves the new version.

Everything is driven by two files:

- `.goreleaser.yaml` — builds, archives, checksums, changelog, the `brews` entry.
- `.github/workflows/release.yml` — the tag-triggered workflow.

## One-time setup

1. Create the public repository `horbo/homebrew-tap` on GitHub. It can be empty; goreleaser
   creates `Formula/stower.rb` on the first release. The name must start with `homebrew-`,
   that is what makes `horbo/tap` a valid tap shorthand.
2. Create a fine-grained personal access token:
   - Resource owner `horbo`, repository access limited to `horbo/homebrew-tap` only.
   - Repository permission `Contents: read and write`. Nothing else.
   - Give it an expiry you are willing to renew; the release fails loudly when it lapses.
3. Store the token on `horbo/stower` as the repository secret `HOMEBREW_TAP_TOKEN`
   (Settings → Secrets and variables → Actions → New repository secret).

`GITHUB_TOKEN` needs no setup: the workflow uses the automatic token, and
`permissions: contents: write` lets it create the release on `horbo/stower`.

## Release procedure

```sh
make check
git tag -a v0.1.0 -m v0.1.0
git push origin v0.1.0
```

`make check` must be green before tagging: the workflow does not run the tests, it only
builds. The tag must be annotated and must be semver with a leading `v`; goreleaser derives
`{{ .Version }}` from it, and the binary reports it as `stower 0.1.0`.

Watch the run under Actions. On success you get:

- a GitHub Release with `stower_<version>_<os>_<arch>.tar.gz` for darwin and linux, amd64
  and arm64, plus `checksums.txt`;
- a commit `stower <version>` by `stower-release-bot` in `horbo/homebrew-tap`.

Verify from a user's point of view:

```sh
brew update && brew install horbo/tap/stower && stower --version
```

## The `brews` deprecation

goreleaser prints `DEPRECATED: brews should not be used anymore` on every run, and
`goreleaser check` exits non-zero for that reason alone. This is expected. The suggested
replacement, `homebrew_casks`, is macOS only, so it would drop `brew install` for the linux
builds and it has no `test` block. `brews` keeps working for the whole v2 line, which is what
`version: "~> v2"` in the workflow pins. Revisit before goreleaser v3.

## Pre-releases

Tags with a semver pre-release part, `v0.1.0-rc1` for example, are built and uploaded to the
GitHub Release but do not touch the tap: the `brews` entry uses `skip_upload: auto`. Use them
to exercise the pipeline without pushing a broken formula to users.

## Testing locally

```sh
make snapshot
```

runs `goreleaser release --snapshot --clean`: it builds every target into `dist/` without a
tag, without publishing anything and without touching the tap. `dist/` is gitignored. Check
the result:

```sh
tar -tzf dist/stower_*_darwin_arm64.tar.gz
tar -xzf dist/stower_*_darwin_arm64.tar.gz -O stower > /tmp/stower && chmod +x /tmp/stower
/tmp/stower --version
```

`make snapshot` requires goreleaser on `PATH` (`brew install goreleaser`); it does not install
it and exits 1 with a hint when it is missing. Validate the config alone with
`goreleaser check`.

## When the tap push fails

The GitHub Release is created before the tap is updated, so a failure at the `brews` step
leaves a complete release and a stale formula. Nothing has to be re-tagged.

1. Fix the cause. Almost always it is the token: expired, missing `Contents: read and write`,
   or not granted to `horbo/homebrew-tap`. Update the `HOMEBREW_TAP_TOKEN` secret.
2. Re-run the failed job from the Actions page. goreleaser is idempotent enough here: the
   release already exists and gets its artifacts re-uploaded.
3. If re-running is not an option, publish from a clean checkout of the tag:

   ```sh
   git checkout v0.1.0
   export GITHUB_TOKEN=<token with contents:write on horbo/stower>
   export HOMEBREW_TAP_TOKEN=<token with contents:write on horbo/homebrew-tap>
   goreleaser release --clean
   ```

   Delete the GitHub Release first if goreleaser refuses to overwrite it.
4. As a last resort the formula is one file: edit `Formula/stower.rb` in the tap by hand, with
   the new `version`, the four `url`s and the `sha256`s from `checksums.txt` of the release.

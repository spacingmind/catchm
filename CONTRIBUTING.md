# Contributing to CatchM

## Branching model

- **`develop` is the integration branch.** Base your feature/fix branch
  on `develop` and open your PR against `develop`.
- **`master` is release-only.** It only receives merges via a
  `develop -> master` promotion PR, and pushes to it drive the release
  machinery (release-please, sync-develop). Don't open feature PRs
  directly against `master`.
- Don't push tags by hand. Release tags are created by release-please
  (see the release runbook below) — a hand-pushed `v*` tag triggers the
  full artifact publish (GitHub release + the R2 update manifest that
  shipped clients auto-update from at runtime), so it must stay
  deliberate.

## Conventional Commits

PR titles must follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat: ...` — a new feature
- `fix: ...` — a bug fix
- `chore: ...` / `docs: ...` / `refactor: ...` — everything
  release-please excludes from the changelog

`pr-title-lint.yml` enforces the format. Individual commits on your
feature branch don't need to follow it strictly, but the PR title does —
it becomes the changelog entry when the next release is cut.

## Local development

```
task build   # Go binary + embedded web UI
task test    # go test ./...
task lint    # go vet + gofmt check
task run     # the daemon (catchm serve)
```

Desktop shell (Windows-specific NSIS/installer work; needs the
[wails](https://wails.io) CLI and, for the installer, NSIS):

```
task desktop:dev
task desktop:build
task desktop:release VERSION=1.2.3 UPDATE_BASE_URL=https://catchm-api.spacingmind.com/update
```

## Release runbook

The whole flow from "code merged on develop" to "shipped clients
updating themselves" — only the promotion clicks are human:

1. **Promote**: open a PR `master` <- `develop` ("release promotion").
   `ci.yml`'s release-metadata guard runs on it (fails if the promotion
   would regress the version manifest or drop changelog entries —
   merge the pending sync-develop PR first if it complains). Merge it
   with **rebase-merge** (master is linear-history-only).
2. **Release PR**: the merge triggers
   `.github/workflows/release-please.yml`, which opens/updates
   `chore(master): release X.Y.Z` (version bump in
   `.release-please-manifest.json` + `CHANGELOG.md`, computed from
   Conventional-Commit PR titles since the last release). Review and
   merge it (also rebase-merge).
3. **Tag + build**: merging the release PR makes release-please create
   the `vX.Y.Z` tag and a skeleton GitHub Release. The tag push
   triggers `.github/workflows/release.yml`, which builds the NSIS
   installer (`task desktop:release`), attaches it + its SHA-256
   checksum to the release, builds `latest.json`, and uploads
   everything to the catchm-releases R2 bucket — `latest.json` last,
   so a half-uploaded release is never advertised. That R2 manifest is
   what shipped clients actually poll (`catchm-api`'s `/update`
   route); the GitHub release is kept as an internal record.
4. **Sync back**: pushing to master also triggers
   `.github/workflows/sync-develop.yml`, which opens
   `chore: sync develop with master after vX.Y.Z release` against
   `develop`. **Merge that PR before the next promotion** — skipping it
   is exactly what clobbered release metadata in this org's smind repo
   once (v0.6.0 -> v0.7.0); the ci.yml guard is the second line of
   defense.

### Dry runs

`release.yml` also has a `workflow_dispatch` with `dry_run` (default
true): builds + checksums the installer and uploads it as a workflow
artifact, skipping the GitHub release, manifest, and R2 upload. Use it
to exercise the Windows build without publishing anything.

### What a release must never do

- Ship with updates silently off: `desktop:release` refuses an empty
  `UPDATE_BASE_URL`, and `release.yml` fails the build if the
  `UPDATE_BASE_URL` repo variable is missing.
- Publish a manifest that lies about asset sizes: `release.yml` reads
  sizes off the built files, not from earlier steps.
- Overwrite `latest.json` before its assets exist: R2 upload order puts
  the manifest last.

## Repo settings this model depends on

(hand-configured once, after the repo went public:)

- Default branch: `develop`.
- Ruleset on `master`: linear history, rebase-merge only, no direct
  pushes.
- Repo variable `UPDATE_BASE_URL`; secrets `R2_ACCESS_KEY_ID`,
  `R2_SECRET_ACCESS_KEY`, `CLOUDFLARE_ACCOUNT_ID`.

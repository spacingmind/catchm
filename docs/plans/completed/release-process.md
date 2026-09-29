# Release process completion (single-branch develop + bot tag)

## Problem

M23 left tagging as a fully manual, unvalidated step ("open question"
in the plan, never resolved): a human runs `git tag vX.Y.Z && git push
--tags` on an arbitrary commit and `release.yml` publishes from there.
Nothing checks the version against the last release, the branch, or
the tree state — and every tag publishes to the R2 bucket that shipped
clients auto-update from at runtime. No CONTRIBUTING.md documents any
of it, and `master` sat 13 commits behind develop with no defined role.

## Decision (closes M23's open question)

**Single integration branch (`develop`) + a bot-created release tag**,
the model goreleaser itself and syncthing use (surveyed before
deciding; the earlier draft of this plan copied smind's
release-please + master/develop split — rejected, see below).

- All development merges to `develop` via PR (unchanged, the existing
  convention).
- A release is cut by running the `tag-release` workflow dispatch with
  a version. The workflow (as github-actions[bot]) validates: plain
  x.y.z format, strictly greater than the latest existing tag, tag
  doesn't already exist. It then creates an **annotated** tag on
  origin/develop HEAD, pushes only that ref, and dispatches the
  existing `release.yml` on it (installer + checksum + GitHub
  release + R2 manifest). The original draft had the tag push trigger
  `release.yml` directly — it can't; see Decisions.
- A final step in the same job waits for that release run and, only
  on its success, fast-forwards `master` to the tag, so
  `master` = "code that has been released" — a read-only pointer,
  never a merge target, no sync-back problem by construction.

Why not smind's release-please model (the earlier draft):

- Its master/develop split needs three guard mechanisms (sync-develop
  workflow, check-release-metadata.sh, rulesets) purely to patch the
  "release commit lands on master, develop never absorbs it" failure
  that already bit smind for real (v0.6.0→v0.7.0). A single-branch
  model can't develop that failure in the first place.
- No large surveyed project uses that split unless forced to
  (goreleaser: single branch + bot-pushed tags, releases authored by
  github-actions[bot]; syncthing: bot tags a release branch because it
  maintains parallel backports, which catchm doesn't).
- release-please's changelog-from-PR-titles buys nothing here:
  release.yml already generates release notes (`--generate-notes`).

## Changes

1. `.github/workflows/tag-release.yml` (new): `workflow_dispatch`
   with `version` and `dry_run` inputs; validate → tag as bot → push
   tag → dispatch `release.yml` on the tag; after that run succeeds,
   fast-forward `master` to the tag.
2. CONTRIBUTING.md (new): single-branch model, PR conventions, the
   release runbook (dispatch → validation → tag → release.yml → R2 →
   master fast-forward), dry-run instructions.
3. Plan doc (this file).

`release.yml`, `ci.yml`, and the Taskfile stay unchanged by this plan
(`f67ddca` in #34 later added a case-collision check to CI, unrelated).

## Out of scope

- The stale `master` branch's current position: the first release
  under this model fast-forwards it onto develop history; until then
  the divergence is harmless (nothing reads master).

## Acceptance criteria

- `tag-release.yml` YAML parses; validation logic rejects: malformed
  version, version <= latest tag, existing tag.
- A dispatch with a valid future version creates the tag as
  github-actions[bot] and pushes exactly that ref (validated on the
  first real release; `dry_run` exercises validation + tag creation
  locally without pushing).
- CI green on the PR.

## Decisions

- **tag-release dispatches release.yml** (`03be6e4`). A tag pushed
  with `GITHUB_TOKEN` never triggers another workflow (GitHub's
  anti-recursion rule; `workflow_dispatch` is exempt). On the first
  real release (v0.2.0) the tag landed but `release.yml` never ran,
  and the run-list call in the master step answered `HTTP 403:
  Resource not accessible by integration`. Fix: tag-release dispatches
  `release.yml` on the tag and polls that dispatch run; the workflow
  gets `actions: write`. `release.yml` itself is still unchanged.
- **Fast-forward master to the tag's commit** (`edb56a5`). v0.2.1's
  release succeeded but `git push origin v0.2.1:refs/heads/master`
  was rejected (`! [remote rejected] v0.2.1 -> master (failed)`): the
  tag is annotated, and a branch must point at a commit, not a tag
  object. Fix: push `v$VERSION^{commit}`.
- The "tag already exists" check is effectively shadowed by the
  "strictly greater than latest" check (an existing tag is always
  <= the latest). Kept as a cheap defensive guard.

## Validation

**Real releases (2026-09-28).** Every release ran a `dry_run` dispatch
first, then the real one:

| Version | Commit    | Real run      | Outcome |
|---------|-----------|---------------|---------|
| v0.2.0  | `17bc59e` | `36388813238` | tag pushed; release.yml not triggered + 403 → `03be6e4`; release.yml dispatched by hand (`36388931774`, green) |
| v0.2.1  | `e916b71` | `36391738566` | tag + release green; master push rejected → `edb56a5` |
| v0.3.0  | `d963c4a` | `36403004300` | release.yml failed (Windows case-only filename collision, fixed in #34); guard correctly refused to move master |
| v0.3.0  | `b62f6c6` | `36404031665` | end to end green: tag, release (`36404051921`), `e916b71..b62f6c6 v0.3.0^{commit} -> master` |

All three tags are annotated with tagger `github-actions[bot]`;
`origin/master` = `v0.3.0^{commit}` = `b62f6c6`.

**Validation logic** (step script extracted from the workflow and run
locally against the repo's tags, latest `v0.3.0`): rejects `0.3`,
`v0.4.0`, `0.4.0-rc1` (format), `0.2.9`, `0.3.0` (not greater);
accepts `0.3.1` and `0.10.0` (numeric, not lexical, ordering).

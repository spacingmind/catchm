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
  origin/develop HEAD and pushes only that ref — which triggers the
  existing, unchanged `release.yml` (installer + checksum + GitHub
  release + R2 manifest).
- A separate `master-ff` job in the same workflow, gated on the
  release workflow's success, fast-forwards `master` to the tag, so
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
   tag; after the resulting release workflow succeeds, fast-forward
   `master` to the tag.
2. CONTRIBUTING.md (new): single-branch model, PR conventions, the
   release runbook (dispatch → validation → tag → release.yml → R2 →
   master fast-forward), dry-run instructions.
3. Plan doc (this file).

`release.yml`, `ci.yml`, and the Taskfile stay unchanged.

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

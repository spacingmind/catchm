# Release process completion (smind model: master/develop + release-please)

## Problem

M23 left tagging as a fully manual, unvalidated step ("open question"
in the plan, never resolved): a human runs `git tag vX.Y.Z && git push
--tags` on some commit and `release.yml` publishes from there. Nothing
checks the tag's version against the last release, the branch, or the
tree state — and every tag publishes to the R2 bucket that shipped
clients auto-update from at runtime. There is also no CONTRIBUTING.md
and the stale `master` branch (13 commits behind develop) shows the
repo never had a branching model.

The repo is now approved to be public, which unlocks rulesets
(currently 403: "Upgrade to GitHub Pro or make this repository public")
— the guardrails smind's model depends on.

## Decision (closes M23's open question)

Adopt smind's proven model, adapted for a tag-driven pipeline with real
artifacts:

- `master` = release-only; `develop` = integration. Feature PRs target
  `develop` with Conventional Commits titles (already the de-facto
  convention here).
- A **develop → master promotion PR** triggers nothing by itself; the
  merge (rebase, per master's linear-history ruleset) runs CI on
  master.
- Tagging moves from "manual, unvalidated" to **release-please-driven**:
  merging the develop→master promotion lets release-please open
  `chore(master): release X.Y.Z` (version bump + CHANGELOG from PR
  titles); merging THAT creates the `vX.Y.Z` tag → `release.yml` builds
  the installer, publishes the GitHub release + R2 manifest exactly as
  today. The artifact half of the pipeline is unchanged.
- `sync-develop.yml` + `check-release-metadata.sh` replicate smind's
  protection against the promotion clobbering release-please's metadata
  commit (the real v0.6.0→v0.7.0 incident there).

Differences from smind, deliberate:

- release-please creates the tag on its release PR merge; catchm's
  `release.yml` (triggered by that same tag push) does the artifact
  build + R2 publish — smind has no artifacts, so it stops at the
  GitHub Release.
- No `draft: false` change / extra config beyond catchm's needs:
  single package, `include-component-in-tag: false` (catchm's tags are
  plain `vX.Y.Z`, not `catchm-vX.Y.Z` — the four existing tags must
  keep resolving).
- ci.yml keeps its catchm-specific steps (web UI build, Taskfile
  validation); only the branch list and the release-metadata guard are
  added.

## Changes

1. `.github/workflows/release-please.yml` (new) — on push to master;
   pinned to the same action SHA smind uses.
2. `release-please-config.json` + `.release-please-manifest.json`
   (new) — seeded at `0.1.3` (the current release/tag).
3. `.github/workflows/sync-develop.yml` (new) — smind's workflow,
   branch names adjusted.
4. `.github/workflows/ci.yml` — run on master pushes too; add the
   develop→master release-metadata guard step.
5. `scripts/check-release-metadata.sh` (new) — smind's script.
6. `.github/workflows/pr-title-lint.yml` (new) — Conventional Commits
   PR titles (release-please depends on them).
7. `CONTRIBUTING.md` (new) — branching model, PR conventions, the full
   release runbook (promote → release PR → tag → release.yml → R2),
   and the M23 resolution.
8. Repo settings (manual, after the PR merges and the repo is public):
   ruleset for master (linear history, rebase-merge only, no direct
   pushes) and default branch = develop.

## Out of scope

- Deleting/archiving the stale `master` branch content — the promotion
  flow re-uses the branch; its stale commits are simply ancestors of
  the first promotion.
- Renaming `master` to `main` (develop's CI config references both
  today; not worth churn).

## Acceptance criteria

- `ci.yml` validates on this PR; `task --list-all` still clean.
- The workflows' YAML parses (actionlint-style check via `gh workflow
  view`/gh api after push, or a yq parse locally).
- release-please + sync-develop trigger correctly on the first real
  promotion (validated in use, not in this PR — same as smind's
  rollout).
- CONTRIBUTING.md matches release.yml's actual gates (dry_run default,
  R2 upload conditions) — verified line-by-line against the workflow.

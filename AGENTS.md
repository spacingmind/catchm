# AGENTS.md

Repository protocol entrypoint for agents working in this repo.

## Project overview

See [README.md](README.md). One line: catch meeting audio, transcribe/
translate, keep per-meeting context as agent-readable files + MCP tools.

## Rules

**(a) Read-only questions.** Inspect the smallest relevant surface, answer
with evidence (file paths, line numbers, quoted code). Don't guess.

**(b) Bounded changes.** Smallest coherent change that satisfies the
request. Run `task test` and `task lint` before considering it done.

**(c) Spec-driven work.** For nontrivial changes, create
`docs/plans/active/<slug>.md` with acceptance criteria before
implementing; move to `docs/plans/completed/` when validated.

**(d) Material ambiguity.** If a choice materially affects architecture
(data model, store layout, tool surface, wire protocol) and isn't already
decided, STOP and present the choice to the user.

**(e) UI consistency.** UI changes follow [docs/ui-conventions.md](docs/ui-conventions.md)
(type scale, control sizes, spacing, patterns). Match peers; reuse the
shared primitives rather than one-off styles.

## refs/

Read-only clones for pattern lookup (gitignored):

- `refs/my-translator` — audio capture per-platform, PCM batching,
  pluggable translation engines.
- `refs/minutes` — MCP tool surface, frontmatter schema, cursor-based
  JSONL live reads.
- `refs/meetily` — local STT pipeline, chunking, diarization.
- `refs/modelcontextprotocol-go-sdk` — external MCP server.

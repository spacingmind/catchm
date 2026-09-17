# CatchM

**Catch the meeting. Keep the mind.**

Self-hosted meeting companion: listens to meetings, transcribes and
translates in real time, and turns each meeting into agent-readable
context (markdown + frontmatter + MCP tools). Single Go binary serving an
API + web UI on localhost, same shape as smind.

## Dev

```sh
task build    # build the catchm binary (bin/catchm)
task test     # go test ./...
task lint     # go vet + gofmt check
```

## Layout

- `cmd/catchm` — binary entrypoint (`catchm serve`).
- `internal/meeting` — session model + store (JSONL transcript, markdown
  + frontmatter, FTS5).
- `internal/tools` — meeting tool surface (web UI + MCP consumers).
- `internal/capture`, `internal/stt`, `internal/translate` — audio and
  engine layers (later milestones).
- `internal/agent` — enrichment jobs via claude-agent-sdk-go.
- `internal/server` — HTTP API + embedded web UI.
- `docs/plans/` — spec-driven plans.
- `refs/` — read-only reference clones (gitignored; see AGENTS.md).

MIT.

# Plan: M1 — meeting store + tool surface + enrich job (no audio yet)

- Status: active
- Created: 2026-09-17

## Goal

Validate the context model before audio exists: a meeting store with
JSONL live transcripts + markdown/frontmatter documents, a typed tool
surface over it, and an enrichment job that runs a real Claude Code agent
(in-process MCP tools) to extract decisions/action items into the
frontmatter. Feed with synthetic JSONL fixtures.

## Scope

- `internal/meeting`: session lifecycle (start/end), JSONL append +
  cursor reads, markdown + frontmatter document write/read, FTS5 index.
- `internal/tools`: typed surface — `list_meetings`, `get_meeting`,
  `search_meetings`, `read_live_transcript`, `add_note`.
- `internal/agent`: `EnrichMeeting` job via claude-agent-sdk-go
  (`WithSDKMcpServer`, `acceptEdits`, budget-capped).
- `cmd/catchm serve`: minimal HTTP server exposing health + the tool
  surface as REST, embedded web UI shell (meeting list + transcript view).
- Test fixtures: synthetic meetings under `internal/meeting/testdata`.

## Acceptance criteria

1. `task build && task test && task lint` pass in CI shape.
2. Starting a meeting, appending utterances, ending it produces:
   `utterances.jsonl` + `meeting.md` on disk under the meetings dir.
3. Cursor-based reads return only new utterances (named test:
   `TestReadSinceCursor`).
4. `EnrichMeeting` on a fixture meeting updates frontmatter with
   decisions/action items (integration test, skipped without `claude` CLI
   on PATH — same pattern as claude-agent-sdk-go's fakecli tests).
5. Web UI at `:4649` lists meetings and renders a transcript (fixture
   data is fine). — DEFERRED (intentionally out of M1; REST API at
   `:4649` is wired via `cmd/catchm serve`).

## Decisions

- Store layout per ADR-0002; tool surface per ADR-0003.
- No audio capture, no STT, no translation in M1.
- Enrichment: agent returns a JSON object; Go applies it to frontmatter
  via `store.UpdateDoc` — the agent has no write path.
- `CATCHM_HOME` overrides the `~/.catchm` base (store at
  `$CATCHM_HOME/meetings`); `CATCHM_ADDR` overrides `:4649`.
- The CLI's session gate blocks SDK MCP tools before the permission
  policy is consulted; enrichment pre-approves `mcp__catchm__*` via
  `WithAllowedTools` for headless runs.

## Progress

- [x] internal/meeting
- [x] internal/tools
- [x] internal/agent
- [x] internal/server (REST; web UI shell deferred)
- [x] fixtures + tests

## Validation

Verified 2026-09-18, Go 1.27, all green:

- `go mod tidy && go build ./...` clean;
  `go test ./...` ok (agent, meeting, server, tools);
  `go vet ./...` clean; `gofmt -l .` empty. `task build`/`task test`/
  `task lint` equivalent commands pass.
- Cursor semantics covered by named test `TestAppendAndReadSinceCursor`
  (internal/meeting): initial read returns all + cursor, delta read
  returns only new utterances, empty delta keeps cursor, append after
  end returns `ErrNotActive`.
- REST surface (internal/server, httptest): health, start, list, get,
  transcript with `?cursor=` delta, add note, end, and 404 for unknown
  meeting across all id-scoped routes.
- `EnrichMeeting` end-to-end with real `claude` CLI on PATH:
  fixture meeting's frontmatter gained decisions ("Freeze the scope by
  Friday"), action items ("Prepare the demo and send the timeline",
  owner sam), participants [alex sam], and a summary. The test skips
  via `exec.LookPath("claude")` when the CLI is absent.
- Unit tests cover prompt building, JSON extraction (fenced/prose/
  non-JSON), applyExtraction persistence, and the MCP tool handlers
  (unknown id, numeric + string cursor args) without the CLI.
- `catchm serve` wires store + surface + server with SIGINT/SIGTERM
  graceful shutdown (5s drain); smoke-tested via `go build`.
- Fixed during M1: `parseDoc` left the doc header/title in `Summary` on
  round-trip (header strip + `# Title` strip now handled).

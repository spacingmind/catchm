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
   data is fine).

## Decisions

- Store layout per ADR-0002; tool surface per ADR-0003.
- No audio capture, no STT, no translation in M1.

## Progress

- [ ] internal/meeting
- [ ] internal/tools
- [ ] internal/agent
- [ ] internal/server + web UI shell
- [ ] fixtures + tests

## Validation

(to be filled as criteria are confirmed)

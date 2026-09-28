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

### Desktop shell (optional)

`cmd/catchm-desktop` opens the same UI/API in a native window instead of a
browser tab, via [Wails v2](https://wails.io), reusing `internal/server`'s
`http.Handler` directly (no separate frontend, no extra API). It's
additive and opt-in — not part of `task build`/`task test`.

Building it requires system webview packages (Linux):

```sh
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config
# (use libwebkit2gtk-4.0-dev instead on older distros)
go install github.com/wailsapp/wails/v2/cmd/wails@latest

task desktop:dev     # wails dev
task desktop:build   # wails build
```

### Evaluating summary quality

`catchm eval` runs your configured summary provider over a set of gold
meetings and scores what it finds, so you can compare models and prompts
before changing a default:

```sh
go run ./cmd/catchm eval                                  # provider + model from your config
go run ./cmd/catchm eval --provider ollama --model qwen3.5:9b
go run ./cmd/catchm eval --dir eval-local --json          # your own gold files, machine-readable
```

- Gold files are JSON: `{title, lang, transcript: [{seq, speaker, text}],
  decisions: [text], actions: [{text, owner?, due?}]}`. The committed set in
  `internal/summarize/eval/testdata/` is synthetic (EN, VI, bilingual, a
  process walkthrough and a no-decisions catch-up).
- Put real transcripts in `eval-local/`. Git ignores that directory.
- Scores, per file and averaged: JSON validity, decision recall/precision,
  action recall/precision, owner and due accuracy, evidence validity, and
  the count of unsupported items. `-` means the metric doesn't apply to
  that file.
- The command reads the config from `CATCHM_HOME` and never writes to it.
  `catchm-cloud` isn't supported from the CLI yet, because it needs the
  app's logged-in session.

## Layout

- `cmd/catchm` — binary entrypoint (`catchm serve`, `catchm eval`).
- `cmd/catchm-desktop` — optional native desktop shell (Wails v2), same
  handler as `cmd/catchm`, no duplicate frontend/API.
- `internal/meeting` — session model + store (JSONL transcript, markdown
  + frontmatter, FTS5).
- `internal/tools` — meeting tool surface (web UI + MCP consumers).
- `internal/capture`, `internal/stt`, `internal/translate` — audio and
  engine layers (later milestones).
- `internal/summarize` — meeting summaries, decisions and action items
  via one structured-output LLM call: local Ollama (default), CatchM
  Cloud, or any OpenAI-compatible server.
- `internal/server` — HTTP API + embedded web UI.
- `docs/plans/` — spec-driven plans.
- `refs/` — read-only reference clones (gitignored; see AGENTS.md).

MIT.

# CatchM

**Catch the meeting. Keep the mind.**

Self-hosted meeting companion: listens to meetings, transcribes and
translates in real time, and turns each meeting into agent-readable
context (markdown + frontmatter + MCP tools). Single Go binary serving an
API + web UI on localhost, same shape as smind.

## Install

Download the latest release:

- **Windows 10/11:** `catchm-desktop-amd64-installer.exe`. It updates
  itself in-app.
- **macOS (Apple Silicon):** `catchm-desktop-arm64.dmg`. Open it and drag
  CatchM to Applications. The app isn't signed by Apple yet, so the
  first launch is blocked once. Right-click CatchM → Open, or on macOS 15+
  open System Settings → Privacy & Security and choose **Open Anyway**.
  macOS may ask for microphone access again after each update, because
  every build carries a new ad-hoc signature. CatchM tells you when an
  update is out; download it the same way. There's no menu-bar icon on
  macOS: closing the window minimises it to the Dock, and recording keeps
  running.

  **Computer audio on macOS** (the other people in Zoom/Meet) needs
  macOS 14.2 or later. The first meeting that records it asks for
  "System Audio Recording" permission; allow it, or turn CatchM on later
  in System Settings → Privacy & Security → Screen & System Audio
  Recording. Without it the meeting keeps running (and still records
  your microphone if the source is "both"), and the live view says no
  computer audio is arriving.
  Running `catchm serve` from a terminal instead of the app? macOS
  attributes the permission to the terminal app (Terminal, iTerm…).

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

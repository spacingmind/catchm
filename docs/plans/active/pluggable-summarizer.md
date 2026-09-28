# Plan: Pluggable summarizer (drop Claude Code)

- Status: in progress
- Created: 2026-09-28
- Branch: `fix/non-tech-ux`. It shares the working tree with
  `non-tech-ux.md` (now in completed/), because MeetingDetail and Settings copy overlap.
- Related repos:
  - `../catchm-api`: the CatchM Cloud endpoint.
  - `../chatgpt-proxy-go`: new; the ChatGPT-login proxy for personal use.

## Why

Summaries, decisions and action items currently need Claude Code
installed and signed in (`internal/agent`, claude-agent-sdk-go). The job
only reads the meeting and its transcript, then returns a fixed JSON
object. One LLM call with structured output does the same thing, with no
agent loop.

## Decisions (user, 2026-09-28)

- **Providers:**
  - `ollama`, the default: local, free, private.
  - `catchm-cloud`: Cloudflare Workers AI behind catchm-api. It uses a
    free shared pool, with a per-account daily quota. The transcript
    leaves the device **unencrypted**, so the user must give explicit
    consent once.
  - `openai`: any OpenAI-compatible base URL, model and key. This covers
    a user's own Workers AI key, OpenRouter, OpenCode Zen, and the local
    `chatgpt-proxy-go`.
- **Claude Code / agent CLI is removed.** `internal/agent` and
  `claude-agent-sdk-go` go away. The in-process MCP tools in
  `internal/tools` stay.
- **ChatGPT login lives in a separate personal-use repo,
  `chatgpt-proxy-go`, modelled on `perplexity-proxy-go`.** It exposes
  an OpenAI-compatible `/v1/chat/completions` on localhost, and CatchM
  reaches it through the `openai` provider. CatchM itself never ships
  ChatGPT OAuth.
- **Update (2026-09-28): CatchM doesn't surface ChatGPT or pplx.**
  - The Custom service copy and placeholders no longer mention a local
    proxy. They use OpenRouter as the example.
  - `chatgpt-proxy-go` stays a standalone personal tool.
  - Its Codex path works, but the Go plan's Codex quota is used up.
  - The web-chat backend was declined, because it would mean bypassing
    Sentinel and Turnstile.
  - An Anthropic-compatible provider for `pplx serve` was also dropped.

## Contracts (all workstreams code against these exact names)

### Extraction JSON

This is the existing `internal/agent` shape, and the output of every
provider:

```json
{ "summary": "string",
  "participants": ["string"],
  "decisions": [{ "text": "string", "scope": "string?" }],
  "action_items": [{ "text": "string", "owner": "string?", "due": "YYYY-MM-DD?" }] }
```

### catchm config (`internal/config`)

```go
type SummaryConfig struct {
    Provider     string             `yaml:"provider"`      // "ollama" (default) | "catchm-cloud" | "openai"
    OllamaModel  string             `yaml:"ollama_model"`  // default "qwen3.5:9b"; host = translate.host
    OpenAI       OpenAICompatConfig `yaml:"openai"`
    CloudConsent bool               `yaml:"cloud_consent"`
}
type OpenAICompatConfig struct {
    BaseURL string `yaml:"base_url"` // e.g. http://127.0.0.1:11435/v1
    Model   string `yaml:"model"`
    APIKey  string `yaml:"api_key"`  // secret: never returned by GET
}
// Config gets: Summary SummaryConfig `yaml:"summary"`
```

- **`GET /api/config`** adds `"summary": {provider, ollama_model,
  openai: {base_url, model}, cloud_consent}` and
  `"summary_openai_api_key_set": bool`.
- **`PUT /api/config`** accepts a partial `summary`, where
  `openai.api_key` is write-only. It uses exactly the same pattern as the
  Soniox key.

### catchm HTTP

- **`GET /api/summary/status`** always returns 200:
  ```json
  { "provider": "ollama",
    "ready": false,
    "reason": "plain, user-facing text, only when not ready",
    "ollama": { "reachable": true, "model": "qwen3.5:9b", "installed": false },
    "cloud":  { "logged_in": true, "consent": false, "remaining_today": 4, "limit": 5, "reset_at": "RFC3339" },
    "openai": { "configured": true } }
  ```
  - Only the block for the active provider is required. The others may
    be omitted.
  - `cloud.remaining_today`, `limit` and `reset_at` come from the
    catchm-api quota endpoint. They are omitted if that endpoint can't be
    reached.
- **`POST /api/translate/pull`** accepts `{"target":"summary"}` as well,
  which pulls `summary.ollama_model`. It uses the same NDJSON stream as
  live and batch.
- **`POST /api/meetings/{id}/refine`**:
  - It is unchanged for success.
  - On a summary failure it answers
    `{ "error": "<plain message>", "kind": "<kind>" }`.
  - `kind` is one of:
    - `summary_not_ready`: the provider isn't set up (Ollama down, model
      missing, no base URL, not logged in to the cloud).
    - `consent_required`: the provider is `catchm-cloud` and
      `cloud_consent` is false. Nothing was sent. Answers with 409.
    - `quota_exceeded`: the cloud daily quota is used up. Answers with
      429 and includes `reset_at`.
    - `summary_failed`: the model call or JSON parsing failed after a
      retry.
  - The existing transcription errors keep their current behaviour.

### catchm-api (Worker)

- **`POST /v1/summarize`** takes `Authorization: Bearer <session>`, the
  same auth as sync.
  - Request:
    `{ "title": "string", "transcript": "string", "language": "string?" }`
  - `200`: the extraction JSON plus `"remaining_today": n`.
  - `401`: not logged in.
  - `413`: the transcript is larger than `MAX_SUMMARY_CHARS`.
  - `429`: `{ "error": "quota_exceeded", "reset_at": "RFC3339" }`.
  - `503`: `{ "error": "ai_unavailable" }` when Workers AI fails or the
    shared pool is exhausted.
- **`GET /v1/summarize/quota`** returns
  `{ "remaining_today": n, "limit": n, "reset_at": "RFC3339" }`.
- **Vars:** `SUMMARY_MODEL` (default `@cf/qwen/qwen3-30b-a3b-fp8`) and
  `SUMMARY_DAILY_QUOTA` (default `"5"`). Parsing is fail-closed, like the
  existing limits.
- **The transcript is never stored or logged.**
- **As implemented** (catchm-api commit `fa3b984`, branch `feat/summarize`):
  - **Sizes:** `MAX_SUMMARY_CHARS = 60000`, because the model has a
    32k-token context. A 413 answer includes `max_chars`.
  - **Rate limit:** `SUMMARY_LIMITER` allows 10 requests a minute per
    account. When it trips, the 429 is `{error:"too many attempts"}` with
    no `reset_at`, so clients must check `error` before treating a 429 as
    a quota problem.
  - **Model failures:** invalid output after the retry answers 503
    `ai_unavailable`.
  - **Quota:** a unit is reserved before the call and refunded if every
    provider fails.
  - **Zen fallback allowlist:** `longcat-2.5-preview-free` (the default)
    and `space-bunny-free`.
- **Fallback to OpenCode Zen (decided 2026-09-28).** The Worker falls
  back to OpenCode Zen when:
  - Workers AI fails, or its free pool is used up;
  - Workers AI returns invalid JSON even after the retry.
  How it's configured:
  - It is skipped when the secret isn't set.
  - The Worker secret is `OPENCODE_ZEN_API_KEY`, set with
    `wrangler secret put`. It is not a `[vars]` entry. Deploys are manual
    today, so a GitHub Actions secret alone never reaches the Worker.
  - The model comes from the var `SUMMARY_FALLBACK_MODEL`. Only
    zero-retention free models are allowed (Space Bunny Free and
    LongCat 2.5 Preview Free, per the Zen docs).
  Response and quota:
  - A fallback call still counts as a single quota use.
  - The 200 response adds `"provider": "workers-ai" | "opencode-zen"`.
    The field is additive, so clients may ignore it.

### chatgpt-proxy-go

- **CLI `cgpt`:**
  - `login`: OAuth PKCE in the browser, with a device-code fallback.
  - `logout`, `status`.
  - `serve [--addr 127.0.0.1:11435] [--api-key]`.
  - `ask "…"`.
  - `mcp`: optional, like pplx.
- **`serve`** exposes OpenAI-compatible `GET /v1/models` and
  `POST /v1/chat/completions` (non-streaming and streaming). It honours
  `response_format` `json_schema` and `json_object`.

## Setup UX pass (decided 2026-09-28)

**Why.** A non-tech user only found out that Ollama was needed after
pressing "Create summary", and sometimes minutes later.

**Contract addition.** `GET /api/summary/status` adds a system block:
```json
"system": { "total_ram_gb": 15.6, "free_disk_gb": 120.4 }
```
- `total_ram_gb` is physical memory.
- `free_disk_gb` is free space on the volume that holds Ollama's
  models: `OLLAMA_MODELS` if set, else `~/.ollama/models`, else the home
  directory.
- Each field is omitted when it can't be read.
- The data comes from `golang.org/x/sys` or the OS APIs. Heavy
  dependencies such as gopsutil are not allowed.

**Workstreams:**
- **U1 (backend):**
  - Add the `system` block.
  - `handleRefine` runs the same readiness checks as the status endpoint
    **before** the batch pass: Ollama reachable, model installed, cloud
    logged in, quota > 0. It answers `summary_not_ready` (503) or
    `quota_exceeded` (429) at once. If the quota endpoint can't be
    reached, it proceeds.
- **U2 (frontend):**
  - The MeetingDetail CTA checks readiness first and offers two setup
    paths inline.
  - Settings → Summaries → "On this computer" becomes a 3-step checklist
    with auto re-check, plus RAM-based model recommendations and a total
    download size.
  - A "Get started" card on Meetings, built from the STT, translation
    and summary status endpoints.

## Workstreams (parallel)

- **S1, catchm backend:**
  - Files: `internal/config`, a new `internal/summarize`,
    `internal/server`, and removal of `internal/agent` and the go.mod
    dependency.
  - Also `web/src/lib/api.ts`, for the types and client functions of the
    contracts above.
- **S2, catchm frontend:**
  - A Settings "Summaries" section in `settings/SettingsSummaries.tsx`
    plus the `Settings.tsx` nav.
  - Consent dialog, and MeetingDetail refine-error mapping by `kind`.
  - Cloud sync copy caveat.
- **S3, catchm-api:** the `/v1/summarize` and `/v1/summarize/quota`
  endpoints and their tests.
- **S4, chatgpt-proxy-go:** new repo.

## Acceptance criteria

1. `go.mod` no longer requires `claude-agent-sdk-go`, and nothing imports
   `internal/agent`.
2. Refine works with each provider against an httptest stub: Ollama
   `/api/chat` with `format`, an OpenAI-compatible
   `/v1/chat/completions`, and catchm-api `/v1/summarize`.
3. Invalid JSON from the model is retried once, then answered as
   `summary_failed`.
4. Long transcripts that go over the model context are chunked: each
   chunk is summarized, then the results are merged.
5. `catchm-cloud` never sends anything before `cloud_consent` is true.
   The UI states that the transcript is sent unencrypted before asking.
6. Settings → Summaries shows each option with:
   - size (Ollama), or pool, quota and privacy (Cloud), or
     "advanced" (OpenAI-compatible)
   - its readiness
   - a download-model action for Ollama
7. `task test`, `task lint`, `bunx tsc -b`, `bun run build` and oxlint
   all pass. catchm-api `npm test` passes. chatgpt-proxy-go `go test ./...`
   and `go vet ./...` pass.

## Status (2026-09-28)

All workstreams (S1–S4, U1, U2) are implemented.

**catchm** passes:
- `task test`
- `go vet`
- `gofmt`
- `bunx tsc -b`
- `bun run build`
- oxlint: only the 3 old warnings in `ui/*`.

`task lint`'s gofmt step still prints `lstat internal/agent/...` until
that deletion is committed.

**catchm-api:** `fa3b984` on `feat/summarize`, 122/122 tests. Not
deployed.

**chatgpt-proxy-go:** `233d08a`, local only. `go test` and `go vet` pass.

Still to do before moving this plan to `completed/`:
- a browser smoke test on `catchm serve`
- a real ChatGPT login and backend call, following `MANUAL-VERIFY.md`
  in chatgpt-proxy-go
- catchm-api: `wrangler d1 migrations apply catchm-api --remote`, then
  `wrangler deploy`. The `OPENCODE_ZEN_API_KEY` secret is already set.
- the RAM and disk readers on real Windows and macOS. Cross-compiling
  isn't possible here because of the cgo audio and systray dependencies.

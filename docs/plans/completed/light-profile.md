# Plan: Light profile — Soniox async, no-Whisper summaries, import, provider presets

- Status: done — merged in #25 (2026-09-28).
- Created: 2026-09-28
- Branch: `feat/light-profile`
- Implementers: GLM coding agents (glm-5.3), three parallel workstreams

## Why

The recommended setup for non-tech users is Soniox for STT and
translation, plus summaries from a model on the user's own Cloudflare
account (or CatchM Cloud). That needs no local Whisper and no Ollama.
Today it breaks: "Create summary & action items" always re-transcribes
with local Whisper (`internal/server/server.go:63`,
`newBatchEngine` → `stt.NewWhisperEngine`).

Soniox's async API is $0.10/h with diarization, language ID, one-way and
two-way translation and context terms all included. Files can be up to
300 min, and uploads can be deleted immediately. That makes it a better
fit for batch passes than Whisper for Soniox users. It also opens two new
features:
- **Improve with Soniox:** after a Whisper meeting, add speakers and
  better accuracy.
- **Import recording:** turn an audio file into a meeting.

## Decisions

1. **The refine engine is chosen per request, and the default is
   `auto`.** `POST /api/meetings/{id}/refine` takes an optional body:
   `{"engine": "auto" | "soniox" | "whisper" | "none"}`. An empty body
   means `auto`, which picks the first that applies:
   1. The meeting's engine is `soniox` and a Soniox key is set →
      `soniox` (async).
   2. Whisper is reachable (`stt.WhisperReachable`) → `whisper`.
   3. A Soniox key is set → `soniox`.
   4. Otherwise → `none`.

   **`none`** skips re-transcription and summarizes the live transcript,
   which `summarize.LoadInput` already falls back to. It is not an error.

   Explicit values fail fast if unusable, with `kind` `summary_not_ready`
   and a plain reason (same error shape as the summary errors).
2. **The meeting records which engine re-transcribed it.** A new
   frontmatter field `refined_with: "soniox" | "whisper" | ""` (json
   `refined_with,omitempty`) is set after a successful re-transcription.
   It stays empty for `none`.
3. **Soniox async engine** (`internal/stt/engine_soniox_async.go`):
   - It implements `stt.BatchEngine`.
   - Flow: upload the file (`/v1/files`), create the transcription
     (`/v1/transcriptions`, model `stt-async-v5`), poll with backoff
     until done, fetch the transcript tokens, turn them into utterances,
     then **delete the transcription and the file**. The delete runs in
     a `defer`, so it also happens on error or ctx cancel.
   - Request settings:
     - diarization on
     - language identification on
     - translation one-way (`TranslateTo`) or two-way
       (`Lang` + `LanguageB` when `TwoWay`)
     - optional context terms
     - `client_reference_id = "catchm-<meetingID>"`
   - It also exposes `CleanupStale(ctx)`, which deletes leftover
     `catchm-` transcriptions and files older than 1h. The server calls
     it once at startup when a Soniox key is set, and failures are
     ignored.
   - Tokens → utterances reuses the realtime engine's grouping (speaker
     change, sentence end, pause). The speaker goes in `Speaker`, and the
     translation tokens go in `Translated`.
   - The audio is uploaded as-is: WAV, or the imported file's own format.
     No transcoding and no new Go dependencies.
4. **`stt.Options` gains `ContextTerms []string` and `Title string`.**
   Only the Soniox async engine uses them, feeding the meeting title and
   participant names to Soniox as context. Glossary terms from
   `entities.json` come later, after meeting-insights P2.
5. **Refine uses the meeting's full translation mode.** `batch.Refine`
   passes `TwoWay` and `LanguageB` too, not just `Lang` and
   `TranslateTo`, and finds the audio file by name (`audio.*`) instead
   of hard-coding `audio.wav`.
6. **Import recording.** New endpoint `POST /api/meetings/import`
   (multipart):
   - Fields: `file`, plus optional `title`, `lang`, `translate_to`,
     `two_way` and `engine` (`auto|soniox|whisper`).
   - It stores the file as `audio.<ext>`, creates an **ended** meeting
     (`started_at` = now, `ended_at` = now, `imported: true`), and runs
     the re-transcription synchronously with the same engine rules. It
     returns the meeting.
   - **Limits:** body up to 1 GiB. Allowed extensions: `wav mp3 m4a mp4
     ogg webm flac aac`.
   - **Whisper** accepts WAV only (the whisper.cpp server needs `--convert`
     for other formats). For a non-WAV file with Whisper, answer 400 with
     a plain reason suggesting Soniox.
   - **Transcription fails after the meeting was created:** keep the
     meeting (so the upload isn't lost), and answer the error with `kind`
     plus `meeting_id`.
   - It does not summarize. The user presses "Create summary" afterwards.
7. **Provider presets in Custom service (UI only, no backend change):**
   - **Cloudflare Workers AI (your account):**
     - The user enters an Account ID and an API token.
     - Base URL: `https://api.cloudflare.com/client/v4/accounts/{id}/ai/v1`.
     - Model: `@cf/qwen/qwen3-30b-a3b-fp8`.
     - Links to the Cloudflare dashboard token page.
   - **OpenRouter:** base URL `https://openrouter.ai/api/v1`, and the
     user enters a model.
   - **Other:** manual entry, as today.
   - The preset is derived from the base URL; there is no new config
     field.
8. **Privacy copy.** Every Soniox batch action says: "The audio is
   uploaded to Soniox, processed, and deleted right after." Improve with
   Soniox shows the estimated cost: duration × $0.10/h.

## Contracts (all workstreams code against these exact names)

**Go:**
- `stt.NewSonioxAsyncEngine(apiKey, baseURL string, opts ...SonioxAsyncOption) *SonioxAsyncEngine`
  - `TranscribeFile(ctx, audioPath, Options) ([]meeting.Utterance, error)`
  - `CleanupStale(ctx) error`
  - The REST base URL defaults to `https://api.soniox.com`. It is not the
    realtime WebSocket URL.
- `stt.Options` gains `ContextTerms []string` and `Title string`.
- `meeting.Meeting` gains `RefinedWith string` (`yaml:"refined_with,omitempty" json:"refined_with,omitempty"`)
  and `Imported bool` (`yaml:"imported,omitempty" json:"imported,omitempty"`).

**HTTP:**
- `POST /api/meetings/{id}/refine`, body `{engine?}`. It returns the
  meeting as today, and errors keep `{error, kind}`.
- `POST /api/meetings/import` (multipart). It returns the meeting.
  Errors are `{error, kind?, meeting_id?}`.
- `GET /api/summary/status` is unchanged.

**Web API (`web/src/lib/api.ts`, owned by G2):**
- `RefineEngine = "auto" | "soniox" | "whisper" | "none"`
- `refineMeeting(id, opts?: { engine?: RefineEngine })`
- `importRecording(file: File, opts: { title?, lang?, translate_to?, two_way?, engine? }, onUploadProgress?: (fraction: number) => void): Promise<Meeting>`
  - Uses XMLHttpRequest for upload progress.
  - Throws `RefineError`-style errors that carry `meeting_id`.
- `Meeting` gains `refined_with?: string` and `imported?: boolean`.

## Workstreams

- **G1 — Soniox async engine:**
  - Files: `internal/stt/engine_soniox_async.go` and its test, plus the
    `stt.Options` fields.
  - Tests use an httptest fake of the Soniox REST API. They cover upload,
    create, poll, fetch, the token grouping, translation, two-way, delete
    on success/error/cancel, and CleanupStale.
- **G2 — server and batch:**
  - Files: `internal/batch`, `internal/server` (refine engine selection,
    the body, `refined_with`, import, the startup cleanup),
    `internal/meeting` (the new fields, audio discovery) and
    `web/src/lib/api.ts`.
  - It builds against G1's constructor name only.
- **G3 — web UI:**
  - Files: `MeetingDetail.tsx` ("Improve with Soniox" plus its confirm
    dialog), a new `ImportRecordingDialog.tsx`, its entry point
    (`AppSidebar.tsx` and the `MeetingList.tsx` empty state), and
    `settings/SettingsSummaries.tsx` (the presets).
  - It builds against G2's `api.ts` names only.

## Acceptance criteria

1. **Refine without local Whisper:** with the Whisper server stopped and
   a Soniox key set, "Create summary" re-transcribes with Soniox async
   (`refined_with: soniox`) and then summarizes. With no key and no
   Whisper, it summarizes the live transcript (`engine none`) and still
   succeeds.
2. **Soniox cleanup:** Soniox files and transcriptions are deleted after
   success, after failure and on cancel (tested with the fake).
   `CleanupStale` removes leftovers.
3. **Two-way:** two-way meetings re-transcribe with two-way translation.
4. **Import:** importing an m4a with Soniox creates an ended meeting
   with a diarized transcript. Importing an mp3 with Whisper gives a
   plain 400.
5. **Improve with Soniox:** it appears on Whisper meetings when a Soniox
   key is set, shows the cost estimate and the privacy note, and
   re-runs refine with `engine: soniox`.
6. **Cloudflare preset:** it fills base URL and model from the account
   ID. The key is saved write-only as today.
7. **Checks:** `task test`, `task lint`, `bunx tsc -b`, `bun run build`
   and oxlint are clean. No new Go or web dependencies.

## Status (2026-09-28)

Implemented by three GLM agents (G1 engine, G2 server, G3 UI) and then
integrated and reviewed.

GLM notes:
- A single reply caps at 8192 output tokens. With thinking set to "high",
  the agents spent whole turns deliberating and never wrote a file.
- What worked: thinking "low", edits in chunks, and a tool call early in
  every turn.

Review fixes applied during integration:
- **Import engine probe:** it wasn't time-bounded, so a Whisper port
  that accepts but never answers (WSL port forwarding) hung the import.
  Regression test: `TestImportWhisperProbeIsBounded`.
- **Early rejects:** bad extension, Whisper with a non-WAV file, Soniox
  without a key, and a second file part now drain the upload before
  answering, so the client gets the reason instead of a reset.
- **Web client field order:** the client sends fields before the file,
  and the server checks an explicit engine at the file part header.
- **Import duration:** imported meetings end at their last transcribed
  line instead of lasting zero seconds, so the time range and cost
  estimates are right.
- **Unknown refine engine:** now 400, checked before the summary probes,
  instead of 503.
- **Copy:**
  - The Cloudflare preset no longer claims audio is sent; only the
    transcript is.
  - The import dialog no longer shows the Soniox privacy/cost line
    while "On this computer" is the active engine.
  - The import dialog no longer repeats the missing-key reason.
  - The stated limit is now 5 hours (Soniox caps files at 300 minutes).

Checks:
- `task test`, `task lint`, `bunx tsc -b`, `bun run build`: pass.
- oxlint: only the 3 old warnings in `ui/*`.
- Smoke test over HTTP and in the browser on `catchm serve`, with no
  Whisper, no Soniox key and no Ollama:
  - an auto import falls back to `none`
  - every early reject returns its plain reason
  - no temp files are left behind
  - the import dialog shows the correct copy
  - the Cloudflare preset fills in the base URL and model

Not verified: the Soniox async flow against the real API (it needs a
key). Two points aren't in the docs: the DELETE status codes and the
speaker id format. Both are handled.

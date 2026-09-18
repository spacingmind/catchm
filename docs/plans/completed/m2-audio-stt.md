# Plan: M2 — audio capture + realtime STT (default/free + Soniox paid)

- Status: complete
- Created: 2026-09-18

## Goal

Real meetings flow through the store: capture mic audio (system audio
via virtual devices works through the same input path), stream it to a
pluggable realtime STT engine, append utterances to the live transcript,
and keep the raw audio file for later batch refinement. Two engines
ship: whisper.cpp server (default, free, local) and Soniox (paid cloud,
diarization + translation).

## Scope

- `internal/stt`: streaming engine interface + two implementations.
  - `StreamEngine` interface: `Stream(ctx, opts) (chan<- PCM, <-chan
    Result, error)` where Result carries text, optional speaker,
    optional translation, and final/partial flag. `SupportsDiarization
    ()` / `SupportsTranslation()` capability methods.
  - `engine_whisper.go`: whisper.cpp server client (HTTP/WS, chunks PCM,
    maps partial/final segments to utterances).
  - `engine_soniox.go`: Soniox WebSocket client (API key from config;
    fills Speaker from diarization and Translated from its translation
    option).
- `internal/capture`: mic capture via malgo (miniaudio Go bindings),
  16kHz mono PCM; enumerate input devices (so a BlackHole/VB-Cable
  virtual device is selectable as system audio later without new code).
- Audio recording: while a meeting is active, raw audio appends to
  `<meeting>/audio.wav` (16-bit PCM WAV).
- `internal/meeting`: extend `Start` with engine/lang options recorded
  in frontmatter; wire a `Pipeline` (capture → engine → store.Append)
  with start/stop lifecycle bound to meeting start/end.
- `internal/server`: endpoints `GET /api/devices`, `POST /api/meetings`
  gains `{device, engine, lang, translate_to}`; transcripts now stream
  real data (existing cursor endpoint unchanged).
- Config: `~/.catchm/config.yaml` (or `$CATCHM_HOME/config.yaml`) with
  engine credentials (soniox api key), defaults (engine, lang).

Out of scope: batch STT refine (utterances.v2), translate layer
(TranslateGemma), Apple SpeechTranscriber sidecar, cloud sync, web UI.

## Acceptance criteria

1. `task build && task test && task lint` pass; whisper/Soniox clients
   have unit tests against fake HTTP/WS servers (no network in CI).
2. `catchm serve` + `POST /api/meetings {engine:"whisper"}` with a real
   mic starts appending utterances with incrementing seqs to
   utterances.jsonl and grows audio.wav (manual test; scripted via
   reading a fixture WAV file as if it were a device when no mic —
   capture layer must accept a file source for testability).
3. Soniox engine: unit tests with a mock WS server verify auth header,
   binary audio frames in, recognized utterances (with speaker) parsed
   and surfaced. Engine selection per meeting is recorded in frontmatter
   (`engine: soniox`).
4. Ending the meeting stops capture and closes the engine cleanly (no
   goroutine leaks — test with goroutine count check).
5. Utterances from engines flow through the same
   `read_live_transcript` cursor path as M1 fixtures (compat test).

## Test scenarios

- `TestWhisperEngineMapping` — fake whisper server returns partial then
  final; engine emits one final utterance.
- `TestSonioxSpeakerAndTranslation` — mock frames carry speaker token +
  translated text; Result fields populated.
- `TestPipelineLifecycle` — file-source capture → whisper fake →
  utterances.jsonl grows; stop; audio.wav valid WAV with N bytes.
- `TestEngineSelectionRecorded` — meeting started with engine soniox has
  `engine: soniox` in frontmatter.

## Decisions

- Engine interface is streaming-first; batch refinement will be a
  separate `BatchEngine` interface in M2.5 (see
  docs/research-stt-translate.md).
- malgo chosen for capture: pure-ish cgo bindings, cross-platform
  (macOS/Windows/Linux), device enumeration included.
- audio.wav written by the pipeline (not the engine) so every engine
  benefits.
- Whisper.cpp server is the default engine because it needs no key and
  no account; Soniox is opt-in paid.

### Implementation deviations / simplifications (recorded during build)

- **Whisper HTTP protocol is our own, not whisper.cpp's real wire
  format.** whisper.cpp's actual `/inference` endpoint has no
  partial/final concept — it just transcribes whatever audio it's given.
  We send each PCM chunk as its own WAV multipart upload and treat the
  JSON response as final unless it carries an explicit `"partial": true`
  field (our own extension). A real whisper.cpp server will thus always
  produce `Final: true` results; partial-hypothesis streaming would need
  either polling accumulated buffers or a different whisper.cpp build
  with real streaming support — out of scope here. Keep-alives are
  disabled on the HTTP client (`Transport.DisableKeepAlives`) so idle
  connections don't linger as goroutines between infrequent chunk
  uploads — this was required to make the goroutine-leak test pass.
- **Soniox wire protocol is a reasonable guess, not verified against the
  real API.** No live Soniox account/spec was available. We authenticate
  with an `Authorization: Bearer <api_key>` header (matches the spec
  text "Auth header"), send raw PCM as binary WebSocket frames, and
  expect the server to reply with JSON text messages shaped as
  `{"text", "speaker", "translation", "is_final"}`. This is the seam a
  real Soniox client would need to be adjusted at if the real message
  schema differs; the `StreamEngine` interface and `Result` type are
  unaffected either way.
- **`meeting.Store.Start` signature changed** from `(title, startedAt)`
  to `(title, startedAt, StartOptions)` rather than adding an overload or
  functional-options variant, since Go has neither and the repo doesn't
  use a functional-options pattern anywhere else. All ~9 existing M1
  call sites were updated to pass `StartOptions{}` (or `meeting.StartOptions{}`
  from other packages); behavior for callers that don't care about
  engine/lang is unchanged.
- **`internal/server.New` signature changed** to `New(surface, cfg)`
  (cfg may be `nil`). Only one production call site (`cmd/catchm/main.go`)
  and one test helper (`newTestServer`) needed updating.
- **Live capture/engine wiring at the HTTP layer is best-effort and
  silently degrades.** `POST /api/meetings` always creates the meeting
  (and records engine/lang/translate_to in frontmatter) regardless of
  whether a live pipeline can actually start. If `engine` is set but mic
  capture or the engine connection fails (no audio backend, whisper
  server not running, missing Soniox key, unreachable device), the
  meeting simply has no live capture — this was not treated as a request
  error. Rationale: many environments (CI, headless servers) have no
  audio hardware at all, and a hard failure here would make the API
  itself unreliable independent of whether the caller wants transcription
  right now. `GET /api/devices` degrades the same way, returning an empty
  list instead of a 500 when the audio backend can't be queried.
- **`Server` builds its capture source and STT engine through two small
  injectable factory funcs** (`newSource`, `newEngine`, both unexported
  fields defaulting to the real malgo mic and the real whisper/Soniox
  engines). This exists purely so HTTP-layer tests can substitute a
  `capture.FileSource` and a trivial in-memory fake engine instead of
  touching real audio hardware or a real STT service — matching the "no
  real network calls, no real audio hardware" test constraint at every
  layer, not just inside `internal/stt`/`internal/capture` themselves.
- **`internal/audio`** was added (not in the original scope list) as a
  small shared WAV encode/decode helper, used by: the whisper engine
  (per-chunk WAV upload), the pipeline (`audio.wav` streaming writer with
  header fixup on `Close`), and `internal/capture`'s `FileSource` (WAV
  fixture playback). This avoided writing the same 16-bit/16kHz/mono WAV
  header logic three times.
- **Utterance dedup/backpressure**: the pipeline only appends `Result`s
  where `Final == true` and `Text != ""`; partial results are surfaced on
  the engine's result channel (so a future live-partial UI could consume
  them) but never written to `utterances.jsonl`, keeping the transcript
  file consistent with M1's one-utterance-per-append model.
- **Mic capture drops chunks under backpressure** rather than blocking
  the miniaudio callback thread (`MicSource`'s data callback does a
  non-blocking channel send with a `default:` drop case). This trades
  perfect fidelity for never stalling the audio driver; acceptable for a
  live-transcription context where losing an occasional buffer during a
  slow consumer is far less harmful than glitching capture entirely.

## Progress

- [x] internal/stt interface + whisper engine
- [x] internal/stt soniox engine
- [x] internal/capture (malgo + file source)
- [x] audio.wav recording + pipeline lifecycle
- [x] server endpoints + config
- [x] tests + manual verification

## Validation

- `task build && task test && task lint` all pass (verified locally on
  Linux/amd64 with cgo enabled; malgo's bundled miniaudio compiled and
  linked without needing system ALSA dev headers).
- `go build ./... && go vet ./... && gofmt -l . && go test ./...` clean
  across every package, including the new ones
  (`internal/stt`, `internal/audio`, `internal/capture`,
  `internal/pipeline`, `internal/config`).
- All four named test scenarios implemented and passing:
  `TestWhisperEngineMapping` (internal/stt), `TestSonioxSpeakerAndTranslation`
  (internal/stt, mock `gorilla/websocket` server), `TestPipelineLifecycle`
  (internal/pipeline, includes the `runtime.NumGoroutine` leak check),
  `TestEngineSelectionRecorded` (internal/meeting, checks both the parsed
  `Meeting` and the raw `meeting.md` frontmatter text).
- Compat test added: `TestEngineUtterancesCompatWithReadLiveTranscript`
  (internal/pipeline) appends one M1-style fixture utterance directly via
  `Store.Append`, then runs a real pipeline (file-source capture + fake
  whisper HTTP server) against the same meeting, and reads both through
  `tools.Surface.ReadLiveTranscript` — proving cursor/seq semantics are
  unchanged when utterances originate from a streaming engine instead of
  a direct `Append` call.
- Server-layer coverage added on top of the four named scenarios:
  `TestStartMeetingWithEngineWiresPipeline` (fake engine + file-source
  capture wired through `POST /api/meetings`, transcript grows, `audio.wav`
  is written, pipeline is untracked after `/end`),
  `TestStartMeetingWithoutEngineHasNoPipeline` (M1-compatible plain start
  stays pipeline-free), `TestDevicesEndpoint` (200 with valid JSON
  regardless of host audio backend).
- Manual smoke test: built `bin/catchm`, ran `catchm serve` with a
  scratch `CATCHM_HOME`, and confirmed `GET /healthz`, `GET /api/devices`
  (returned this sandbox's one real capture device via malgo), and
  `POST /api/meetings` (engine-less) all work end-to-end over HTTP.
  Did not exercise a real whisper.cpp server or a live Soniox account —
  no such credentials/binaries are available in this environment; the
  fake-HTTP-server and mock-WebSocket-server tests are the substitute
  per the hermetic-test constraint.
- `go mod tidy` run after wiring in `gorilla/websocket` and
  `github.com/gen2brain/malgo`; both moved from `// indirect` to direct
  requires in `go.mod`/`go.sum`.

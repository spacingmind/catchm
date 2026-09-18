# Plan: M2 — audio capture + realtime STT (default/free + Soniox paid)

- Status: active
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

## Progress

- [ ] internal/stt interface + whisper engine
- [ ] internal/stt soniox engine
- [ ] internal/capture (malgo + file source)
- [ ] audio.wav recording + pipeline lifecycle
- [ ] server endpoints + config
- [ ] tests + manual verification

## Validation

(to be filled as criteria are confirmed)

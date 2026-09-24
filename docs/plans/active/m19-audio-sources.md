# Plan: M19 — Audio sources: microphone, system audio, both (mixed)

- Status: active
- Created: 2026-09-24
- Repo: `catchm`, **Go only** (`internal/capture`, `internal/pipeline`,
  `internal/server`, `internal/meeting` if needed). No `web/` changes:
  M20 is building the UI in parallel against the contract below.
- Branch/worktree: `m19-audio-sources` in `../catchm-m19` (never
  commit to `develop` directly; never push).

## Why

User test on real Windows (2026-09-24): recording only offers a single
input device. There's no way to capture the other meeting
participants (system audio), and no "both". Separately,
`Server.startPipeline` swallows every failure (engine construction,
opening the device, pipeline start). The meeting gets created, nothing
is captured, and the user is never told why.

## Reference: how my-translator does it (read in full, 2026-09-24)

`refs/my-translator/src-tauri/src/{audio/*, commands/audio.rs}`:
- **System (Windows)**: legacy WASAPI loopback on the default render
  endpoint (`eRender, eConsole`, `AUDCLNT_STREAMFLAGS_LOOPBACK`). f32
  only, first channel only, nearest-neighbour downsample to 16 kHz.
  **Buffers flagged `AUDCLNT_BUFFERFLAGS_SILENT` are dropped**, so
  nothing arrives while nothing is playing.
- **Mic**: cpal default input, mono mixdown, linear resample.
- **"both"**: *not mixed*. Both receivers are forwarded into one
  channel in arrival order, and chunks are interleaved into a single
  STT stream (batched every 200 ms). When both are active the stream
  carries ~2× real-time audio and speech gets chopped between sources.
  **Don't copy that.** The user chose a real sample-wise **mix into one
  stream** (decision 2026-09-24).
- UX worth copying: the source can be switched **mid-session** (status
  row select + ⌘1/2/3). It pauses, swaps the source, resumes, and
  shows a toast.

## Scope

### 1. System-audio source (`internal/capture`)

- `malgo` v0.11.26 exposes `malgo.Loopback` (miniaudio: WASAPI only).
  Add a `SystemSource` that opens the **default playback device** in
  loopback mode, asking miniaudio for `FormatS16`, 1 channel, 16 kHz
  (miniaudio does the conversion/resampling; don't hand-roll
  nearest-neighbour like the reference).
- Platform support: Windows = loopback. Linux = no loopback in
  miniaudio, but PulseAudio/PipeWire expose `*.monitor` *capture*
  devices. Decide whether "system" on Linux means "the default sink's
  monitor device" (if detectable via malgo's device list) or just
  "unsupported", and record it. macOS = **unsupported** (needs
  ScreenCaptureKit, out of scope). An unsupported platform must return
  a clear error, not a silent no-op.
- Expose capability: `capture.SystemAudioSupported() (bool, reason
  string)`.

### 2. Mixer: one continuous stream from 1–2 inputs

- `capture.Mixer` implementing `capture.Source`. It owns 0–2 inputs
  (mic, system) and emits **one** 16 kHz s16 mono stream **at real-time
  pace**: on a fixed tick (e.g. 20 ms = 320 samples), pull what each
  input has buffered, **pad missing samples with zeros** (WASAPI
  loopback delivers nothing during silence, and an input may be
  briefly behind), sum with **int16 saturation** (clamp, no
  wraparound), and emit.
- Per-input bounded jitter buffer (e.g. ~300–500 ms). If an input's
  clock runs fast (device clock drift), drop the oldest samples instead
  of growing unbounded or adding latency. Record the numbers you pick.
- Real-time pacing also keeps the STT stream alive through silence
  (the Soniox realtime session shouldn't starve when nobody on the
  system side is talking).
- The current `MicSource` callback drops chunks when its channel is
  full (`select default`). In the mixer path a drop means a timing gap.
  Make sure the device callback feeds the jitter buffer without
  blocking the audio thread *and* without silently desyncing.
- **Level metering**: per input, compute RMS and peak (dBFS or
  0–1) over the last tick window. Expose them so the server can serve
  them (section 4).
- Hermetic tests with fake inputs and an injected clock: sum +
  saturation at the int16 bounds; zero-padding on underflow; overflow
  drops oldest and bounds latency; one-input mode passes through
  unchanged; output cadence is real-time (N ticks → N×320 samples);
  levels for known signals (silence = 0/−inf, full-scale sine ≈
  expected RMS).

### 3. Mid-session source switching

- Switching `mic` ↔ `system` ↔ `both` (or the mic device) while
  recording must **not** end the meeting or restart the STT session.
  The mixer keeps its output channel and swaps inputs internally (open
  the new device first; only if that succeeds, close the old one; on
  failure, keep the old input and return the error).
- `audio.wav` keeps recording the mixed output continuously across a
  switch.
- Record the switch in the meeting (e.g. a note or a frontmatter
  field such as `audio_source`, whatever fits the existing
  meeting.md schema; record the choice) so the timeline is honest.

### 4. Server API (the contract M20 builds against, keep it exact)

- `GET /api/audio/sources` →
  ```json
  {"mic_devices":[{"ID":"…","Name":"…","IsDefault":true}],
   "system":{"supported":true,"reason":""}}
  ```
  (`mic_devices` has the same shape as today's `GET /api/devices`;
  keep `/api/devices` working.)
- `POST /api/meetings` gains `"source": "mic" | "system" | "both"`
  (default `"mic"` when omitted, so today's UI keeps working).
  `"device"` keeps meaning the mic device ID.
- **Start failures are no longer silent.** If the engine or the audio
  source can't start, the meeting is still created (existing
  behaviour), but the response includes
  `"capture_error": "<human-readable reason>"` (e.g. "Soniox API key
  is not set", "System audio isn't supported on macOS", "Couldn't open
  microphone: …"). `capture_error` is absent on success.
- `POST /api/meetings/{id}/source` `{"source": "...", "device": "..."}`
  → 200 `{"source": "...", "device": "..."}` on success; 404 if the
  meeting has no live pipeline; 400 on a bad source; 409/422 (your
  call) if the new device can't open (the old input stays active).
- `GET /api/meetings/{id}/levels` →
  `{"mic": {"rms": 0.12, "peak": 0.4}, "system": {"rms": 0, "peak": 0}}`
  (omit inputs that aren't active). Cheap enough to poll at ~5–10 Hz.
- `GET /api/meetings/{id}` includes the current `audio_source`.

## Out of scope

Web UI (M20/M21), acoustic echo cancellation (document "use
headphones; on laptop speakers the mic re-captures the other side"),
per-app loopback / excluding CatchM's own output, choosing a
non-default playback device for loopback, macOS system audio.

## Acceptance criteria

1. `go build ./... && go vet ./... && gofmt -l . && go test -race
   ./...` clean in the worktree (CI runs `-race`).
2. Mixer tests per section 2, switching tests per section 3 (with fake
   inputs), server tests per section 4, including `capture_error`
   for engine-missing-key and source-open-failure.
3. **Real Windows verification** (the reason this milestone exists):
   build `catchm-desktop.exe` on the Windows machine (toolchain from
   M5/M11: Go 1.27, WinLibs gcc, Wails CLI) or run `catchm serve`
   there, then:
   - system mode: play a known speech WAV on Windows (e.g. via
     PowerShell `System.Media.SoundPlayer`) and confirm levels show
     system signal and `audio.wav` contains it;
   - both mode: system playback + mic simultaneously, levels show
     both, `audio.wav` has both, mixed;
   - silence: system mode with nothing playing still produces a
     continuous real-time stream (audio.wav duration ≈ wall time);
   - mid-session switch mic → both → system without the meeting
     ending.
   STT transcription with Soniox needs the user's key. Ask before
   spending it. The level/audio.wav checks above don't need a key.

## Decisions

(fill in)

## Progress

- [ ] SystemSource (Windows loopback; Linux/macOS decision)
- [ ] Mixer (pacing, padding, saturation, jitter buffer, levels)
- [ ] Mid-session switching
- [ ] Server API + capture_error
- [ ] Real Windows verification

## Validation

(to be filled)

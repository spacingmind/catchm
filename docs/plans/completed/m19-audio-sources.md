# Plan: M19 — Audio sources: microphone, system audio, both (mixed)

- Status: complete
- Completed: 2026-09-25
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

- **Linux "system" audio: unsupported, by design.** miniaudio has no loopback device type outside Windows. Rather than guess which capture device is "the default sink's monitor" across PulseAudio/PipeWire/ALSA, "system" on Linux returns a clear reason string pointing at the existing escape hatch: M4's mic device list already enumerates capture devices, which includes `*.monitor` under PulseAudio — selecting one as the mic gives system audio today. macOS: unsupported (ScreenCaptureKit out of scope). `SystemAudioSupported()` is the single source of truth; `NewSystemSource` errors on unsupported platforms instead of no-op'ing.
- **Mixer numbers**: tick = 20 ms (320 samples @16 kHz, absolute-schedule pacing so output never runs fast); per-input jitter buffer bound = 400 ms (6400 samples) with drop-oldest on overflow; output channel = 25 ticks (~500 ms) to absorb a normal WebSocket/engine write hiccup before dropping; bounded warmup ≤100 ms at start so the first ticks carry content when content exists (a silent system-only source times out into padding mode, per spec). Padding (underflow) is excluded from level computation so a starved input meters 0.
- **Drop counters (no silent degradation)**: output-side drops under a slow consumer → `dropped_ticks` (atomic, mixer-global); jitter-buffer trims under clock drift → per-input `dropped_samples`. Both surface via `GET /api/meetings/{id}/levels`.
- **Input ownership protocol (final, post-review)**: the opener owns every source it opens. `Mixer.SetInputs` atomically installs the new pair and RETURNS the displaced sources — the swapper (server) closes them immediately after the swap; on any open failure the swapper never calls SetInputs and closes only what it just opened (old inputs untouched). `Mixer.Close` (reached via pipeline Stop) closes whatever is currently installed. All input sources' `Close` is idempotent (`sync.Once`), so a close race between swap and teardown cannot double-close. `SetInputs` on a closed mixer errors; the handler then closes the new sources, returns "meeting ended during switch", and writes no timeline note. Output channel + tick loop are never touched by a swap, so STT + audio.wav see one continuous stream; at most one tick's window straddles a swap. Proven by `TestOwnershipEverySourceClosedExactlyOnce` (start→switch→switch→end, every source closed exactly once) and `TestSwitchFailureKeepsOldInput422`.
- **One code path**: every mode (mic/system/both) runs through a Mixer — a one-input mixer for mic-only. `Pipeline.Mixer()` exposes it for switching/levels.
- **Switch failure status**: 422 (Unprocessable Entity) with the platform/open-failure reason when the NEW source can't open (old input stays live); 400 for a malformed source value; 404 when the meeting has no live pipeline. Chosen over 409 because 409 elsewhere on this endpoint family means "state conflict, retry differently" while here the fix is environmental (plug in / pick another device).
- **Timeline record**: `audio_source` frontmatter field on the meeting (set at start and on every switch) plus a timestamped note (`audio source switched to X at <RFC3339>`) appended to `meeting.Notes` — notes are the existing free-form timeline surface, visible in MeetingDetail today.
- **Start-failure surfacing**: `POST /api/meetings` response stays the Meeting object at top level (M20 contract) with `capture_error` added ONLY on failure; `audio_source` always set. Legacy `"source"` omitted → `"mic"` (old UIs unchanged). `/api/devices` kept as-is; `GET /api/audio/sources` is additive.
## Progress

- [x] SystemSource (Windows loopback; Linux/macOS decision)
- [x] Mixer (pacing, padding, saturation, jitter buffer, levels)
- [x] Mid-session switching
- [x] Server API + capture_error
- [x] Real Windows verification

## Validation

Hermetic: `go build/vet/gofmt` clean; `go test -race -count=3 ./...`
green (independent review + delta re-review: verdict **merge**).

**Real Windows 11** (2026-09-25; `catchm serve` built on the Windows
machine from this branch, isolated test `CATCHM_HOME`, no Soniox key
used, test data deleted afterward):

- **Bug found only on real hardware:** miniaudio's WASAPI loopback takes
  its client format from the *Capture* subconfig. With only Playback
  set, audio arrived at the endpoint's native 48 kHz and the mixer
  dropped ~2/3 of the samples. Fixed in `internal/capture/system.go`
  (set Capture format/channels too).
- `GET /api/audio/sources` → `system: {supported: true}`; mic device
  "Microphone (Realtek(R) Audio)" listed as default.
- **Silence pacing (system, nothing playing):** wall 38.12 s,
  `audio.wav` 38.1 s. The stream stays continuous through silence.
- **Mid-session switching:** mic → both → system → mic in one meeting.
  All three switches were recorded as timestamped notes, `audio.wav`
  58.9 s vs 58.99 s wall, no gap.
- **Audible system capture:** a ~13 s Windows-TTS speech WAV played
  through the speakers during a `source: "system"` meeting. Levels at
  0.5 s polls: silence rms 0.001 → speech rms 0.02–0.08 (peak up to
  0.37, with inter-phrase dips) → back to 0.002 after. **`dropped_chunks`,
  `dropped_samples`, `dropped_ticks` stayed 0 throughout.** `audio.wav`
  16.86 s vs 16.90 s wall; per-second RMS 0.004, 0.001, then
  0.021–0.055 for ~13 s, so the speech is in the file.
- **Device reopen / no leak:** a `source: "mic"` meeting started right
  after opened the mic fine; mic levels tracked the room noise floor
  (rms 0.003–0.006), 0 drops.
- Not done: STT transcription of captured system audio (needs the
  user's Soniox key; capture and levels above don't depend on it).

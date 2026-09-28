# Plan: M25 — macOS system audio via Core Audio process taps

- Status: planned
- Created: 2026-09-27
- Repo: `catchm`, Go + a small Objective-C shim in `internal/capture`,
  plus the `Info.plist` usage string for the desktop shell. No `web/`
  changes: `/api/audio/sources` already reports `system.supported`, and
  the UI already honours it (M19/M20).
- Branch/worktree: `m25-macos-system-audio` (never commit to `develop`
  directly).

## Why

`capture.SystemAudioSupported()` returns false on darwin
(`internal/capture/system.go:32`), so on a Mac CatchM can only hear the
local mic and misses the other participants in Zoom/Meet. malgo/miniaudio
has no loopback on macOS, which is why M19 left it out of scope.

## Decisions (user, 2026-09-27)

- **Minimum macOS 14.2** for system audio. Older macOS reports
  `supported: false` with a reason; mic capture still works there.
- **API: Core Audio process taps** (`CATapDescription`,
  `AudioHardwareCreateProcessTap`, private aggregate device, IOProc).
  We don't use ScreenCaptureKit: it needs the Screen Recording grant
  and a dummy video stream. Taps are audio-only and can exclude our own
  process.
- **No third-party capture library.** We write our own cgo +
  Objective-C shim against Apple frameworks only (CoreAudio,
  AudioToolbox, Foundation). The Go candidates surveyed
  (`tmc/apple`, `graphaelli/audiotap`, `LocalKinAI/sckit-go`) are too
  young or unproven. `progrium/darwinkit` hasn't been maintained since
  2025-03 and has no audio capture.
- Reference only, not a dependency: `insidegui/AudioCap` (BSD-2), for
  the tap → aggregate → IOProc → teardown sequence. Don't copy its
  private-TCC permission preflight.

## Design

### Files

- `internal/capture/system.go`: keeps the shared `SystemAudioSupported`
  doc. Per-OS behaviour moves into build-tagged files.
- `internal/capture/system_malgo.go` (`//go:build !darwin`): today's
  WASAPI-loopback `SystemSource`, unchanged.
- `internal/capture/system_darwin.go` (`//go:build darwin`, cgo): the
  Go `SystemSource` with the same exported surface (`NewSystemSource`,
  `Chunks`, `Close`, `DroppedChunks`), so `internal/server` and the
  Mixer don't change.
- `internal/capture/tap_darwin.m` + `tap_darwin.h`: the shim.
  `#cgo CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=14.2`,
  `#cgo LDFLAGS: -framework CoreAudio -framework AudioToolbox -framework Foundation`.

### Shim C ABI (small, opaque handle)

```c
typedef struct catchm_tap catchm_tap;
int  catchm_tap_supported(char *reason, int reason_len);   // runtime macOS >= 14.2
catchm_tap *catchm_tap_start(char *err, int err_len);
int  catchm_tap_read(catchm_tap *t, int16_t *out, int max_frames); // 16 kHz mono s16, non-blocking
uint64_t catchm_tap_overruns(catchm_tap *t);
void catchm_tap_stop(catchm_tap *t);                       // idempotent full teardown
```

### Capture path

1. Look up our own process object
   (`kAudioHardwarePropertyTranslatePIDToProcessObject` on `getpid()`).
   Create `[[CATapDescription alloc] initStereoGlobalTapButExcludeProcesses:@[self]]`
   with `muteBehavior = CATapUnmuted` and `privateTap = YES`, then
   `AudioHardwareCreateProcessTap`.
2. Create a **private** aggregate device (`kAudioAggregateDeviceIsPrivateKey`,
   `kAudioAggregateDeviceTapAutoStartKey`). Its main sub-device is the
   current default output device's UID, and its tap list holds the tap's
   UUID.
3. Read the tap's real format from `kAudioTapPropertyFormat`. Never
   hard-code it; it's usually float32 stereo at the device rate.
4. The IOProc (realtime thread) **only copies** raw frames into a
   preallocated lock-free SPSC ring (about 2 s). It doesn't allocate,
   lock, or call Go. On overflow it drops and bumps an overrun counter.
5. `catchm_tap_read` (called from a Go goroutine, not realtime) drains
   the ring through an **`AudioConverter`** (Apple's resampler + downmix)
   to 16 kHz mono s16. We don't hand-roll resampling (same rule as M19).
6. Go side: a goroutine calls `catchm_tap_read` every 20 ms and pushes
   `stt.PCM` chunks to `ch` with the same non-blocking send and `dropped`
   counter as the malgo sources. It exits and closes `ch` on `Close`.
7. **Default-output change** (headphones plugged in, etc.): a property
   listener on `kAudioHardwarePropertyDefaultOutputDevice` rebuilds the
   aggregate device and IOProc on the new device while keeping the same
   ring and converter. The Mixer sees at most a short gap, which it
   already zero-pads.
8. Teardown order in `stop`: `AudioDeviceStop` → `AudioDeviceDestroyIOProcID`
   → `AudioHardwareDestroyAggregateDevice` → `AudioHardwareDestroyProcessTap`
   → remove listener → dispose converter. It must be safe on partial
   start failure and on double call.

### Permission

- There is no public preflight API. The prompt appears when the
  aggregate device first starts. We don't call private TCC functions.
- Add `NSAudioCaptureUsageDescription` to the desktop shell's
  `Info.plist` (`cmd/catchm-desktop/build/darwin/Info.plist`).
- Denied permission shows up as **all-zero buffers**, not as an error.
  `SystemSource` tracks "N seconds of exact digital zeros since start".
  Past a threshold (e.g. 10 s) the server surfaces a non-fatal hint in
  the live status: "No system audio received. Check System Settings →
  Privacy & Security → Screen & System Audio Recording". Record the
  threshold chosen.
- Headless `catchm serve` from a terminal: the grant is attributed to
  the terminal app. Document this in the README; no code change.

## Out of scope (separate milestone)

- macOS `.app` packaging, Developer ID signing, notarization, and
  auto-update for macOS. A stable signature is what keeps the TCC grant
  across rebuilds. This milestone only adds the plist key.
- Per-app capture (choosing Zoom only). A global tap that excludes
  CatchM is enough for now.

## Acceptance criteria

1. On macOS ≥ 14.2, `GET /api/audio/sources` returns
   `system.supported: true`. On older macOS it returns `false` with a
   reason naming 14.2.
2. `source: "system"` and `"both"` capture audio playing from another
   app (e.g. a YouTube video in a browser) into `audio.wav` and the live
   transcript, at 16 kHz mono s16. This is verified by listening to
   `audio.wav`, not only by checking status codes.
3. CatchM's own playback isn't captured (own-process exclusion).
4. Switching the default output device mid-meeting doesn't end capture.
   The gap is ≤ 1 s and capture continues on the new device.
5. Ending the meeting or switching source away from system tears
   everything down. After a stop, no CatchM aggregate device or tap
   remains (checked with Audio MIDI Setup or `AudioHardwareGetProperty`
   device list), and other apps aren't muted.
6. With permission denied, the meeting still runs and the "no system
   audio received" hint appears.
7. Overruns/drops are counted and visible through the existing
   `DropCounter`/levels path. They're never silent.
8. `task test` and `task lint` still pass on Linux. Non-darwin
   behaviour is byte-for-byte unchanged (`system_malgo.go` is a move,
   not a rewrite).
9. The darwin build is verified. CI gains a `macos-latest` job that runs
   `go vet ./...` and `go test ./internal/capture/...` (build + the
   hermetic tests; real capture stays a manual check on a Mac).

## Validation

Real-device checks (criteria 2–6) need a Mac on 14.2+. They can't be
done from the Linux/WSL dev box. Record the machine, macOS version,
and results here when done.

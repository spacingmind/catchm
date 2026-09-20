# Brand & UX research notes (Perplexity, 2026-09-21)

Reference material for future icon/logo design and UX copy work — not
a spec, no milestone attached. Pulled from a deep-research pass on
meeting-assistant UX (Otter, Fireflies, Granola, Fathom, Krisp),
tray-icon conventions, and E2EE trust UX (Bitwarden, 1Password, Signal,
Standard Notes, Obsidian). Some of this has already been applied — see
"Applied so far" below.

## Product framing

> Capture locally. Understand immediately. Sync only by choice.

Position CatchM as a "quiet local utility," not a meeting bot —
closer to Granola (keeps the user's own notes central, not just a
transcript) + Otter (visible live transcript builds trust) + Fathom
(summary/decisions/actions lead, transcript is evidence below) +
Krisp (bot-free capture as a UX win, not just a privacy checkbox).

## Tray icon states (for a future tray-icon revision beyond M11)

| State | Treatment | Tooltip |
|---|---|---|
| Idle | Monochrome mark | "CatchM — Ready" |
| Recording | Accent color / pulse ring | "CatchM — Recording" |
| Transcribing | Accent ring + waveform | "CatchM — Transcribing" |
| Syncing | Small directional indicator | "CatchM — Syncing N meetings" |
| Permission issue | Amber (not red) | "CatchM — Microphone permission needed" |
| Error | Red/high-contrast badge | "CatchM — Capture stopped" |

Don't put meeting titles in the tooltip (screen-share privacy). Left
click opens a compact menu, not the full window. Keep the same outer
silhouette across all states; encode meaning via color/small badge, not
by changing the whole shape.

## Icon/logo directions

Avoid literal camera/eye/big-shield (reads as surveillance software).
Two strongest directions:

- **A — Catching waveform**: a rounded "C" (echoes CatchM) containing
  a short speech waveform. Friendly, no literal lock needed.
- **C — Sealed speech bubble**: a rounded speech bubble containing a
  waveform that closes into a small loop (capture → retention). Most
  scalable for favicon/tray use.

Suggested palette: ink `#17202A`, warm paper `#F6F5F1`, catch teal
`#12B8A6` (capture), translation violet `#7567F8`, secure blue
`#3478F6` (sync/security), amber `#E6A23C` (attention), coral
`#D95C5C` (error). Flat glyph for tray/in-product icons; a soft
gradient is fine for the desktop/web app icon only. Needs a real
designer pass to produce actual assets — this doc just sets direction.

## E2EE trust-copy guidelines

Say what the boundary actually is, don't reach for compliance-badge
language:

- Prefer: "Encrypted before it leaves this device", "CatchM cannot
  read synced meetings", "Recovery code rotates after use."
- Avoid: "military-grade encryption", "100% safe", "zero knowledge"
  (unless fully explained), a bare lock icon with no data-flow
  explanation.
- Recovery-code warning tone: calm but accurate — "Without your
  recovery code or a trusted device, encrypted synced meetings may not
  be recoverable" rather than "your data will be lost forever."

For a future M14 (device linking) UI: show device name + approximate
time/location + a human-readable confirmation phrase on the approving
device (Bitwarden's pattern), not a bare cryptographic fingerprint.

## Applied so far

- `web/src/components/SettingsCloud.tsx`: added a "stays on this
  device by default, sync is optional and encrypted" line above the
  signup/login form; added a rotation notice on the recovery form.
- `web/src/components/RecoveryCodeDialog.tsx`: reworded the warning to
  the calm-but-accurate phrasing above.

## Not yet applied

Actual icon/logo asset production (needs a designer), tray-icon state
revision beyond M11's current idle/recording tooltip, onboarding-flow
redesign (progressive disclosure: local-first start → permissions →
one test capture → optional sync → recovery setup), M14's
device-approval UI copy (no spec written yet).

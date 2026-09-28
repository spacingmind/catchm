# Plan: Non-tech UX pass

- Status: in progress
- Created: 2026-09-28
- Branch: `fix/non-tech-ux`

## Why

A review from a non-technical user's point of view (2026-09-28) found
three blockers and a lot of jargon:

1. Nothing tells the user about first-run setup. The default engine,
   Whisper, needs a Whisper server running at `127.0.0.1:8080`, and when
   it fails the live error says "Check your network".
2. Summary, decisions and action items only appear after
   "Refine transcript" (`handleRefine` → `enrichFinal`). Ending a meeting
   does not produce them, yet `ActionItems.tsx` claims it does. Refine
   also needs Claude Code, and the UI never says so.
3. Translation needs Ollama and downloaded models. The start popover
   never warns when Ollama isn't there, and Settings talks in Ollama
   jargon.

## Decisions

- **Shared labels.** `web/src/lib/labels.ts` is the single source for
  audio source and engine names:
  - Sources: Microphone / Computer audio / Both.
  - Engines: On this computer / Cloud (Soniox).
  - Languages: `languageLabel()` in `lib/languages.ts`.
- **Copy stays English and sentence case.** Whimsical copy ("burrow",
  "stash", "loose ends") is replaced with plain wording.
- **Blocker 1, decided 2026-09-28: new endpoint `GET /api/stt/status`.**
  Contract (workstreams C and F depend on these exact names):
  ```json
  {
    "default_engine": "whisper",
    "whisper": { "ready": false, "reason": "The Whisper server at http://127.0.0.1:8080 isn't responding." },
    "soniox":  { "ready": true }
  }
  ```
  - Whisper is ready when an HTTP request to the configured
    `whisper_url` gets any response within about 1.5s.
  - Soniox is ready when an API key is set. The server is not dialled.
  - `reason` is plain, user-facing text, present only when
    `ready` is false.
  - Client: `getSttStatus(): Promise<SttStatus>` in `lib/api.ts`, owned
    by F.
- **Blocker 2, decided 2026-09-28: stays manual.** Ending a meeting does
  not enrich. The detail page instead gives a prominent
  "Create summary & action items" (the renamed Refine button) with an
  explanation. There is no backend change.

## Workstreams (parallel, disjoint files)

- **A. Lists and detail:** `MeetingList.tsx`, `MeetingDetail.tsx`,
  `ActionItems.tsx`.
- **B. Live:** `LiveMeeting.tsx`, `live/StatusRow.tsx`,
  `live/CompactOverlay.tsx`.
- **C. Start popover:** `StartMeetingPopover.tsx`.
- **D. Settings:** `settings/SettingsTranscription.tsx`,
  `SettingsTranslation.tsx`, `SettingsRecording.tsx`,
  `SettingsSystem.tsx`, `SettingsShortcuts.tsx`, `pieces.tsx`.
- **E. Cloud:** `settings/SettingsCloudSync.tsx`,
  `RecoveryCodeDialog.tsx`.
- **F. Backend (if decided):** `internal/server`, `internal/agent`,
  `web/src/lib/api.ts`.

## Acceptance criteria

1. There are no user-visible uses of these words:
   - "utterance"
   - "burrow"
   - "stash"
   - "config" in user copy
   - "Pushed/Pulled"
   - "manifest"
   - "conflict-* files"
   - "rotates"
   - "Check that CatchM is running"
2. Source and engine names come from `lib/labels.ts` everywhere, and the
   meeting card doesn't show a raw engine id. Language codes are shown
   through `languageLabel`.
3. The Action items empty state and the meeting detail describe how
   summaries and action items are actually produced.
4. A Whisper error points to Settings → Transcription, not to the network.
5. The start popover warns before starting when translation is chosen and
   Ollama is unreachable or the live model isn't installed.
6. Translation settings explain what Ollama is, link to its install page,
   and use plain names for the models.
7. `cd web && bunx tsc -b && bun run build` pass, `bunx oxlint` reports no
   new warnings, and `task test` and `task lint` pass.

## Status (2026-09-28)

All six workstreams are implemented.

Passing:
- `bunx tsc -b`
- `bun run build`
- `bunx oxlint`: the only warnings are old ones in `ui/button`, `ui/toggle`
  and `ui/badge`.
- `task test`
- `task lint`

Still to do before this plan moves to `completed/`: a browser smoke test
against `catchm serve`. Check:
- the start popover's readiness warning, with Whisper stopped
- press-to-record shortcuts in the desktop shell
- the summary CTA card after End meeting
- how the live status row wraps on a narrow window

## Follow-ups (separate plans)

- **Whisper failures during a meeting aren't surfaced.** Whisper always
  reports `streaming` (see the comment in `lib/api.ts`), so the new
  engine-aware live error only ever shows for Soniox. The backend needs
  to report Whisper request failures in its engine state.
- **Summaries still need Claude Code.** The proposal is a pluggable
  summarizer: a single structured-output LLM call instead of an agent
  loop, Ollama by default, and an optional OpenAI-compatible adapter.
  Decided 2026-09-28 and tracked in `pluggable-summarizer.md`.
- **Recording a shortcut that is already registered doesn't work.**
  Windows fires the existing global hotkey instead of passing the keys to
  the field. Fixing this needs a backend pause on hotkeys while
  recording.

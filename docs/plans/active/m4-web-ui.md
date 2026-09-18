# Plan: M4 — web UI (Tailwind + shadcn/ui)

- Status: active
- Created: 2026-09-18

## Goal

A minimal, functional web UI over the existing REST API (`internal/server`),
served by the same Go binary (no separate frontend server in production).
Covers the full loop: start a meeting, watch it live, end it, refine it,
and set up translation — using shadcn/ui components on Tailwind, in the
brand's two-tone chocolate/cream palette. Follow the terminology rules
and empty-state copy in `docs/ui-brainstorm.md`'s "Golden rule for
terminology" and "Empty states" sections exactly: metaphor words
("Burrow", "stash") are flavor/copy only, never replacing standard
nouns ("Meetings", "Transcript", "Decision", "Action item") as IA labels
or button text.

## Stack & scaffold

- `web/` — bun-managed Vite + React + TypeScript app (`bun create vite`
  with the `react-ts` template), Tailwind CSS, shadcn/ui (`bunx
  shadcn@latest init`, style/theme defaults are fine — then override the
  generated CSS variables with the brand palette below). No router
  library: this app is small enough that a `useState`-driven view switch
  in `App.tsx` (`"list" | "live" | "detail" | "settings"` + a selected
  meeting id) is simpler than wiring React Router for three-ish screens
  — keep it basic, this is a decision already made for you, don't
  second-guess it.
- If `bunx shadcn@latest init`/`add` cannot reach its registry in this
  sandbox, fall back to hand-writing the same small set of component
  files shadcn/ui would generate (they're just Radix primitives +
  `class-variance-authority` + Tailwind classes you already know well)
  under `web/src/components/ui/`, installing the underlying deps
  directly (`@radix-ui/react-*` as needed, `class-variance-authority`,
  `clsx`, `tailwind-merge`, `lucide-react`). Record which path you took
  in the Decisions section.
- Brand palette (from `docs/brand.md`) mapped onto shadcn's CSS
  variables in `web/src/index.css`: `--background`/`--card`/`--popover`
  → `#FBF7EA` (surface), `--foreground`/`--card-foreground` → `#6C3524`
  (on-surface), `--primary` → `#6C3524`, `--primary-foreground` →
  `#FBF7EA`, `--secondary`/`--muted` → a lighter tint derived from
  `#A9826D`. Keep default shadcn neutrals for destructive/border/ring;
  don't invent new brand colors.
- `web/src/lib/api.ts`: one typed fetch client covering every endpoint
  in `internal/server/server.go` — list this file's exports against
  that file's `routes()` method 1:1 so nothing is missed: `getDevices`,
  `listMeetings(q?)`, `startMeeting(body)`, `getMeeting(id)`,
  `getTranscript(id, cursor)`, `addNote(id, note)`, `endMeeting(id)`,
  `refineMeeting(id)`, `getTranslateStatus()`, `pullTranslateModel(target,
  onProgress)` (reads the NDJSON stream via `fetch` + `ReadableStream`,
  calling `onProgress` per parsed line), `testTranslate(body)`. Mirror
  the exact JSON field names each Go handler in `internal/server`
  actually sends/expects (read every handler, don't guess).

## Screens

1. **Meeting list** (`MeetingList.tsx`) — cards from `listMeetings()`:
   title, started/ended time, engine, decision/action-item counts. A
   "Record a meeting" button opens `NewMeetingDialog`. Empty state uses
   the exact copy from `docs/ui-brainstorm.md`: "Your burrow is quiet." /
   "Start a meeting to capture the conversation, decisions, and
   follow-ups in one place." Clicking a card opens `LiveMeeting` (if
   `ended_at` is null) or `MeetingDetail` (if ended).
2. **New meeting dialog** (`NewMeetingDialog.tsx`, shadcn `Dialog`) —
   form: title (`Input`, required), device (`Select`, populated from
   `getDevices()`), engine (`Select`: whisper/soniox), source language
   (`Input`, optional), translate to (`Input`, optional). Submits
   `startMeeting`, then switches to `LiveMeeting` for the new id.
3. **Live meeting** (`LiveMeeting.tsx`) — polls `getTranscript(id,
   cursor)` on an interval (~1.5s, `setInterval` + cleanup on unmount),
   appending new utterances to a `ScrollArea` that auto-scrolls to the
   bottom on new content (a "your position" affordance if the user has
   scrolled up is a nice-to-have, not required). Each utterance shows
   speaker (if present), text, and translated text as a secondary muted
   line (if present). A note `Textarea` + "Add note" `Button` calling
   `addNote`. An "End meeting" `Button` calling `endMeeting` then
   switching to `MeetingDetail`.
4. **Meeting detail** (`MeetingDetail.tsx`) — for an ended meeting:
   summary, participants, decisions, action items (each as a `Card` or
   list item; empty sections use "No loose ends." for action items per
   the brainstorm doc), notes, a `Badge` showing "Refined" when
   `meeting.refined` is true. A "Refine transcript" `Button` calling
   `refineMeeting(id)` with a loading state (disable button, show a
   spinner) since it's a synchronous multi-second call; refresh the
   meeting doc on success.
5. **Settings — Translation** (`SettingsTranslate.tsx`) — calls
   `getTranslateStatus()` on mount; shows an Ollama-reachable `Badge`
   (green/amber) and, for each of live/batch model, name + an
   installed/not-installed `Badge`. For a missing model, a "Pull" button
   triggers `pullTranslateModel`, showing a shadcn `Progress` bar parsed
   from the streamed NDJSON `status` strings (e.g. extract a percentage
   with a regex like `/(\d+)%/`; if a line has no percentage, just
   update a status text label instead of the bar). A small test panel:
   text input + target-language input + "Test translate" button calling
   `testTranslate`, showing the result inline.

## Go embedding

- New `internal/webui` package (or a `webui.go` file directly in
  `internal/server` if that reads cleaner once you see the existing
  file) with `//go:embed all:dist` over `web/dist`, exposing an
  `http.FileSystem` (or `http.Handler`) for the built assets.
- `Server.routes()` (or `ServeHTTP`) serves the embedded UI for any
  request that doesn't match an `/api/*` or `/healthz` route, with SPA
  fallback (unknown paths under `/` serve `index.html`, so client-side
  view state surviving a hard refresh isn't a requirement — you don't
  need real routes/URLs for this, per the "no router" decision above,
  but a hard refresh must still load the app shell rather than 404).
- `web/dist` doesn't exist in a fresh checkout. Since `//go:embed`
  fails to compile against a missing directory, commit a minimal
  placeholder `web/dist/index.html` (e.g. "Run `task web:build`
  first.") so `go build`/`go test ./...` work out of the box before
  anyone has built the frontend; the real `task build` always rebuilds
  `web/dist` first, overwriting the placeholder. `web/node_modules` and
  the rest of `web/dist`'s real build output are gitignored (add a
  `web/.gitignore`), but the placeholder `index.html` itself is
  committed so embed always has something to point at.

## Taskfile

Add to `Taskfile.yml`: `web:install` (`bun install --cwd web` or
`cd web && bun install`), `web:build` (`bun run build` in `web/`,
producing `web/dist`), `web:dev` (`bun run dev` in `web/`, with the
Vite dev server proxying `/api` and `/healthz` to `http://127.0.0.1:4649`
in `vite.config.ts` so `bun run dev` works standalone against a running
`task run`). Make the existing `build` task depend on `web:build` first
(Taskfile `deps:` or just chain `cmds:`), so `task build` always embeds
a freshly built UI.

Out of scope: authentication/multi-user, React Router / deep-linkable
URLs, dark mode, the mascot/animation flourishes described in the
brainstorm doc (icons + copy + palette only, no illustration/motion
work), any change to the Go API's behavior or shapes (this milestone
only consumes the existing API, it doesn't add or change endpoints).

## Acceptance criteria

1. `task web:install && task web:build` succeeds, producing `web/dist`
   with a working `index.html` + bundled JS/CSS.
2. `bunx tsc --noEmit` (or equivalent, whatever the Vite react-ts
   template wires as a type-check script) passes with no errors.
3. `task build && task test && task lint` still pass with the real
   `web/dist` built (the Go side must compile and all existing Go tests
   must still pass — this milestone must not touch `internal/server`'s
   API handlers, only add the static-serving layer alongside them).
4. A new Go test (e.g. `internal/server/webui_test.go`) proves: a
   request to `/` returns 200 and contains `<html`/`<!DOCTYPE` (whatever
   the built `index.html`'s content looks like — or the placeholder if
   run before `task web:build`, either is acceptable for this test,
   it's just proving the embed+serve wiring works, not the built asset
   content), a request to an arbitrary unknown path also returns the
   app shell (SPA fallback) rather than 404, and existing `/api/*`
   routes are completely unaffected (reuse or extend
   `TestMeetingLifecycleOverHTTP` etc. to confirm no route conflicts).
5. Manual verification (to be done by whoever picks this up next, not
   necessarily in this run, since it requires a browser): `task run` in
   one terminal, `task web:dev` in another, walk through: create a
   meeting with the `whisper` engine and a file-based/no-mic
   environment (the pipeline may fail silently to start without real
   audio hardware — that's fine and expected per M2's "best effort"
   design; the UI must still create the meeting and not crash), view
   the (possibly empty) live transcript, end the meeting, view the
   detail screen, hit refine (requires a real `audio.wav`, which won't
   exist without a real capture session — refine failing gracefully
   with a visible error state, not a crash, is what to check for), and
   check the translate settings screen renders sensibly whether or not
   Ollama is actually running locally.

## Decisions

(fill in as the implementer makes calls — expected topics: shadcn CLI
vs hand-written components, exact CSS variable values chosen for
secondary/muted, polling interval and cleanup approach, how pull
progress percentage parsing handles lines with no `%`, SPA-fallback
implementation detail.)

## Progress

- [ ] web/ scaffold (Vite + React + TS + Tailwind + shadcn/ui)
- [ ] lib/api.ts typed client for all endpoints
- [ ] MeetingList + NewMeetingDialog
- [ ] LiveMeeting (polling transcript, notes, end)
- [ ] MeetingDetail (summary/decisions/actions, refine)
- [ ] SettingsTranslate (status, pull with progress, test)
- [ ] Go embed + SPA-fallback serving + placeholder dist
- [ ] Taskfile web:install/web:build/web:dev + build dependency
- [ ] tests

## Validation

(to be filled as criteria are confirmed)

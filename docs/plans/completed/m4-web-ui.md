# Plan: M4 — web UI (Tailwind + shadcn/ui)

- Status: complete
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

- **shadcn CLI vs hand-written fallback**: the sandbox had outbound
  internet access (verified with a direct `curl` to `ui.shadcn.com`
  before scaffolding), so `bunx shadcn@latest init` / `add` was used —
  the hand-written fallback was not needed. Note the current `shadcn`
  CLI (v4.21, "radix-nova" preset family) is a newer generation than the
  classic tutorials describe: it generates components against a combined
  `radix-ui` package plus a `cn` npm package (tiny `clsx` +
  `tailwind-merge` helper) rather than per-primitive `@radix-ui/react-*`
  packages and a hand-rolled `src/lib/utils.ts`. This is still genuine,
  current shadcn/ui output on Tailwind v4 and satisfies the spec's
  "shadcn/ui components on Tailwind" requirement; it was kept as
  generated rather than rewritten to the older style. Ran `init -t vite
  -b radix -p nova -y` (non-interactive flags — the CLI's plain
  `--defaults` assumes a Next.js template, so template/base/preset were
  pinned explicitly), then `add dialog input select textarea card badge
  progress scroll-area label` (`button` came from `init`).
- **CSS variable values**: `--background`/`--card`/`--popover` →
  `#FBF7EA`, `--foreground`/`--card-foreground`/`--popover-foreground` →
  `#6C3524`, `--primary` → `#6C3524`, `--primary-foreground` → `#FBF7EA`
  (all pinned by the spec). `--secondary`/`--muted`/`--accent` →
  `#D9C4B4`, a lighter tint of the brand's secondary muted brown
  (`#A9826D`) blended roughly 55–60% toward the cream surface — spec
  said "a lighter tint derived from `#A9826D`" without pinning an exact
  ratio, so this implementer picked one by eye for contrast against both
  `#FBF7EA` and `#6C3524` text. `--muted-foreground` → `#A9826D` itself
  (used verbatim as the "secondary muted brown" text color the brainstorm
  doc calls out). `--accent` wasn't named in the spec's CSS variable list;
  mapped it to the same tint as secondary/muted for palette consistency
  rather than leaving it at shadcn's neutral gray. `--destructive`/
  `--border`/`--input`/`--ring` left at shadcn's generated neutrals,
  per spec. Dropped the generated `.dark` class and the unused
  `--sidebar-*`/`--chart-*` tokens entirely from `index.css` (no dark
  mode, no sidebar/chart components in this milestone) rather than
  leaving dead CSS.
- **Go embed location**: `//go:embed` patterns cannot use `..` to escape
  their own source file's directory, so a package under `internal/`
  cannot directly embed `web/dist` (and embedding across a symlink isn't
  reliably supported by `go:embed`). The embed directive therefore lives
  in a new one-file package `web/embed.go` (`package web`, import path
  `github.com/spacingmind/catchm/web`), which embeds its own `dist`
  subdirectory. `internal/server/webui.go` imports that package and
  does the actual serving — this matches the spec's "or a webui.go file
  directly in internal/server" alternative for the serving logic, while
  the unavoidable embed-directive placement is the one deviation from
  the spec's literal suggestion (`internal/webui` with `//go:embed
  all:dist` over `web/dist` isn't achievable as literally described).
- **SPA fallback implementation**: `webUIHandler()` wraps
  `http.FileServer(http.FS(sub))`; before delegating, it does an
  `fs.Stat` on the cleaned request path against the embedded FS, and if
  that fails (path doesn't exist as a real built asset), it clones the
  request and rewrites its URL path to `/` before calling the file
  server, so unknown paths always resolve to `index.html`. Registered as
  `s.mux.Handle("/", webUIHandler())`, the last line of `routes()` for
  readability — Go's `net/http.ServeMux` (1.22+) always prefers the most
  specific registered pattern regardless of registration order, so this
  catch-all can never shadow `/healthz` or any `/api/*` route (verified
  by `TestWebUIDoesNotShadowAPIRoutes`).
- **`web/dist` gitignore shape**: `web/.gitignore` ignores `dist/*` with
  a `!dist/index.html` exception, so the committed placeholder tracks
  normally while every real build artifact (JS/CSS bundles, `assets/`,
  and the real `index.html` once built) stays untracked. Running
  `task web:build` overwrites the tracked `dist/index.html` on disk with
  the real build output; that's an expected, normal "modified but
  unstaged" state after a local build and was not committed — only the
  placeholder content is checked in, matching the spec's "web/dist
  doesn't exist in a fresh checkout" framing.
- **`api.ts` scope**: added `getHealthz()` in addition to the spec's
  named export list, since `GET /healthz` is also registered in
  `routes()` and the deliverable asks for every endpoint covered 1:1;
  it's otherwise unused by any screen. Also: `capture.Device` has no
  `json` tags in Go, so its wire JSON keys are the exported field names
  verbatim — `ID`, `Name`, `IsDefault` (capitalized) — the TS `Device`
  type mirrors that exactly rather than the lowerCamelCase you'd expect
  from most of this API; called out with a comment in `api.ts` so it
  isn't "fixed" by a future editor. `getDevices`/`listMeetings`/
  `getTranscript` normalize a possible JSON `null` (a nil Go slice
  serializes as `null`, not `[]`) to `[]` inside the client so screens
  never need a null-check.
- **Polling interval and cleanup**: `LiveMeeting` polls
  `getTranscript` every 1.5s via `setInterval` inside a `useEffect` keyed
  on `meeting.id`; the cursor is held in a `useRef` (not state) to avoid
  a render per poll tick, and the interval is cleared plus a `cancelled`
  flag set on unmount/id-change to drop any in-flight response that
  resolves after teardown.
- **Pull progress percentage parsing**: `SettingsTranslate` matches each
  NDJSON `status` string against `/(\d+)%/`. When a line matches, the
  `Progress` bar's value updates and is shown; when a line has no `%`
  (e.g. "verifying sha256 digest"), the bar keeps its last known percent
  (or stays hidden if none has been seen yet) and only the status text
  label below it updates — per the spec's "if a line has no percentage,
  just update a status text label instead of the bar".
- **View wiring / IA**: no router, per spec. `App.tsx` holds `view:
  "list" | "live" | "detail" | "settings"` plus the selected `Meeting`.
  A persistent header (logo → back to list, a "Settings" button) is the
  only chrome outside each screen's own content — `MeetingList` itself
  doesn't duplicate a settings entry point.
- **New-meeting engine field**: the dialog always submits a concrete
  `engine` (defaulting to `whisper`, per the `Select`'s default value)
  rather than leaving it blank, since `handleStartMeeting` only attempts
  to start a live capture pipeline when `engine` is non-empty — this
  means every meeting created through the UI gets a best-effort pipeline
  attempt, matching the M2 "best effort" design referenced in acceptance
  criterion 5.

## Progress

- [x] web/ scaffold (Vite + React + TS + Tailwind + shadcn/ui)
- [x] lib/api.ts typed client for all endpoints
- [x] MeetingList + NewMeetingDialog
- [x] LiveMeeting (polling transcript, notes, end)
- [x] MeetingDetail (summary/decisions/actions, refine)
- [x] SettingsTranslate (status, pull with progress, test)
- [x] Go embed + SPA-fallback serving + placeholder dist
- [x] Taskfile web:install/web:build/web:dev + build dependency
- [x] tests

## Validation

Verified in this run (see commits on `develop` for the exact diffs):

- `task web:install && task web:build` succeeds and produces a real
  `web/dist/index.html` + hashed JS/CSS bundles under `web/dist/assets/`
  (criterion 1).
- `bun run build` runs `tsc -b && vite build`; `tsc -b` performs a full
  project type-check (the `react-ts` template's `tsconfig.app.json` has
  `"noEmit": true`, so this is the equivalent of `tsc --noEmit`) and
  passed with zero errors, confirmed both as part of `bun run build` and
  standalone via `bunx tsc --noEmit -p tsconfig.app.json` (criterion 2).
  `bun run lint` (oxlint) exits 0; it prints a handful of pre-existing
  `react/only-export-components` and `react/set-state-in-effect`
  *warnings* (not errors) on shadcn-generated files and on the
  fetch-in-`useEffect` data-loading pattern used by every screen — these
  are oxlint being conservative about a legitimate, common pattern and
  aren't part of any acceptance criterion, so left as-is.
- With the real built `web/dist` present: `go build ./...`,
  `go vet ./...`, `gofmt -l .` (empty output), and `go test ./... -count=1`
  all pass — every pre-existing package still reports `ok` (including
  `internal/server`'s full existing suite, e.g.
  `TestMeetingLifecycleOverHTTP`), and the new
  `github.com/spacingmind/catchm/web` package (holding only the
  `//go:embed`) reports `[no test files]`, i.e. it compiles cleanly with
  either the placeholder or a real `web/dist` on disk (criterion 3, plus
  the "must not touch existing API handler behavior" hard constraint —
  `internal/server/server.go`'s only change is one appended
  `s.mux.Handle("/", webUIHandler())` line in `routes()`).
- `internal/server/webui_test.go` (`TestWebUIServesAppShellAtRoot`,
  `TestWebUISPAFallbackForUnknownPaths`,
  `TestWebUIDoesNotShadowAPIRoutes`) passes: `/` returns 200 with
  HTML-shell markup, three different unknown/nested paths all return 200
  with the same app-shell markup instead of 404, and `/healthz` +
  `/api/meetings` (existing, 200) + `/api/meetings/does-not-exist`
  (existing, 404) all keep their exact prior JSON behavior — proving the
  catch-all doesn't shadow the API either for success or error responses
  (criterion 4).
- `task build`, `task test`, and `task lint` were each run directly (not
  just their underlying commands) with the real `web/dist` on disk, and
  all three passed; `task build` was confirmed to invoke `web:build`
  first (its dependency) before compiling the Go binary.
- **Not done in this run — left for whoever picks this up next**:
  acceptance criterion 5, the manual browser walkthrough (`task run` +
  `task web:dev`, click through record → live → end → detail → refine →
  translate settings). This requires an actual browser and, per the
  spec, wasn't expected to be completed here. Everything it depends on
  (a working `task run` server, a working `task web:dev` proxy target of
  `127.0.0.1:4649`, and every screen's error states for the "no
  hardware"/"no Ollama"/"no audio.wav yet" cases described in criterion
  5) is implemented and unit-tested at the HTTP-contract level, but the
  actual click-through in a browser has not been performed.

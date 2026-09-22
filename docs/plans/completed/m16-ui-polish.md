# Plan: M16 — UI polish pass

- Status: complete
- Created: 2026-09-22
- Repo: `catchm`, `web/` only (no Go changes expected).

## Goal

CatchM's web UI was built incrementally, milestone by milestone
(M4/M4.1/M4.2 web UI, M6 config UI, M7 meeting management, M8 cross-
meeting actions, M9/M10 translation, M13d cloud sync), each shipping
its own screen without a dedicated cross-screen design pass. This
milestone is that pass: consistent spacing/typography, real empty/
loading states instead of placeholders, a properly-behaved auto-scroll
in the live transcript, and cleanup of the usual "unpolished MVP"
tells (focus rings, button variant drift, text overflow, error-message
styling). Based on a Perplexity research pass on shadcn/Tailwind UI
polish conventions — read this spec's Decisions section once filled
for the actual checklist items pulled from that research; the research
itself isn't re-quoted here, ask if you need the full text.

**No dark mode work** — grep confirms the app is light-only today
(`web/src/index.css` explicitly notes "No dark" mode support), so skip
that entirely; don't add dark-mode variants as part of this pass.

## Scope

### 1. Typography & spacing audit (all screens)

Sweep `web/src/components/*.tsx` (MeetingList, LiveMeeting,
MeetingDetail, ActionItems, NewMeetingDialog, SettingsConfig,
SettingsTranslate, SettingsCloud, RecoveryCodeDialog) for:
- Arbitrary spacing (`mt-[13px]`-style) or one-off `gap`/`p`/`m` values
  that don't fit a `2/3/4/6/8` (× 4px) rhythm — normalize to the
  nearest standard step.
- Heading/body/label text-size drift — settle on: `text-xs` (metadata/
  timestamps), `text-sm` (labels/helper text), `text-base` (body/
  transcript text), `text-lg` (card titles), `text-xl`/`text-2xl`
  (page headings), and apply consistently.
- Raw Tailwind color classes (`bg-white`, `text-gray-900`,
  `border-gray-200`) instead of shadcn theme tokens (`bg-background`,
  `text-foreground`, `text-muted-foreground`, `border-border`) — this
  matters even without dark mode, since it's what any future theming
  work would need anyway and it's the existing convention other
  components already follow (check `SettingsCloud.tsx` for the
  established pattern before changing anything).

Don't do a mechanical find-replace — read each screen, decide the
right token, and note anything genuinely ambiguous in Decisions rather
than guessing.

### 2. Empty & loading states

Currently probably placeholder-y or missing entirely — check each of:
- `MeetingList.tsx`: no meetings yet (first run) vs. no search results
  (filtered empty) should read differently — different copy, not just
  "no meetings" both times. Loading: skeleton rows matching the real
  row shape (shadcn's `Skeleton` component — check if it's already in
  `web/src/components/ui/`, add it via the project's existing shadcn
  component-add convention if not), not a bare spinner or blank screen.
- `ActionItems.tsx`: no action items across any meeting — an
  encouraging first-run message + explain where action items come from
  (agent-enriched at meeting end), not just "empty."
- `MeetingDetail.tsx`: a meeting with no notes yet, no action items yet
  — same idea, small and low-key (this one's more common/expected than
  MeetingList's empty state, so keep it low-key, not a big empty-state
  graphic every time).
- Loading states should render skeletons shaped like the real content
  (not a generic spinner) wherever a screen fetches data before first
  paint — check `getConfig`/`listMeetings`/`getTranscript`/etc. call
  sites in each component for where this applies.

### 3. Live transcript auto-scroll (LiveMeeting.tsx)

Read the current implementation first: `bottomRef.current?.scrollIntoView(...)`
runs unconditionally on every update — meaning if a user scrolls up to
re-read something mid-meeting, the view yanks back to the bottom on
the next utterance. Fix:
- Track whether the user is currently scrolled to (near) the bottom
  (an `IntersectionObserver` on a sentinel element at the end of the
  list is the clean way, per the research — check if a simpler scroll-
  position threshold check is more consistent with this codebase's
  existing patterns instead, your call, record which and why).
- Only auto-scroll on new content when already at the bottom.
- When not at the bottom and new content arrives, show a small "New
  lines ↓" affordance (a `Button` or similar, bottom-of-view, low-key)
  that scrolls to bottom and dismisses itself on click.
- Don't regress the existing behavior for the common case (user
  watching live, stays scrolled to bottom, transcript keeps
  auto-scrolling) — write a test or at least a clear manual-verification
  note proving both the common case and the scrolled-up case work.

### 4. Common polish-pass fixes

- **Focus rings**: audit for any custom/interactive element missing
  `focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2`
  (shadcn's `Button`/`Input` primitives already have this — the bug
  target here is any hand-rolled clickable element, e.g. the "Forgot
  password?" `<button>` in `SettingsCloud.tsx`, that isn't going through
  those primitives).
- **Button variant consistency**: one clear primary action per screen/
  form (`variant="default"`), secondary actions `outline` or `ghost` —
  audit for any raw `<button className="...">` that should be the
  shadcn `Button` component instead.
- **Text overflow**: long meeting titles, long speaker names, long
  action-item text — `truncate` for single-line contexts,
  `line-clamp-2`+tooltip for card previews, `break-words` for anything
  free-text that could be long (translated text, notes).
- **Error-message styling consistency**: audit every `text-destructive`/
  inline-error usage across the app (there are several by now —
  SettingsConfig, SettingsTranslate, SettingsCloud all have their own)
  and make sure they render the same way (same classes, same
  placement relative to the form).

## Out of scope

Dark mode (not implemented, not part of this pass), keyboard shortcuts/
command palette (a nice idea from the research but a new feature, not
a polish fix — flag it as a possible future M-something in Decisions
if you think it's worth it, don't build it here), any new screens or
features, `internal/server`/Go changes (this is `web/` only).

## Acceptance criteria

1. `cd web && bun run build` (typecheck + vite build) and
   `go build ./... && go vet ./... && gofmt -l .` (embedded assets)
   clean.
2. Every screen in section 1's list reviewed; Decisions records what
   was actually normalized (not just "done").
3. Empty and loading states added/fixed per section 2, for at least
   MeetingList and ActionItems (MeetingDetail's if time allows — note
   what was and wasn't covered).
4. LiveMeeting's auto-scroll fixed per section 3, with a clear
   before/after description of the bug and how it was verified (manual
   walkthrough is fine — describe exactly what you did, e.g. "started
   a meeting, let 5 utterances arrive while scrolled to bottom [gone],
   scrolled up, triggered 2 more utterances, confirmed no forced
   scroll + saw the new-lines affordance, clicked it, confirmed jump to
   bottom").
5. Section 4's fixes applied; Decisions notes what was found and
   fixed vs. what was already fine.
6. Manual click-through of every screen after the pass (can't run a
   real browser here — use whatever manual verification method prior
   milestones used for UI, e.g. curl-driven state changes plus reading
   the rendered component tree, or the actual `run` skill if usable in
   this environment — be honest in Validation about what was and
   wasn't actually visually verified vs. inferred from code).

## Decisions

- **Typography scale settled**: `text-xs` metadata (timestamps, speaker labels, "due" chips) · `text-sm` labels/helper/error copy · `text-base` body + transcript text (LiveMeeting utterances, MeetingDetail card bodies — both were drifted `text-sm`) · `text-lg` accent headings/brand wordmark · `text-2xl` page headings (all 7 screens already agreed; untouched). `CardTitle` keeps its primitive default (`text-base`), no per-screen overrides.
- **Spacing**: existing values were already on the 4px rhythm (`gap-1/1.5/2/3/4`, `px-6 py-8`); zero arbitrary `[Npx]` values found (grep-verified). Only normalization: NewMeetingDialog body `py-2`→`py-4` to match MeetingDetail's rename dialog.
- **Raw colors**: one offender, SettingsTranslate's Ollama badge (`bg-green-100`/`bg-amber-100`) — initially flattened to neutral tokens (regression caught in review), final: reachable = `secondary` + `bg-primary/15 text-primary`, unreachable = `variant="destructive"` (`bg-destructive/10 text-destructive`). Unreachable-is-destructive is deliberate: Ollama down genuinely blocks live translation.
- **Skeletons**: added shadcn `ui/skeleton.tsx` (bg-accent — brand tan as shimmer material); loading states content-shaped per screen (MeetingList card rows, ActionItems checkbox rows, MeetingDetail heading+3 cards). ActionItems empty state rewritten to explain provenance ("appear when a meeting ends and the agent extracts follow-ups").
- **Auto-scroll**: scroll-position threshold (120px) over IntersectionObserver — the decision only matters inside the `[utterances]` effect, so reading scrollTop/scrollHeight there fits the component's ref pattern; a scroll listener is still needed to self-dismiss the affordance, so an observer would have been a second parallel mechanism. Initial batch (prevLen 0→n) auto-scrolls unconditionally: a freshly loaded transcript should land on the newest line, which was the old behavior's one correct case.
- **Focus rings**: 5 raw `<button>`s found, all text-style buttons that shouldn't become full shadcn `Button`s (tag chips, "Forgot password?", 2 ActionItems meeting-links, App logo) — each got `focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2` + a rounding class. All actual command buttons already go through shadcn `Button` (variants audited: default/outline/secondary/ghost/destructive usage is consistent; one primary per form).
- **Text overflow**: zero truncate/break-words existed app-wide. Added: `truncate` + `min-w-0` on MeetingList card titles (single-line context); `break-words` on ActionItems item text (open + done), LiveMeeting h1, and `min-w-0` on both title-link containers; `flex-wrap` on MeetingDetail h1 (badge + rename button co-exist). MeetingDetail body text left as-wraps (block context, no fix needed).
- **Error styling convention**: `text-sm text-destructive`, page-level errors `mb-4` below the heading, form/action errors `mt-3` after the form area. Normalized: LiveMeeting `mb-2`→`mb-4`, added `mt-3` to NewMeetingDialog/SettingsConfig saveError/SettingsCloud syncError. SettingsTranslate's per-card `text-xs` pull error kept — it's inside a card body next to xs status text, a deliberate size step down.
- **Flagged future item (not built)**: keyboard shortcuts/command palette (from the research) — real feature, not polish; candidate for a future milestone.
## Progress

- [x] Typography/spacing audit across all screens
- [x] Empty/loading states (MeetingList, ActionItems, MeetingDetail)
- [x] LiveMeeting auto-scroll-with-intent fix
- [x] Focus rings / button variants / text overflow / error-styling audit
- [x] Manual verification pass, Validation recorded
## Validation

2026-09-22. `cd web && bun run build` (tsc -b && vite build) clean after
every step; `go build ./... && go vet ./... && gofmt -l .` clean
(embedded dist rebuilt each step). Commits: a4e6400 (typography/tokens),
669bd20 (badge signal fix), 847eebf (skeletons/empty states), 9080eaa
(auto-scroll), plus this step's.

**Verification honesty (criterion 6)**: no browser or browser-automation
tooling exists in this environment — verification was code-reading +
build/typecheck, the same limitation M13d/M15 recorded for UI work.
Specifically:
- Typography/spacing/overflow/error-class changes: asserted per-file via
  grep counts before/after (e.g. zero `[Npx]` arbitrary values, exactly 2
  raw title-link buttons) — mechanically checkable, visually inferred.
- Auto-scroll (criterion 4): verified by code-path walkthrough, not a
  click-through. Common case: initial batch auto-scrolls (prevLen 0→n
  branch); each new utterance reads atBottomRef (kept true by the scroll
  listener after the previous auto-scroll) → scrollToBottom. Scrolled-up
  case: listener sets atBottomRef=false ≥120px from bottom → next batch
  skips scrolling, sets the New-lines pill; pill dismisses on click,
  on manual scroll-to-bottom (listener), or on meeting switch (reset
  effect). A real browser walkthrough remains open — noted as a known
  gap for the next time a browser is available.
- Focus rings: applied the same class set to all 5 raw buttons; no
  visual confirmation possible.

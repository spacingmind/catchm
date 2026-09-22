# Plan: M16 — UI polish pass

- Status: active
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

(fill in: exact typography/spacing scale settled on, which empty-state
copy was used per screen, IntersectionObserver vs. scroll-threshold
choice for the auto-scroll fix and why, anything left as a flagged
future item rather than fixed now.)

## Progress

- [ ] Typography/spacing audit across all screens
- [ ] Empty/loading states (MeetingList, ActionItems, MeetingDetail)
- [ ] LiveMeeting auto-scroll-with-intent fix
- [ ] Focus rings / button variants / text overflow / error-styling audit
- [ ] Manual verification pass, Validation recorded

## Validation

(to be filled as criteria are confirmed)

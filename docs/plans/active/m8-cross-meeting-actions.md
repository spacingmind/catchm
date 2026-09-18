# Plan: M8 — cross-meeting action items view

- Status: active
- Created: 2026-09-18

## Goal

`ActionItem`s currently only exist embedded inside each meeting's
frontmatter, visible one meeting at a time on `MeetingDetail`. Add a
single aggregated view across all meetings — "what do I still owe
across every meeting" — with the ability to check an item off without
opening each meeting individually.

Depends on M7 being merged first only loosely (this milestone doesn't
need M7's tags/rename/delete work, but if M7 hasn't landed yet, that's
fine — this can be implemented independently against the current
`Meeting`/`ActionItem` structs). Read `internal/meeting/meeting.go`'s
current `ActionItem`/`Meeting` structs and `internal/tools/tools.go`
first regardless, to confirm the exact current shape before writing
code (M7 may have already landed and changed nearby code slightly).

## Scope

- `internal/tools/tools.go`: add `Surface.ListActionItems() ([]ActionItemRef, error)`
  where `ActionItemRef` (new type, define alongside `Surface` or in
  `internal/meeting` — your call, record which) wraps a
  `meeting.ActionItem` plus `MeetingID string` and `MeetingTitle string`
  and its own index within that meeting's `ActionItems` slice (needed to
  address it for the toggle endpoint below). Implementation: `List()`
  all meetings, flat-map their `ActionItems`.
- `internal/meeting`: add `Store.SetActionItemStatus(meetingID string, index int, status string) error`
  — loads the meeting via `UpdateDoc`, bounds-checks `index` against
  `len(m.ActionItems)` (return a clear error, not a panic, on an
  out-of-range index — this is the main correctness trap here, a stale
  client-side index after the underlying list changed shouldn't crash
  the server), sets `ActionItems[index].Status`, saves.
- `internal/server`: add `GET /api/action-items` (returns the flat list
  from `Surface.ListActionItems`, sorted with open items first — read
  how `sortMeetings` in `internal/meeting/meeting.go` is structured for
  the existing sort-by-time convention and write something analogous
  rather than pulling in a new dependency) and
  `PATCH /api/meetings/{id}/action-items/{index}` accepting
  `{"status": "..."}` (400 on a non-numeric `{index}` path value, 404 on
  unknown meeting or out-of-range index — distinguish these two 404
  causes in the response's error message so it's debuggable, they don't
  need distinct HTTP codes).
- Web: new `web/src/components/ActionItems.tsx` screen — a flat list of
  action item rows (text, owner, due, a checkbox or a status `Select`
  reflecting `status` — reuse whatever status vocabulary
  `MeetingDetail.tsx` already displays for action items, don't invent a
  new one), each row showing which meeting it came from (small link/label
  using `MeetingTitle`, clicking it opens that `MeetingDetail`). Toggling
  a checkbox calls the new `PATCH` endpoint and updates local state
  optimistically (revert on error). Add this as a new top-level nav
  entry in `App.tsx` alongside "Meetings"/"Settings" (a plain text nav
  item is fine, doesn't need to match the existing "Settings" button's
  exact styling if that's inconvenient — just be consistent with
  whatever `App.tsx` looks like by the time you read it, M6/M7 may have
  already changed its header layout).
- `web/src/lib/api.ts`: `listActionItems()` /
  `updateActionItemStatus(meetingId, index, status)` typed functions.

Out of scope: reordering/prioritizing across meetings, due-date sorting
or reminders/notifications, editing an action item's text/owner/due from
this view (status toggle only — full edit still happens by re-running
`refine` or by whatever future editing UI `MeetingDetail` gets), a
kanban/board layout (a flat list is enough for this milestone).

## Acceptance criteria

1. `task build && task test && task lint` pass. `task web:install &&
   task web:build` succeeds, no TypeScript errors.
2. `TestListActionItemsAcrossMeetings` (internal/tools) — two meetings
   with action items each; the flat list contains all of them tagged
   with the right `MeetingID`/`MeetingTitle`.
3. `TestSetActionItemStatusBoundsChecked` (internal/meeting) — a valid
   index updates status and persists; an out-of-range index returns an
   error, doesn't panic, doesn't corrupt the file (verify the doc is
   unchanged after the failed call).
4. `TestActionItemsEndpoint` / `TestActionItemStatusEndpoint`
   (internal/server) — GET returns the aggregated list; PATCH with a bad
   index is 404, not 500; a successful PATCH is reflected in a
   subsequent GET.
5. Manual/browser verification: the new nav entry opens the flat action
   items list populated from at least two different seeded meetings,
   toggling a status persists across a page reload.

## Decisions

- `ActionItemRef` lives in `internal/tools` (defined alongside `Surface`):
  an embedded `meeting.ActionItem` plus `MeetingID`/`MeetingTitle` string
  fields and `Index int`, so its JSON keeps text/owner/due/status at the
  top level of each row (no nested `item` object for the web client).
- Sort order: `Surface.ListActionItems` keeps `Store.List`'s newest-meeting-
  first order, in-document order within a meeting. The open-items-first
  grouping the spec asked for happens client-side in `ActionItems.tsx`
  (open rows first, then a "Done" section of struck-through rows) — the
  API itself stays a stable flat list (agreed in the step-2 review:
  "keep whatever sort order you already implemented").
- Out-of-range index: `internal/meeting` exports a sentinel
  `ErrActionItemIndex` (wrapped with meeting id/count context via `%w`);
  `SetActionItemStatus` bounds-checks before mutating, so a failed call
  leaves meeting.md byte-identical. The PATCH endpoint maps it to 404 and
  `ErrNotFound` to a different-text 404 ("action item index out of range"
  vs "meeting not found"), per spec — same code, distinct messages.
- Status vocabulary: binary checkbox. MeetingDetail only renders raw
  status text (no fixed vocabulary exists to reuse), so the screen treats
  `status === "done"` as closed and anything else (including empty) as
  open; toggling sets `"done"` or `""`. PATCH rejects an empty status with
  400 only when it is the *only* thing wrong with the request body; a
  successful PATCH responds with the updated meeting JSON.
- Web: `ActionItems.tsx` toggles optimistically (revert + error banner on
  PATCH failure); each row shows owner (Badge), due date, and a clickable
  `MeetingTitle` label that GETs the meeting and routes through App.tsx's
  existing `openMeeting`/View state machine; rows keyed by
  `meeting_id:index`. Nav entry: a ghost `Button` with a `CheckSquare`
  icon ("Action items"), styled exactly like the existing Settings button,
  both wrapped in a flex container in the header.

## Progress

- [x] ActionItemRef type + Surface.ListActionItems
- [x] Store.SetActionItemStatus (bounds-checked)
- [x] GET /api/action-items + PATCH /api/meetings/{id}/action-items/{index}
- [x] web: ActionItems screen + nav entry
- [x] tests

## Validation

1. Confirmed: `go build ./...`, `go vet ./...`, `gofmt -l .` clean,
   `go test ./... -count=1` all green; `task web:install && task
   web:build` succeeds with no TypeScript errors (also `bun run lint`
   reports 0 errors).
2. Confirmed: `TestListActionItemsAcrossMeetings` (internal/tools) — three
   items across two meetings (plus one item-less meeting contributing
   nothing) with correct `MeetingID`/`MeetingTitle`/`Index`, newest
   meeting first.
3. Confirmed: `TestSetActionItemStatusBoundsChecked` (internal/meeting) —
   valid index updates and persists; index 2 and -1 both return
   `ErrActionItemIndex` without panic and meeting.md is byte-identical
   after the failed calls; unknown meeting still `ErrNotFound`.
4. Confirmed: `TestActionItemsEndpoint` / `TestActionItemStatusEndpoint`
   (internal/server) — GET aggregates 3 items from 2 meetings; bad index
   path value 400; out-of-range index 404 (not 500) with "out of range"
   in the message vs "meeting not found" for the unknown-meeting 404;
   successful PATCH reflected in a subsequent GET.
5. Confirmed by the task owner, independently of the implementer: real
   binary built, two meetings seeded with action items via curl, and the
   full flow verified in a headless browser with screenshots — open items
   grouped first, a DONE section with strikethrough for completed ones,
   checkbox toggling updating in real time (the item moved between
   sections and persisted), meeting title/owner/due all rendered
   correctly.

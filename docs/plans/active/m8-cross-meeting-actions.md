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

(fill in: exact `ActionItemRef` shape/location, sort order chosen, nav
entry styling, whether status is a checkbox (binary) or reuses a richer
status vocabulary already present in `MeetingDetail`.)

## Progress

- [ ] ActionItemRef type + Surface.ListActionItems
- [ ] Store.SetActionItemStatus (bounds-checked)
- [ ] GET /api/action-items + PATCH /api/meetings/{id}/action-items/{index}
- [ ] web: ActionItems screen + nav entry
- [ ] tests

## Validation

(to be filled as criteria are confirmed)

# Plan: M7 — meeting management (rename, delete, search UI, tags/projects)

- Status: active
- Created: 2026-09-18

## Goal

Three related gaps in meeting-level management, all touching the same
files, bundled into one milestone:

1. **Rename/delete**: `internal/meeting.Store` can create, end, and
   update-via-`UpdateDoc` a meeting, but has no delete, and there is no
   API/UI path to rename a meeting's title after creation.
2. **Search UI**: `internal/tools.Surface.SearchMeetings` and the
   `GET /api/meetings?q=` query param already exist and work (verify by
   reading `internal/server/server.go`'s `handleListMeetings` — the `q`
   param is already wired), but `MeetingList.tsx` has no search input in
   the UI at all.
3. **Tags/projects**: there is no way to group meetings (e.g. by
   project). Add a simple `Tags []string` field on `Meeting` (frontmatter
   `tags:` list) — not a separate entity/table, just labels on the
   meeting doc, filterable client-side or via an extended search.

## Scope

### Rename & delete

- `internal/meeting/meeting.go` or `transcript.go`: add
  `Store.Delete(id string) error` — removes the meeting's whole directory
  (`os.RemoveAll`), returning `ErrNotFound` if it doesn't exist first
  (check via `Get`/`os.Stat` before removing, don't just let `RemoveAll`
  silently no-op on a missing dir — the caller needs to distinguish
  "deleted" from "already gone").
- Renaming reuses the existing `Store.UpdateDoc` — no new Store method
  needed, just wire it through the API.
- `internal/tools`: add `Surface.DeleteMeeting(id string) error` and
  `Surface.RenameMeeting(id, title string) error` (the latter via
  `UpdateDoc`), following the existing thin-wrapper style of every other
  `Surface` method in that file.
- `internal/server`: add `DELETE /api/meetings/{id}` (404 if missing,
  204 on success — also stop any live pipeline for that id first via the
  existing `stopPipeline` if it's somehow still active, though deleting
  an active meeting should probably 409 instead — read how
  `handleRefine`'s active-meeting check works and mirror that pattern)
  and `PATCH /api/meetings/{id}` accepting `{"title": "..."}` (400 if
  title is empty, 404 if missing, returns the updated meeting doc).

### Search UI

- `web/src/lib/api.ts`: `listMeetings` likely already accepts a query
  param per the spec note above — confirm, and if the function signature
  doesn't yet expose it, add the `q` parameter.
- `web/src/components/MeetingList.tsx`: add a search `Input` above the
  meeting cards (shadcn `Input` with a search icon from `lucide-react`),
  debounced (e.g. 300ms `setTimeout`, cancel on unmount/re-type — don't
  fire a request per keystroke) calling `listMeetings(query)` and
  re-rendering the list. Preserve the existing empty-state copy for "no
  meetings at all", but add a distinct "No matches in the stash." message
  (from `docs/ui-brainstorm.md`'s empty-states section) for "search
  returned zero results while meetings do exist".

### Tags/projects

- `internal/meeting/meeting.go`: add `Tags []string` to `Meeting`
  (`yaml:"tags,omitempty" json:"tags,omitempty"`).
- `internal/meeting.StartOptions`: add `Tags []string`, threaded through
  `Store.Start` into the new `Meeting.Tags` field (mirror exactly how
  `Engine`/`Lang`/`TranslateTo` are already threaded through
  `StartOptions` → `Start` → `Meeting`).
- `internal/tools`, `internal/server`: extend `StartMeetingWithOptions`
  call sites and `POST /api/meetings`'s request body to accept an
  optional `tags: string[]`. Also allow tags to be edited after creation
  via the same `PATCH /api/meetings/{id}` endpoint added above (body
  becomes `{"title"?: string, "tags"?: string[]}`, both optional,
  at-least-one-present).
- Web: `NewMeetingDialog.tsx` gets a simple tags `Input` (comma-separated
  free text, split/trim/dedupe client-side into a string array on
  submit — no fancy tag-picker component, no new dependency).
  `MeetingList.tsx` renders each meeting's tags as small `Badge`s on its
  card, and add a lightweight client-side tag filter: a row of tag chips
  above the list (derived from the currently-loaded meetings, click to
  toggle filtering to that tag) — this can be pure client-side filtering
  over the already-fetched list, no new API needed for filtering by tag
  (only the free-text `q` search hits the server). `MeetingDetail.tsx`
  displays the meeting's tags (read-only display is enough for this
  milestone — inline tag editing on the detail screen is a nice-to-have,
  not required).

### Light responsive touch (bundled here since this milestone already
touches `MeetingList.tsx` the most)

The desktop shell (M5) window is resizable, unlike a fixed-width mobile
viewport. Check `MeetingList.tsx`'s and `App.tsx`'s outer containers:
they currently use a fixed `max-w-3xl mx-auto` centered column
regardless of window width. Keep that pattern (don't redesign into a
sidebar/dashboard layout — that's out of scope), but verify by resizing
a real browser window from ~800px to ~1600px wide that nothing looks
broken (search bar, tag filter row, cards) — if the centered column at
`max-w-3xl` already handles this gracefully (it should, that's what
`mx-auto` + a max-width is for), no code change is needed here beyond
confirming it with a screenshot at a wide viewport; only fix something
if you actually find breakage.

Out of scope: bulk delete/multi-select, tag autocomplete/suggestions
from existing tags, a dedicated "grouped by project" alternate view
(that's effectively what the tag filter chips give you already at this
scope), undo for delete (a confirmation `Dialog` before delete is
enough safety).

## Acceptance criteria

1. `task build && task test && task lint` pass. `task web:install &&
   task web:build` succeeds, no TypeScript errors.
2. `TestDeleteMeeting` (internal/meeting) — deletes an existing meeting,
   `ErrNotFound` on a second delete or an unknown id.
3. `TestRenameMeeting` / reuse of `UpdateDoc` — a rename persists and
   round-trips through `Get`.
4. `TestDeleteEndpoint` / `TestRenameEndpoint` (internal/server) — HTTP
   status codes per the spec (404/409/204/200).
5. `TestTagsRecordedAndReturned` — a meeting started with tags has them
   in its frontmatter and in the `POST`/`GET` JSON responses.
6. Manual/browser verification: search input filters the list, tag
   badges render on cards, tag filter chips toggle client-side
   filtering, a delete confirmation dialog appears and actually removes
   the meeting from the list after confirming, rename updates the title
   shown in the list and detail screens.

## Decisions

(fill in: exact delete-confirmation UI pattern chosen — shadcn
`AlertDialog` if available/added, or reusing `Dialog`; whether renaming
happens inline on the detail screen or via a small dialog; how tag
chips are laid out.)

## Progress

- [ ] Store.Delete + Surface.DeleteMeeting/RenameMeeting
- [ ] DELETE/PATCH /api/meetings/{id} endpoints
- [ ] Search input wired into MeetingList
- [ ] Tags field end-to-end (Meeting struct, StartOptions, API, NewMeetingDialog, MeetingList badges + filter, MeetingDetail display)
- [ ] Responsive check at wide viewport
- [ ] tests

## Validation

(to be filled as criteria are confirmed)

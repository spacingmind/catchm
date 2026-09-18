# Plan: M7 — meeting management (rename, delete, search UI, tags/projects)

- Status: complete
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

- Delete confirmation reuses the existing shadcn `Dialog` (no
  `AlertDialog` component in web/src/components/ui and none was added);
  destructive-styled confirm button, plain Cancel, disabled while the
  DELETE request is in flight.
- Renaming happens via a small `Dialog` opened from a pencil-icon ghost
  button next to the title on the detail screen (not inline editing) —
  it mirrors the NewMeetingDialog form pattern already in the codebase.
- DELETE of an active meeting returns 409 (mirrors handleRefine's
  active-meeting conflict); 404 when the id is unknown, 204 on success
  after stopping any live pipeline for that id.
- PATCH body uses pointer-field semantics (`{"title"?, "tags"?}`, both
  optional, at-least-one-present ⇒ 400) so tags can be set to `[]`
  distinctly from "left alone"; a present-but-empty title is a 400.
- Tag editing after creation goes through the same PATCH endpoint via a
  new `Surface.SetMeetingTags` thin wrapper (title-only got
  `Surface.RenameMeeting`), both on top of the existing `UpdateDoc`.
- Tags entry in NewMeetingDialog is a plain comma-separated `Input`,
  split/trim/deduped client-side on submit; no tag-picker dependency.
- Tag chips row sits between the search input and the card list:
  outline Badges for inactive, filled for the active filter, click
  toggles (click again to clear), derived client-side from the loaded
  meetings; per-card tags render as secondary Badges inline after the
  decisions/action-items counts.
- Search is server-side (`GET /api/meetings?q=`) debounced 300ms with
  the timer cleared on unmount/re-type; tag filtering is pure
  client-side over the already-fetched list, no extra API call.
- Empty states: "Your burrow is quiet." kept for zero meetings total;
  "No matches in the stash." shown only when meetings exist but the
  search (or search + tag filter) yields none.
- Responsive: kept `max-w-3xl mx-auto` — no redesign. Verified via
  headless-browser screenshots at 800px and 1600px: column stays
  centered, search bar/chips/cards render with no overlap or clipping.
  No code change needed.

## Progress

- [x] Store.Delete + Surface.DeleteMeeting/RenameMeeting
- [x] DELETE/PATCH /api/meetings/{id} endpoints
- [x] Search input wired into MeetingList
- [x] Tags field end-to-end (Meeting struct, StartOptions, API, NewMeetingDialog, MeetingList badges + filter, MeetingDetail display)
- [x] Responsive check at wide viewport
- [x] tests

## Validation

- `go build ./...`, `go vet ./...`, `gofmt -l .` clean;
  `go test ./... -count=1` all ok (incl. new TestDeleteMeeting,
  TestDeleteEndpoint, TestRenameEndpoint, TestTagsRecordedAndReturned
  alongside the pre-existing suite). `task web:install` and
  `task web:build` succeed with no TypeScript errors.
- Store-level: delete removes the directory, second delete and unknown
  id return ErrNotFound; rename round-trips through Get.
- HTTP-level: DELETE 404 unknown / 409 active / 204 success; PATCH 400
  (bad json, empty/absent fields) / 404 unknown / 200 with updated doc;
  tags present in POST and GET JSON and in the meeting.md frontmatter,
  replaceable via PATCH.
- Independently verified by the user via real headless-browser
  screenshots against a real binary with curl-seeded meetings: search
  filtering, tag badges + filter chips, and the ended-meeting detail
  screen (rename pencil icon, red Delete button) all render and work;
  the agent additionally screenshotted the list at 800px and 1600px
  (centered column intact, no breakage).

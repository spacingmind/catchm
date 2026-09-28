# Plan: Group meetings by project

- Status: completed 2026-09-28 (branch `feat/projects`); the user chose "project = special tag".
  Starts after meeting-insights P1 merges, because both touch
  MeetingDetail.tsx and api.ts.
- Created: 2026-09-28

## Why

People working on several projects want their meetings grouped by
project. Meetings already have `tags` (frontmatter, `PATCH
/api/meetings/{id}`, a tag filter on the Meetings list), but there's no
UI to set them and no project concept.

## Decision: a project is a tag with the `project:` prefix

- `tags: ["project:CatchM Cloud", "client A"]`. The name after the
  prefix is the display name, with its case kept.
- Projects match case-insensitively, after trimming and collapsing
  spaces.
- **At most one project per meeting.** The UI enforces it. A backend
  helper takes the first project tag if there are several.
- **Why not a new `project` field:**
  - `internal/meeting/doc.go` round-trips the frontmatter through a
    fixed struct, so an older client drops any field it doesn't know the
    moment it edits a meeting (rename, action status, tags) and syncs.
  - Tags survive every existing client, cloud sync and the MCP tools.
  - An older client shows the project as a plain `project:…` chip, which
    is cosmetic only.
- **No registry file.** The project list is derived from the meetings'
  tags. A project without meetings doesn't exist, which is fine for now.
  Colours are derived from a hash of the name.

## Contract

**Backend:**
- `meeting.ProjectOf(tags []string) string`: the display name, or "".
- `meeting.WithProject(tags []string, project string) []string`:
  replaces any project tag with `project` (or removes it when `project`
  is empty) and keeps the other tags.
- **`GET /api/projects`** returns `[{name, meetings, last_meeting_at}]`,
  sorted by most recent meeting.
- **`POST /api/projects/rename {from, to}`** rewrites the project tag on
  every matching meeting. It returns `{renamed: n}`. Renaming onto an
  existing project merges the two.
- **Start and import accept an optional `project`,** which is added as
  the project tag:
  - `POST /api/meetings` takes it in the JSON body.
  - `POST /api/meetings/import` takes it as a form field.
- `PATCH /api/meetings/{id}` with `tags` is unchanged. The UI sends the
  full tag list.

**Web API:**
- `listProjects()`, `renameProject(from, to)`
- A `project` option on the start and import calls
- Helpers `projectOf(tags)` and `withProject(tags, name)`

**UI:**
- **Sidebar:** a "Projects" section with the most recent projects (up to
  about 6), then "All projects…". Clicking one opens Meetings filtered to
  it via `#/meetings?project=<name>`.
- **Meetings list:**
  - A project filter, shown as a chip with a clear button.
  - A "Group by: Date / Project" toggle, remembered.
  - Meetings with no project go under "No project".
  - Project tags show as a distinct badge (folder icon) and never as
    plain tag chips.
- **Meeting page:**
  - A project picker: a combobox listing existing projects, plus
    "New project…" and "No project".
  - A simple tag editor for other tags: add, remove, and autocomplete
    from existing tags.
- **Start popover and Import dialog:** a project select defaulting to
  the last used project (stored under a localStorage `catchm.prefs.*`
  key), plus a "New project…" option.
- **Action items:** a Project filter group next to Owner and Meeting.
- **Projects management:** on the project filter / "All projects" view,
  allow a rename (through `renameProject`).
- **Copy:** plain wording, e.g. "Project", "No project", "New project…",
  "Move to project".

**Later, with meeting-insights P2:** after a summary, if
`analysis.entities` has a `project:` entity and the meeting has no
project, suggest "This meeting seems to be about <X>. Add it to that
project?".

## Acceptance criteria

1. **Assigning a project:** from the meeting page, Start and Import. It
   persists as a `project:` tag, and a rename on another (old) client
   keeps it.
2. **Filtering and grouping:** the sidebar Projects list and the list
   filter/grouping work, and "No project" groups the rest.
3. **Rename:** rename moves every meeting, and renaming onto an existing
   project merges them.
4. **Action items:** they filter by project.
5. **Checks:** `task test`, `task lint`, `bunx tsc -b`, `bun run build`
   and oxlint are clean. Tests cover ProjectOf/WithProject, the projects
   endpoint and rename.

## Status (2026-09-28)

Implemented on `feat/projects`, and smoke-tested in a browser against
`catchm serve` (fresh CATCHM_HOME).

Choices made during implementation:
- **Spelling:** when the same project is written with different case,
  `GET /api/projects` shows the most-used spelling. A tie goes to the
  newest meeting's spelling. `WithProject` puts the project tag first.
- **Validation:** names are normalized (trim, collapse spaces) and must
  be 1 to 80 characters. Rename returns 400 otherwise, and so do start
  and import when `project` is set.
- **Action items:** `ActionItemRef` has a new `meeting_project` field
  (omitted when empty), so the Project filter needs no extra fetch.
- **"All projects":** its own route, `#/projects`, beside
  `#/meetings?project=<name>`.
- **Date grouping:** Today, Yesterday, Last 7 days, then one group per
  month.
- **MCP:** there is no MCP server or tag filter yet, so nothing was
  added there.

Not covered: the desktop shell (Wails), and dark-mode visuals (the
automated browser couldn't take screenshots).

## Status (2026-09-28)

Done:
- **Backend:** `ProjectOf`/`WithProject`, `GET /api/projects`,
  `POST /api/projects/rename` (merges onto an existing project), a
  `project` field on start and import, and `meeting_project` on action
  items.
- **UI:**
  - a sidebar Projects section
  - an All projects page (`#/projects`) with rename and merge
  - the Meetings list: grouping by Date or Project, and the project
    filter via `#/meetings?project=`
  - on the meeting page, a project picker and a tag editor
  - a project select in Start and Import
  - an Action items Project filter

Checked:
- `task test`, `task lint`, `tsc`, build and oxlint are clean.
- On `catchm serve`, over HTTP and the DOM:
  - A case-insensitive merge keeps the most-used spelling.
  - The sidebar lists projects.
  - Grouping gives "Website redesign · 3" and "No project · 1".
  - A sidebar click filters the list.
  - The picker and tag editor are on the meeting page.

Left for meeting-insights P2: suggest a project from
`analysis.entities`.

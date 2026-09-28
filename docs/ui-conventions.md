# UI conventions

The web UI's design language, taken from the established screens
(Settings → Recording / Transcription, `settings/pieces.tsx`, the meeting
page's header and Overview, the Meetings cards, Action items and the
sidebar). New screens should use these tokens and primitives. Colours
are the tokens in `web/src/index.css` (see [brand.md](brand.md)).

## Type scale

| Role | Classes | Where |
|---|---|---|
| Page h1 | `text-2xl font-semibold text-balance`; optional `size-6` icon, `flex items-center gap-2` | Meetings, Action items, Projects, Settings, meeting title |
| Section heading (h2) | `text-lg font-semibold`; `size-4` icon, `gap-2` | `SettingsSection`, meeting Overview sections, callout cards, flow cards |
| Group heading (h2 in a list) | `text-sm font-medium text-muted-foreground` | date/project groups, action-item groups |
| List card title | `CardTitle` default (`text-base font-medium`) | meeting and project cards |
| Row / field label | `text-sm font-medium` (`Label`, `SettingRow` label) | settings rows, dialog and popover fields |
| Inline control label | `text-xs font-medium text-muted-foreground` | "Project", "Owner", "Group by", "Tags" in front of chips or a control |
| Description / help (page) | `text-sm text-muted-foreground` | settings rows, tab intros, card descriptions |
| Field hint (dialog / popover) | `text-xs text-muted-foreground` | Start meeting, Import, project select |
| Meta / caption | `text-sm text-muted-foreground` (card date); `text-xs text-muted-foreground` (item meta row, speaker name) | |
| Content text | `text-base` | decisions, summary, notes, transcript |
| Badge text | `Badge`: `text-xs font-medium`, `h-5` | |

## Weights and colours

| Use | Token |
|---|---|
| Primary text | `text-foreground` (inherited) |
| Secondary text, hints, empty messages | `text-muted-foreground` |
| Error text | `text-destructive` (via `StatusMessage tone="error"`) |
| Non-fatal warning | `text-warning`, `font-medium`, with a `TriangleAlert` icon |
| "Ready / done / on" badge | `Badge variant="secondary"` + `border-transparent bg-primary/15 text-primary` |
| "Not ready" (blocking) badge | `Badge variant="destructive"` |
| "Checking…" badge | `Badge variant="outline"` + `motion-safe:animate-pulse` |
| Selected surface | `bg-muted` (nav), `border-primary bg-primary/5` (choice card) |
| Categorical colours | the project-dot palette (`PROJECT_COLORS`, `lib/projects.ts`); the mind-map branches (`insights/flow.css`) use the same hues. Nothing else is hard-coded. |

Weights: `font-semibold` for h1/h2 only; `font-medium` for labels,
card titles, buttons, badges; everything else normal.

## Control sizes

| Control | Size | Where |
|---|---|---|
| Page / header action | `Button` default (`h-8`, `text-sm`, icon `size-4`) | Redo summary, Delete, Find flows, dialog footers, empty-state CTA |
| Inline / row action | `Button size="sm"` (`h-7`, `text-[0.8rem]`, icon `size-3.5`) | Try again, Open settings, Set up, Add key, Rename, Hide, Clear filters |
| Retry after a load error | `Button size="sm" variant="outline"` "Try again" | |
| Icon button | `icon-sm` (`size-7`) in rows and bars; `icon` in the sidebar | |
| Text link inside a meta row | `Button variant="link" size="xs"`, `text-muted-foreground hover:text-foreground` (`SourceLink`) | the only `xs` control |
| Inline text link | `ExternalLink` (`font-medium text-foreground underline underline-offset-2 hover:text-primary`); in-app: `underline-offset-2 hover:text-foreground hover:underline` | |
| Input / select | `h-8` (default); `h-7` only when it shares a row with `sm` chips (tag field) | |
| Filter chip | `FilterChip` (`Toggle` outline `sm`, `h-7`, `rounded-full`); a removable chip is `Badge` `h-6` with a `size-5` remove button (`X size-3`) | |
| 2–4 choices | `SegmentedControl` | |
| Icons | `size-4` in `text-sm`/`text-base`/`text-lg` text and default buttons; `size-3.5` in `sm` controls and `text-xs` rows; `size-6` in the h1; always `aria-hidden` | |
| Menu rows | `SelectItem`: `rounded-md px-1.5 py-1 gap-1.5 text-sm`, leading marker in a `size-4` slot | select, project picker |

## Spacing

| Use | Value |
|---|---|
| Page | `mx-auto max-w-3xl px-6 py-8` (`max-w-4xl` Settings, `max-w-6xl` graph tabs) |
| h1 to content | `mb-6` (`mb-2` + description `mb-6` when there is one) |
| Stacked page controls (search, filters, status) | `mb-4` |
| Sections | `mb-8`; heading to body `mb-2` (description `mt-0.5`) |
| Setting row | `py-4`, `border-b` between rows, `gap-8` label/control |
| Card lists | `grid gap-3`; groups `gap-6`, heading to cards `gap-2` |
| Form fields (dialog / popover) | form `grid gap-4`; label/field/hint `grid gap-1.5` |
| Text + action row | `flex flex-wrap items-center gap-3`, text `min-w-0 flex-1` |
| Chip rows | `flex flex-wrap items-center gap-2` |

## Radii, borders, cards

| Element | Style |
|---|---|
| Card | `Card` (`rounded-xl`, `ring-1 ring-foreground/10`, `py-4`/`px-4`), no extra border. A callout is a plain `Card` whose title is an h2 (`CardTitle className="text-lg font-semibold"`) |
| Toolbar panel | `rounded-xl border border-border bg-card px-4 py-3` (live StatusRow, evidence bar) |
| Nested box inside a card | `rounded-lg border border-border p-3` (choice card, setup options, notices) |
| Graph canvas | `rounded-xl border border-border` (`rounded-lg` when nested in a card) |
| Controls | `rounded-lg` (buttons, inputs); chips and badges `rounded-full` |
| Dividers | `border-b border-border`, `last:border-b-0` |

## Patterns

| Pattern | How |
|---|---|
| Empty state (page or tab) | `rounded-xl border border-dashed border-border px-6 py-16 text-center`; title `text-lg font-medium`; body `mt-1 text-sm text-pretty text-muted-foreground`; CTA `mt-6` default button (`InsightEmpty` for graph tabs) |
| Empty after filtering | `text-sm text-muted-foreground` + `Button size="sm" variant="outline"` "Clear filters" |
| Empty section | one `text-sm text-muted-foreground` line ("No decisions yet. …") |
| Loading | `Skeleton`s in the content's shape, `aria-busy`, an sr-only "Loading …" status |
| Inline status | `StatusMessage` (always mounted; `text-sm` by default, `text-xs` only in dense rows like item cards and download progress); errors end with a next step; `SaveStatus` in settings |
| Badges | status (see colours), owner `outline`, tag `secondary`, project `ProjectBadge`; `Live` is the default badge |
| Links | see Control sizes: external `ExternalLink`, in-app muted with hover underline, `SourceLink` for evidence |
| Copy | sentence case everywhere ("Mind map", "Open settings"); product and page names keep their case (CatchM Cloud, Cloud sync, Settings → Summaries) |

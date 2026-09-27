# Plan: Web UI audit follow-ups

- Status: completed (2026-09-28). The Windows desktop checks listed under Results are still pending.
- Created: 2026-09-28
- Branch: `fix/web-ui-audit`, stacked on
  [web-ui-audit-fixes](web-ui-audit-fixes.md)
- Repo: `catchm`, covering `web/`, `internal/server`, `internal/update` and
  `cmd/catchm-desktop`

## Why

The audit fixes left four items open:

1. **Server-URL confirm fires too often.** It appears on every URL
   change, because the API can't say whether account data is stored.
2. **An update download can't be cancelled.**
3. **App state isn't in the URL.** Reload or Back loses your place, and
   meetings can't be deep-linked.
4. **The app is light-only.** The user decided on 2026-09-28 to add a
   real dark theme that follows the OS, plus a Light / Dark / System
   setting.

We also split the bundle, since the main chunk is 515 kB and trips
Vite's 500 kB warning.

## Decisions

- **Routing is hash-based.** It works the same under `catchm serve` and
  the Wails asset server, and needs no Go SPA fallback or router
  dependency. Routes:
  - `#/meetings`
  - `#/meetings/{id}` (plus `?tab=transcript`)
  - `#/live/{id}`
  - `#/action-items`
  - `#/settings/{section}`

  Back and Forward navigate. Unknown routes fall back to `#/meetings`.
- **The theme setting is per device, stored in localStorage.** The key
  is `catchm.theme`, with values `"system"` (the default), `"light"` or
  `"dark"`. The `.dark` class on `<html>` drives the `dark:` variant,
  which `index.css` already scopes to `.dark`. A tiny inline script in
  `index.html` applies the class before first paint, so there's no
  flash.
- **`account_stored` is an additive boolean** on `GET /api/cloud/status`.
  No existing field changes.

## Tracks (parallel, disjoint files)

### T1: cloud `account_stored`
Files:
- `internal/server/cloud.go` and its tests
- `web/src/lib/api.ts` (only the CloudStatus type)
- `web/src/components/settings/SettingsCloudSync.tsx`

- [x] `/api/cloud/status` reports `account_stored: true` while logged
  out if the account params that a server change would clear are still
  on disk. It is also true while logged in.
- [x] A Go test covers logged out with params, logged out without, and
  logged in.
- [x] The server-URL ConfirmDialog appears only when `account_stored`
  is true. Otherwise the URL saves directly.

### T2: cancel update download
Files:
- `internal/update/*`
- `cmd/catchm-desktop/update*.go`
- `web/src/lib/wailsRuntime.ts`
- `web/src/components/useUpdateInstall.ts`
- `web/src/components/UpdateBanner.tsx`
- `web/src/components/settings/SettingsSystem.tsx`, update row only

- [x] A `CancelDownload` bridge method cancels the in-flight download
  through its context and removes any partial file.
- [x] Download state returns to "available", so the user can retry.
- [x] Cancelling when nothing is downloading does nothing.
- [x] The banner and Settings show "Cancel" while downloading, with a
  live status message.
- [x] Go tests cover cancel mid-download (partial file removed) and
  retry after cancel.

### T3: URL routing and code split
Files:
- `web/src/App.tsx`
- `web/src/components/AppSidebar.tsx`
- `web/src/components/settings/Settings.tsx`
- `web/src/components/MeetingDetail.tsx`, tab state only
- `web/src/components/ActionItems.tsx`
- new `web/src/lib/route.ts`

- [x] The view, selected meeting, settings section and the meeting
  detail tab live in the hash route.
- [x] Reload restores the view. Back and Forward work.
- [x] Wails-driven navigation (tray, toasts, shortcuts) updates the
  route.
- [x] `#/live/{id}` for a meeting that has ended opens its detail.
  Live never remounts on a compact toggle.
- [x] Settings, MeetingDetail and ActionItems load with `React.lazy`
  and Suspense skeletons. The main chunk falls below 500 kB, so
  Vite's warning goes away.
- [x] A long meeting-title FilterChip in ActionItems truncates.

### T4: dark theme
Files:
- `web/src/index.css`
- `web/index.html`
- `web/src/main.tsx`
- new `web/src/lib/theme.ts`
- new `web/src/components/settings/SettingsAppearance.tsx`
- `cmd/catchm-desktop/main.go`, window theme and background only

- [x] A `.dark` token set derived from the brand palette
  (`docs/brand.md`).
  - Text is at least 4.5:1.
  - The ring and input borders are at least 3:1.
  - It covers the warning and destructive tokens.
- [x] The `desktop-mica` rules work in both themes.
- [x] `lib/theme.ts` provides `getThemePref`, `setThemePref` and
  `useTheme()`. It follows OS changes live while the setting is
  `"system"`.
- [x] `theme-color` and `color-scheme` follow the active theme.
- [x] `SettingsAppearance` offers a Light / Dark / System
  SegmentedControl. The orchestrator wires it into the Settings nav
  after T3.
- [x] The Wails window background and title bar follow the theme where
  Wails supports it.
- [x] Any component using colors that break in dark is reported, then
  fixed during integration.

## Acceptance

1. Every checklist item is done.
2. `cd web && bunx tsc -b && bunx oxlint && bun run build` passes, with
   no chunk-size warning.
3. `task test` and `task lint` pass.
4. Smoke test against `catchm serve`:
   - A deep link and reload restore the view.
   - Back and Forward work.
   - The dark and light themes both render (checked via emulated media
     and the setting).
   - The server-URL confirm appears only when `account_stored` is true.

## Results (2026-09-28)

All four tracks are done.

### Integration fixes
- **Appearance in the Settings nav:** added the Appearance section.
- **Delete replaces history:** delete now replaces the history entry
  (`onDeleted`), so Back can't land on the deleted meeting.
- **Dark-theme color fixes:**
  - `text-muted-foreground/70` is replaced by `text-muted-foreground`
    in the live views.
  - `dark:bg-black/50` is added to the dialog overlays.
  - `dark:ring-foreground/15` is added on card, popover, select and
    dialog edges.
  - The active tab uses `bg-card` in dark.
- **Wails theme typings:** the theme and background method types moved
  into `WailsWindowRuntime`.

### Two older update bugs fixed in T2
- **Download timeout:** the 30s client timeout used to cap the whole
  installer download. Now the manifest keeps a 30s deadline, asset
  headers get 30s, and an asset fails only if no bytes arrive for 60s.
  The download is still cancellable.
- **Leftover installers:** an installer that was declined or replaced
  is deleted. On startup, stale `catchm-update-*.exe` files in the temp
  dir are swept, except the currently verified one.

### Verification
- `bunx tsc -b` passes.
- `bun run build` passes with no chunk warning. The main chunk is
  371.9 kB, down from 515 kB.
- `bunx oxlint` shows only the 3 existing `ui/` warnings.
- `task test` and `task lint` pass.
- Smoke test against `catchm serve`:
  - The deep link `#/meetings/{id}?tab=transcript` opens that tab after
    a reload.
  - Back and Forward work between settings and meeting detail.
  - After a delete, Back doesn't return to the deleted meeting.
  - System follows the OS (the test browser was set to dark).
  - Light and Dark switch, and `theme-color` updates with them.
  - The dark dialog overlay is visible.
  - With `account_stored: false`, the server URL saves with no dialog.

### Needs Windows desktop verification
- Title bar and background follow the theme. There may be a brief cream
  flash at startup when the stored setting is dark.
- Cancelling an update download, a slow download past 30s, and the
  stale-installer sweep after "Not now" plus a restart.
- Toast and tray navigation, including a cold start that opens a
  meeting.
- Desktop-only settings sections load correctly from the URL.

### Open decision
- **Mica:** real Mica would need `WebviewIsTransparent: true`. The user
  decided on 2026-09-28 to leave it off, so there is no change.

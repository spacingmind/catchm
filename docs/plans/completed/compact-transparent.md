# Transparent background for compact mode (macOS)

- Status: done — shipped in v0.5.0. Transparent and subtitle modes tested by the owner on macOS, Windows 11 see-through tested by the owner (2026-11-27).

## Problem

The compact overlay (480×260, always on top) has a solid
`bg-background`, so it hides whatever it floats over (the video call,
slides). The user wants the overlay to show only the text, like
subtitles over the content.

## Decisions (user, 2026-10-05)

- Only **compact mode** goes transparent. The full window keeps its
  solid background.
- Look: **no background**. Text is white with a black outline
  (subtitle style), readable on light and dark content.
- Toggle: a button **in the compact overlay**, remembered on this
  device (localStorage, like the theme).
- **macOS first** (proven on this Mac, see Findings). Windows 11
  22H2+ is implemented-untested: the user accepted it without a real
  machine (decision, 2026-10-05) — DWMSBT_NONE over the already
  NOREDIRECTIONBITMAP window shows the desktop through the transparent
  webview. Windows < 22621 (and Linux) stay unsupported: the button is
  hidden (transparentSupported() false).

## Findings (experiments on Apple Silicon, 2026-10-05)

- `Mac.WebviewIsTransparent` + a background colour with alpha 0 + CSS
  `transparent` everywhere still showed the cream background. Wails
  v2.16 never calls `setOpaque:NO` (grep `internal/frontend/desktop/
  darwin`), so the NSWindow stays opaque and macOS ignores the colour's
  alpha.
- A cgo/Objective-C shim doing `[w setOpaque:NO]; [w setBackgroundColor:
  [NSColor clearColor]]` on the main queue made the window see-through.
- The native title bar stays (Wails v2 can't drop it at runtime).
  Click-through to apps behind is not possible in Wails v2. Both are
  out of scope.

## Design

### Go (`cmd/catchm-desktop`)

- `Mac.WebviewIsTransparent: true` in `options.App`. With the window
  opaque this changes nothing outside compact mode: the page paints
  its own background.
- `transparent_darwin.go` (cgo, Cocoa): `setWindowTransparent(on bool)`,
  dispatched to the main queue. On: `setOpaque:NO` + `clearColor`. Off:
  `setOpaque:YES` (the page then re-syncs the window colour via
  `lib/theme.ts`).
- `transparent_other.go`: no-op; `transparentSupported() bool` is false.
- Bound methods: `DesktopBridge.SetCompactTransparent(on bool)` and
  `DesktopBridge.CompactTransparentSupported() bool`.

### Web

- `lib/wailsRuntime.ts`: typings + wrappers for the two methods.
- `useCompactMode`: on exit, always turn transparency off, then re-run
  the theme's window-colour sync, so the full window can never be left
  transparent.
- `CompactOverlay`: a toggle button (icon, `aria-pressed`, translated
  label/title) shown only when supported; the preference goes in
  localStorage (`catchm.compact.transparent`). While on (and in
  compact mode): call `SetCompactTransparent(true)`; add a class on
  `<html>` that makes html/body/root and the overlay's surfaces
  transparent and gives text white fill + a black outline (multi-
  direction `text-shadow`; `-webkit-text-stroke` thins glyphs).
  Muted/coloured text (status, warnings) keeps its hue but gets the
  same outline. The level meter and icons stay visible.
- i18n keys under `compactOverlay` (en + vi).

## Subtitle mode (user, 2026-10-05; split into an independent option)

The subtitle LAYOUT is its own toggle (Captions/List icon in the strip),
available on every platform (no `supported` gate), stored per device as
`catchm.compact.layout` ("list" default). The transparency toggle keeps
only the see-through look plus hiding the status strip and error line
until hover/focus-within:
- **Only the latest 1–2 lines**, bottom-anchored and centred, at a larger
  size (about 1.25× the overlay's font size). The side-by-side view mode
  renders stacked here, since two columns don't read as subtitles.
- **Fade on silence:** the lines fade out after about 5 s with no new
  utterance, and come back on the next one (no transition under
  prefers-reduced-motion).
- **The status strip (and the error line) show only on hover**, or while
  focus is inside the overlay, so keyboard users can still reach the
  toggle and the exit button.
- The user chose not to keep an always-visible recording dot or error
  line. While the strip is hidden, a capture failure shows only on hover.

## Out of scope

- Windows (and Linux) transparency; click-through; hiding the title bar.

## Acceptance criteria

- macOS: in compact mode, toggling on shows the content behind the
  overlay through everything but the title bar. Text is white with a
  black outline, readable over a white page and a dark video. Toggling
  off restores the solid background. Leaving compact mode always
  restores an opaque full window. The preference survives restarts.
- Windows: the toggle isn't shown and behaviour is unchanged.
- `go test ./...`, vet, gofmt, tsc/build/lint green. Built and checked
  by hand on a Mac.

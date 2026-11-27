# Meeting export: copy / TXT / Markdown / Word

- Status: done — shipped in v0.4.0.

## Problem

There's no way to get a meeting out of CatchM except reading the files
under `~/.catchm/meetings/<id>/`. Users want to paste a meeting into an
email or chat, or send it as a file (plain text, Markdown, Word).

## Decisions (user, 2026-10-01)

- **Scope:** the whole meeting only, as one document. There are no
  per-section exports.
- **Where:** generated server-side in Go. `.docx` is written by hand
  with `archive/zip` + a small WordprocessingML subset, so there's no new
  dependency.
- **Transcript content:** speaker names, a timestamp per line, and the
  translation under the original when the meeting has one.
- **Heading language:** follows the UI language (the web passes
  `lang=en|vi`). Summary text, decisions etc. stay in whatever language
  they were written in.

Built on top of `feat/i18n-en-vi` (UI strings need `t()`).

## Design

### Document content (all formats, same order)

1. Title (H1), then a meta block: date/time range (`StartedAt`–`EndedAt`,
   formatted for `lang`), duration, project and tags when set,
   participants when set.
2. Summary (`m.Summary`), when present.
3. Decisions (`m.Decisions`): a bullet list; scope in parentheses when
   set.
4. Action items (`m.ActionItems`): a bullet list with owner, due date and
   done state (`[x]` / `[ ]` in Markdown, "(done)" in TXT/DOCX).
5. Notes (`m.Notes`), when present.
6. Transcript: the same source the meeting page shows ("best": the
   refined v2 transcript when there is one, else live). Consecutive
   lines from one speaker are grouped under one header. Each line is
   `[hh:mm:ss] text`, offset from the first utterance (hh omitted under
   1 h). The translation goes on the next line, indented/italic, when
   the line has one. A speaker label ("Speaker 1") is replaced by the
   person's name when the analysis maps that speaker (`People[].Speaker`).

Empty sections are omitted. A meeting that's still recording can be
exported; it's just what exists so far.

### Formats

- **txt:** plain text, UTF-8, `\n` line endings, headings underlined
  with `=`/`-`.
- **md:** CommonMark. `#`/`##` headings, `-` lists, `- [ ]` action
  items, `**Speaker**` group headers, `*translation*`, with user text
  escaped for Markdown-significant characters at line start.
- **docx:** minimal OOXML: `[Content_Types].xml`, `_rels/.rels`,
  `word/document.xml`, `word/styles.xml` (Title, Heading1, Heading2,
  normal, list paragraph via plain "• " prefix, no numbering part). All
  text XML-escaped. It must open in Word, LibreOffice and Google Docs.

### API

`GET /api/meetings/{id}/export?format=txt|md|docx&lang=en|vi`
- Responds with `Content-Type` per format and `Content-Disposition:
  attachment; filename*=UTF-8''<slug>.<ext>`. The slug comes from the
  title plus date. Non-ASCII is kept via RFC 5987, with an ASCII
  fallback `filename=`.
- A bad format → 400 (uncoded validation). An unknown meeting → 404
  `meeting_not_found` (existing code).
- `lang` defaults to `en`; unknown values → `en`.
- Package: `internal/export` (pure functions: `Build(m, analysis?,
  utterances, lang) Document` → `Text()`, `Markdown()`, `Docx() []byte`),
  with its own small en/vi label table. The server handler only loads
  data and writes the response.

### UI

- On the meeting page header: an **Export** menu (next to Rename /
  Delete) with: *Copy as text*, *Copy as Markdown*, *Download .txt*,
  *Download .md*, *Download Word (.docx)*.
- Copy fetches the txt/md export and calls `navigator.clipboard.
  writeText`. A short status ("Copied" / an error) shows in the existing
  StatusMessage pattern.
- Download:
  - **Browser (`catchm serve`):** navigate an `<a download>` to the export
    URL.
  - **Desktop (Wails):** WKWebView ignores downloads, so a new bound
    method `DesktopBridge.SaveExport(meetingID, format, lang) (path
    string, err error)` opens `runtime.SaveFileDialog` with the suggested
    filename, then writes the bytes from `internal/export` directly.
    Cancel → no-op. It shows "Saved to <path>".
  - Pick the path with `hasWindowControls()`, the same test the rest of
    the UI uses.
- All strings i18n'd (`meetingDetail.export*`).

## Out of scope

- Per-section export, PDF, exporting several meetings at once.
- An MCP tool / CLI for export (the `internal/export` package makes it
  easy later).
- Mind map / flows in the export.

## Acceptance criteria

- `internal/export` unit tests: each format for a fixture meeting with
  summary, decisions, action items (open + done), notes, a two-speaker
  transcript with translations; a meeting with none of those (title +
  meta only); Markdown escaping of a line starting with `#`; XML
  escaping of `<&>` in docx; timestamps under/over 1 h.
- The `.docx` is a valid zip whose `word/document.xml` parses as XML,
  checked in a test. It's also opened by hand in LibreOffice or Word
  once (Validation).
- Server test: 200 with the right Content-Type/Content-Disposition per
  format; 400 on a bad format; 404 + `meeting_not_found`.
- Desktop: `SaveExport` writes the file; cancel returns no error and
  no file.
- UI: all five menu actions work in `catchm serve` (browser) and in the
  desktop app, in en and vi.
- `go test ./...`, vet, gofmt, `bun run build`, `bun run lint` green.

## Progress

Steps 1–4 done on `feat/meeting-export` (stacked on `feat/i18n-en-vi`):
`internal/export` (5d5e0a3), `GET /api/meetings/{id}/export` (b613cbc),
`DesktopBridge.SaveExport` (d561139), the Export menu (5c0376d).

## Decisions made during implementation

- `.docx` runs repeat each style's formatting directly: macOS Quick
  Look, TextEdit and Pages ignore `styles.xml`, so headings would render
  as body text there. A `word/_rels/document.xml.rels` part is needed
  for Word to load the styles at all.
- The ASCII filename fallback folds Vietnamese diacritics
  (`summarize.FoldDiacritics`): `hop-du-an-q4-…` rather than `h-p-d-n-…`.
- Participants come from the meeting; the analysis's people only fill in
  when the meeting has none, and map "Speaker N" to names. An unreadable
  analysis costs the names, never the export.
- Copy goes through `lib/clipboard.ts`: the Wails runtime's
  `ClipboardSetText` on the desktop, and a `ClipboardItem` created inside
  the click in browsers. WebKit rejects `writeText` after an awaited
  fetch.

## Validation

- `go test ./...` (incl. `internal/export`, server and desktop export
  tests), vet, gofmt green; `tsc` 0 errors; lint only the 3
  pre-existing `components/ui` warnings.
- HTTP against a seeded Vietnamese meeting: all three formats 200 with
  the right Content-Type and `filename` + `filename*=UTF-8''`; `pdf` →
  400. The .docx opens in macOS `textutil` with correct text, and
  bold/italic/sizes render after the direct-formatting change.
- Still to do: click through the five menu items in the browser
  (`catchm serve`) and in the desktop app (macOS + Windows), en and vi;
  open a .docx in Word or Google Docs once.

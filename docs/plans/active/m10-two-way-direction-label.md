# Plan: M10 — per-utterance translation direction label

- Status: active
- Created: 2026-09-20

## Goal

`LiveMeeting.tsx` already renders `u.translated` as a secondary line
under each utterance, but in a two-way meeting (where the translated-to
language varies per utterance depending on who's speaking) there's no
indication of *which* language that secondary line is in. Add a small
"→ {lang}" label, but only when we actually know the language —
`Utterance.lang` is deliberately left empty for a two-way meeting on a
natively-translating engine (Soniox), a decision recorded in
`docs/plans/completed/m9-two-way-translation.md`'s pipeline section,
because the backend has no reliable per-utterance signal for which of
the two configured languages a given utterance ended up in. Do not
invent one client-side — an honest "no label" beats a guessed-and-
possibly-wrong one.

## Scope

- `web/src/components/LiveMeeting.tsx`: next to the existing
  `{u.translated && <p>...}` secondary line, when `u.lang` is
  non-empty, prepend a small muted label like `→ vi` (a `<span>` with
  slightly lighter/smaller styling than the translated text itself,
  not a full sentence — this is a quiet annotation, not new copy to
  translate/localize). When `u.lang` is empty (the Soniox two-way
  case), render the translated line exactly as it does today, with no
  label — don't fall back to guessing the language from `meeting.lang`/
  `translate_to`.
- Check whether `Utterance`'s TypeScript type in `web/src/lib/api.ts`
  already exposes `lang` (it should, since the Go struct has had the
  field since M2/M3) — if it's missing from the TS type, add it.
- `web/src/components/MeetingDetail.tsx`: the ended-meeting transcript
  view (if it renders utterances/translations similarly — check first)
  should get the same treatment for consistency, only if it already
  displays per-utterance translated text; don't add a transcript view
  to MeetingDetail if it doesn't already have one, that's out of scope.

Out of scope: reconstructing/guessing a language label when `lang` is
empty, any change to backend behavior or the `Utterance`/`Meeting` Go
structs, editing historical utterances, i18n/localization of the label
text itself.

## Acceptance criteria

1. `task web:install && task web:build` succeeds, no TypeScript errors.
2. Manual/browser verification: seed a two-way meeting's transcript
   with a mix of utterances that have `lang` set (e.g. via a whisper-
   engine two-way scenario) and utterances with empty `lang` (Soniox
   two-way style), confirm the label appears only where `lang` is
   present and never shows an incorrect/guessed value.
3. A one-way (non-two-way) meeting's translated utterances are
   unaffected in appearance if `lang` happens to be set the same way it
   already is today (i.e. this change must not alter one-way rendering
   at all beyond adding the label if `lang` is present there too — one-
   way meetings already set `Lang` to the target language today, so
   they'll also start showing the label; confirm this reads sensibly,
   e.g. a one-way en→vi meeting's utterances all show "→ vi", which is
   correct and consistent, not a regression).

## Decisions

- Label rendering: the translated `<p>` now wraps its text in the same
  muted paragraph as before, with the label as an inline `<span
  className="mr-1 text-xs text-muted-foreground/70">→ {u.lang}</span>`
  immediately before the translated text — inline on the same line,
  one Tailwind size step smaller and one opacity step lighter than the
  `text-sm text-muted-foreground` translated text itself. Kept as an
  arrow + code, not a sentence, per spec.
- No-guess rule honored: when `u.lang` is empty (Soniox two-way), the
  span simply doesn't render; nothing reads `meeting.lang` or
  `translate_to` as a fallback.
- `Utterance` TS type in `web/src/lib/api.ts` already had `lang?:
  string` — verified, no change needed there.
- MeetingDetail: N/A — it renders only meeting-level cards (summary,
  participants, decisions, action items, notes) and never fetches or
  displays the transcript, so there is no per-utterance translated
  line to label. Per spec, adding a transcript view there is out of
  scope.

## Progress

- [x] Utterance TS type has `lang` field (already present, verified)
- [x] LiveMeeting.tsx direction label (lang-present only)
- [x] MeetingDetail.tsx same treatment if applicable (N/A — no
      transcript view exists there)

## Validation

- Criterion 1 confirmed: `bun run build` (what `task web:build` runs,
  from `web/`) completes cleanly — `tsc -b` + `vite build`, 2004
  modules, zero TypeScript errors. `task web:install` itself was not
  re-run since `node_modules` was already present and the build proved
  it sufficient.
- Criteria 2 and 3 (browser verification with seeded two-way/one-way
  transcripts) not performed in this run — no running backend in this
  sandbox session. The rendering change is a single conditional span;
  by construction it cannot alter the translated line's content or
  appearance when `lang` is empty, and one-way meetings with `Lang`
  set will show "→ vi" style labels, which is the correct reading per
  the spec's own criterion-3 analysis. Remaining: a quick human spot
  check in the browser next time a live meeting is running.

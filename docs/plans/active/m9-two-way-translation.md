# Plan: M9 — two-way (bidirectional) live translation

- Status: active
- Created: 2026-09-18

## Goal

Today a meeting has one `TranslateTo` language: whatever is spoken gets
translated *to* that one language, one direction only. Two-way mode is
for a bilingual conversation between exactly two known languages (e.g.
speaker A in English, speaker B in Vietnamese): each utterance should be
translated *into whichever of the two languages it wasn't spoken in* —
auto-detected per utterance, not fixed per speaker.

This also fixes a real, pre-existing correctness gap found during
research for this milestone: `internal/stt/engine_soniox.go`'s
`Stream()` never sends Soniox's required initial JSON config message at
all (documented as a guess in M2's Decisions, never verified against
the real API) — it just starts writing binary PCM frames straight after
the WebSocket handshake. Soniox's real protocol requires a first
text-frame JSON config message. Fix this as part of this milestone since
two-way mode is configured in exactly that message.

## Confirmed real API shape (from Soniox's actual docs — not a guess)

The first message sent over the WebSocket after connecting must be JSON
text (not binary):

```json
{
  "api_key": "...",
  "model": "stt-rt-v4",
  "audio_format": "auto",
  "enable_language_identification": true,
  "translation": {
    "type": "two_way",
    "language_a": "en",
    "language_b": "vi"
  }
}
```

Binary PCM frames follow after that. Server messages carry per-token
`language` and `translation_status` fields alongside `text`; the
existing `sonioxMessage` struct in `engine_soniox.go` (`Text`, `Speaker`,
`Translation`, `IsFinal`) is a simplification that doesn't yet parse
`language`/`translation_status` — extend it to match the real message
shape enough to populate `Result.Text`/`Result.Translated`/`Result.Speaker`
correctly for two-way mode (read Soniox's docs shape for the exact
per-token field names if you have any way to check them again; if not,
keep the current flattened-message assumption but add the `translation`
config to the initial handshake, since that's the part proven wrong and
untested — don't rewrite more of the message-parsing than you can
verify, record what's still a guess vs what's now confirmed).

## Scope

- `internal/stt/stt.go`: extend `Options` with `LanguageB string` and
  `TwoWay bool`. When `TwoWay` is true, `TranslateTo` is not used (two
  languages are `Lang` and `LanguageB` instead); when false, behavior is
  exactly as before (this must not regress M2/M3's existing one-way
  tests).
- `internal/stt/engine_soniox.go`:
  1. Send the JSON config message (per the confirmed shape above) as the
     first WebSocket text frame in `Stream()`, before any PCM is
     written — include `api_key`, a `translation` object that is either
     `{"type": "one_way", "target_language": opts.TranslateTo}` (if
     Soniox's one-way shape differs from this guess, use your best
     verified understanding, and say in Decisions if this part is still
     unverified) when `!opts.TwoWay`, or `{"type": "two_way",
     "language_a": opts.Lang, "language_b": opts.LanguageB}` when
     `opts.TwoWay`.
  2. Update `TestSonioxSpeakerAndTranslation` (existing test) and add a
     new `TestSonioxSendsConfigMessage` / `TestSonioxTwoWayConfig` —
     mock WS server asserts the *first* frame received is JSON text
     matching the expected config shape (specifically the `translation`
     object), *then* reads subsequent binary PCM frames. This is a
     protocol-shape test, not a "does translation quality work" test —
     keep it hermetic exactly like the existing Soniox test.
- `internal/stt/engine_whisper.go` (no native translation): add a
  `internal/translate`-based two-way path. Since whisper has no built-in
  translation, direction must be inferred from the *transcribed text* of
  each utterance:
  - Add a tiny local language-identification helper (new file, e.g.
    `internal/translate/detect.go`) — given text and exactly two
    candidate language codes, decide which one it's more likely to be.
    Do **not** pull in a new heavy dependency (no fasttext model, no
    external service) — the simplest defensible approach for two known
    languages is calling the same LLM you already use for translation
    with a tiny classification prompt ("Respond with only 'A' or 'B':
    is this text in language {A} or language {B}? Text: ...") via the
    existing `Translator` interface's underlying HTTP client pattern
    (or add a small sibling method), OR (simpler, no extra LLM round
    trip) a lightweight heuristic using each language's characteristic
    Unicode ranges/diacritics/stopwords if the two configured languages
    are distinguishable that way (e.g. Vietnamese's dense diacritics vs
    English ASCII) — pick whichever is simpler to implement correctly
    and test hermetically, and record the tradeoff in Decisions. This
    does not need to be perfect; document it as a best-effort heuristic,
    not a robust NLP feature.
  - Wire this into `internal/pipeline`: when `opts.TwoWay` is set and
    the engine doesn't natively support translation
    (`!engine.SupportsTranslation()`), detect the utterance's language
    from `{Lang, LanguageB}`, translate to the *other* one via the
    existing `translate.Translator`, same only-fill-if-empty pattern
    already used for one-way translation in M3's pipeline wiring — don't
    duplicate that logic, extend it.
- `internal/meeting`: no struct change needed if `LanguageB`/`TwoWay`
  just live in `StartOptions` and get folded into the existing
  `Lang`/`TranslateTo` frontmatter fields somehow — decide the simplest
  representation (e.g. store `lang: en`, and when two-way, store
  `translate_to: vi` too, plus a new `two_way: true` bool field on
  `Meeting` — pick whichever avoids inventing parallel storage for the
  same two language codes, record the choice).
- `internal/server`: extend `POST /api/meetings`'s request body with an
  optional `two_way: bool` and reuse `translate_to` as "language B" when
  `two_way` is true (don't add a redundant third field if
  `lang`+`translate_to` already carry both codes cleanly — your call,
  record it).
- Web: `NewMeetingDialog.tsx` gets a "Two-way translation" checkbox;
  when checked, relabel "Translate to" as "Language B" (both `lang` and
  the relabeled field become required together in this mode — simple
  client-side validation, no new component). `LiveMeeting.tsx` already
  renders `translated` as a secondary line per utterance — confirm this
  reads correctly for a two-way meeting where the "translated" language
  varies utterance-to-utterance (it should, since each utterance's
  `Translated` field is independently populated); if a small label like
  "→ vi" or "→ en" per utterance would remove ambiguity about which
  direction that particular line went, add it (small, optional touch).

Out of scope: more than two languages, TTS/voice output, automatic
speaker-to-language binding (a specific person always speaking language
A) — detection is per-utterance from content, not per-speaker identity;
Soniox billing/cost implications are the user's concern, not this
milestone's.

## Acceptance criteria

1. `task build && task test && task lint` pass. Existing one-way
   Soniox/whisper/pipeline tests from M2/M3 still pass unmodified in
   behavior (only the Soniox config-message addition changes wire
   behavior, and it's additive — a message that was never sent before,
   not a removal).
2. `TestSonioxTwoWayConfig` — mock WS server captures the first frame,
   asserts it's valid JSON with `translation.type == "two_way"` and the
   right language codes, before any binary frame arrives.
3. `TestSonioxOneWayConfigStillSent` — same idea for the non-two-way
   path, proving the fix doesn't only work in two-way mode.
4. `TestDetectLanguageBetweenTwoOptions` (internal/translate or
   wherever the helper lives) — given clearly-English and
   clearly-Vietnamese sample strings and the pair `("en","vi")`, the
   detector picks correctly for both. Use obviously unambiguous samples
   (don't test edge cases you can't reliably classify with a
   best-effort heuristic).
5. `TestPipelineTwoWayTranslatesBothDirections` (internal/pipeline) —
   fake whisper engine emits one utterance in each of two languages;
   fake translator/detector wired in; both utterances end up with
   `Translated` set to the *other* language's slot, not the same
   direction twice.
6. `TestTwoWayMeetingOptionRecorded` — a meeting started with
   `two_way: true` persists whatever representation you chose and it
   round-trips through `Get`.

## Decisions

(fill in: exact Soniox one-way config shape used and confidence level,
frontmatter representation chosen for two-way, language-detection
approach chosen (LLM-based vs heuristic) and why, whether per-utterance
direction labels were added to LiveMeeting.)

## Progress

- [ ] Soniox: send initial JSON config message (one-way and two-way)
- [ ] stt.Options: TwoWay + LanguageB
- [ ] Language detection helper for whisper path
- [ ] Pipeline wiring for two-way translation
- [ ] Meeting frontmatter representation + API + NewMeetingDialog UI
- [ ] tests

## Validation

(to be filled as criteria are confirmed)

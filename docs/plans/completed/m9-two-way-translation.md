# Plan: M9 — two-way (bidirectional) live translation

- Status: complete
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

- **Soniox config shape.** The overall first-frame shape (api_key /
  model "stt-rt-v4" / audio_format "auto" /
  enable_language_identification true / translation object) and the
  two_way translation object are confirmed from Soniox's docs. The
  one_way translation object used is `{"type": "one_way",
  "target_language": opts.TranslateTo}` — **still a best guess** (the
  docs only showed two_way), marked as such in a comment on
  `sonioxConfig`. Confidence: high for two_way, low-medium for one_way
  field names. When no translation is requested at all, the
  `translation` key is omitted rather than sent empty. Server-message
  parsing was left on the existing flattened `sonioxMessage` assumption
  (the confirmed per-token `language`/`translation_status` shape was
  NOT adopted — that remains a documented simplification, since
  Soniox's real per-token fields couldn't be re-verified hermetically).
- **Frontmatter representation.** A single new `two_way: true` bool on
  `Meeting` (+ `StartOptions.TwoWay`), reusing the existing `lang` /
  `translate_to` fields as language A / language B in two-way mode — no
  parallel storage invented. The API likewise reuses `translate_to` as
  language B when `two_way: true` (no third language field);
  `startPipeline` was changed to take the full `stt.Options` and maps
  `LanguageB = translate_to` there.
- **Language detection: heuristic, not LLM.** `translate.DetectLanguage`
  (internal/translate/detect.go) counts runes that match one candidate's
  script but not the other's (Vietnamese diacritics vs English ASCII)
  and tie-breaks toward the more constrained script. Chosen over an LLM
  classification call because it adds no extra round trip per utterance,
  stays fully hermetic to test, and the primary pair (en/vi) is exactly
  the case diacritics distinguish trivially. Documented as best-effort:
  unknown language codes degrade to "first candidate" rather than
  failing, and the scripts table only lists languages the heuristic can
  actually distinguish.
- **UI.** NewMeetingDialog adds a "Two-way translation" checkbox; when
  checked, "Source language"/"Translate to" relabel to required
  "Language A"/"Language B" (HTML required + a client-side guard that
  shows the shared error line). LiveMeeting was reviewed and left as-is:
  each utterance's `Translated` line is populated independently, which
  reads correctly when the direction varies per utterance; no
  per-utterance "→ xx" labels were added (each utterance already carries
  its own `lang` field, and the labels felt like clutter for the small
  ambiguity they'd remove).

## Progress

- [x] Soniox: send initial JSON config message (one-way and two-way)
- [x] stt.Options: TwoWay + LanguageB
- [x] Language detection helper for whisper path
- [x] Pipeline wiring for two-way translation
- [x] Meeting frontmatter representation + API + NewMeetingDialog UI
- [x] tests

## Validation

All acceptance criteria confirmed on 2026-09-18:

1. `go build ./... && go vet ./... && gofmt -l .` and
   `go test ./... -count=1` all pass, including the pre-existing M2/M3
   one-way tests (`TestSonioxSpeakerAndTranslation` was extended to
   read/assert the new config frame first — the only existing-test
   change, and additive on the wire). Independently re-verified by the
   reviewer: full Go test suite green, including the Soniox
   config-frame tests.
2. `TestSonioxTwoWayConfig` green — first frame is JSON text with
   `translation.type == "two_way"`, `language_a == "en"`,
   `language_b == "vi"`, ahead of any binary frame.
3. `TestSonioxOneWayConfigStillSent` green — one-way sessions also open
   with the JSON config frame (`type == "one_way"`,
   `target_language == "es"`).
4. `TestDetectLanguageBetweenTwoOptions` green — clearly-English and
   clearly-Vietnamese samples both attributed correctly in both argument
   orders.
5. `TestPipelineTwoWayTranslatesBothDirections` green — an English and
   a Vietnamese utterance each end up translated into the OTHER
   language (asserted via a direction-recording fake translator).
6. `TestTwoWayMeetingOptionRecorded` green — `two_way: true` with
   `lang: en` / `translate_to: vi` round-trips through `Get`, both
   parsed and in the raw meeting.md.
7. Web build green (`task web:install && task web:build`). Independently
   verified by the reviewer against the real binary via
   headless-browser screenshots of NewMeetingDialog: before checking
   "Two-way translation" it shows optional "Source language" /
   "Translate to"; after checking it, the fields relabel to required
   "Language A" / "Language B" with the right placeholders.

## Post-landing follow-up: Soniox protocol deep-dive (2026-09-18)

The one-way config shape and the response-message shape were both
flagged above as unverified guesses. A deep-research pass against
Soniox's actual docs (`api-reference/stt/websocket-api`,
`translation/stt-translation`, `sdk/node-SDK/reference/types`) resolved
both, and two real correctness bugs were found and fixed on top of the
work above:

- **One-way shape confirmed correct as guessed**:
  `{"type": "one_way", "target_language": "..."}` — no code change
  needed here, just confidence upgraded from "guess" to "confirmed".
- **Bug found and fixed: `audio_format`**. The config sent `"auto"`,
  but the pipeline streams raw headerless 16-bit LE mono PCM — Soniox
  requires the explicit format `"pcm_s16le"` plus `sample_rate: 16000`
  and `num_channels: 1` alongside it for any raw PCM format (these two
  fields were missing entirely). Without this fix a real Soniox
  connection would likely have rejected the session or misinterpreted
  the audio. Fixed in `engine_soniox.go`, covered by extending
  `TestSonioxTwoWayConfig`'s assertions.
- **Bug found and fixed: response message shape**. The implementation
  assumed a flat `{text, speaker, translation, is_final}` object per
  server message. Soniox actually sends `{"tokens": [...]}`, an array
  of per-token objects each carrying `text`, `is_final`, `speaker`,
  `translation_status` ("original"/"translation"/"none"), and
  `language`. Rewrote `sonioxMessage`/added `sonioxToken` to match, and
  added `buildResult()` to reduce one message's tokens into a `Result`
  (original-status tokens concatenate into `Text`, translation-status
  tokens into `Translated`, `Final` requires every token in the batch
  finalized). Without this fix, parsing every real Soniox response
  would have silently failed (`json.Unmarshal` into the old flat struct
  wouldn't error on the mismatched shape, it would just leave `Text`
  empty on every message) — the engine would have looked like it
  connected successfully but produced no transcript at all. Covered by
  a new `TestBuildResultGroupsTokensByTranslationStatus` unit test plus
  the updated integration-style test's fixture.
- **Still not fully resolved**: whether Soniox real-time STT expects
  `api_key` in the JSON config body (what's implemented) versus an
  `Authorization` header (what's also sent, redundantly, for the HTTP
  upgrade request) — the docs describe both patterns in different
  places and the research pass couldn't disambiguate which applies to
  this specific endpoint. Both are currently sent, which is harmless if
  one is simply ignored; this is the one remaining thing to confirm
  with a real API key before relying on Soniox in production.

No real Soniox API key was available to run an actual live smoke test;
this fix is desk-verified against documentation and covered by
hermetic mock-WebSocket tests only. Recommend an actual live smoke test
(one real session, one short utterance) the first time this ships to a
user with a real Soniox account.

## Cross-check against a real production client (2026-09-18)

Cloned `phuc-nt/my-translator` (the reference app this whole project
took inspiration from, per `docs/research-stt-translate.md`) and read
its working `src/js/soniox.js` client to validate the desk research
above against actual production code, not just docs. It confirmed the
`pcm_s16le`/`sample_rate`/`num_channels` fix and both translation
shapes, and also surfaced three more gaps, all fixed:

- **Bug: diarization never enabled.** `SonioxEngine.SupportsDiarization()`
  returns `true`, but the config never sent
  `enable_speaker_diarization` — without it Soniox never returns
  speaker labels at all, regardless of what the capability method
  claims. Added `EnableSpeakerDiarization: true` to `sonioxConfig`.
- **Model version.** Switched `stt-rt-v4` → `stt-rt-v5`, matching what
  the working reference client currently uses (v4 was an unverified
  guess; v5 is now backed by a real, running production app rather
  than upgraded confidence on a guess).
- **Bug: response parsing dropped finalized text whenever a message
  mixed finalized and provisional tokens.** The reference client
  builds separate `originalText`/`provisionalText`/`translationText`
  buffers per message and only keeps `is_final` tokens in the ones it
  emits — a single message legitimately mixes both. The previous
  `buildResult` required *every* token in a message to be final before
  emitting anything, which would have silently discarded already-final
  text sitting alongside a still-provisional token in the same batch.
  Rewrote `buildResult` to drop non-final tokens individually (not the
  whole batch) and to skip the reference client's `"<end>"` end-of-turn
  sentinel token, which was not previously handled and would otherwise
  have leaked the literal string `"<end>"` into transcripts. Speaker is
  now only captured from `translation_status == "original"` tokens,
  matching the reference client exactly (previously any non-empty
  `Speaker` field counted, which the real API's `translation_status:
  "translation"` tokens might also carry echoed from the source).
- **Also confirmed**: the reference client authenticates purely via
  `api_key` inside the JSON config body and sends no `Authorization`
  header at all on the WebSocket handshake — resolving the
  previously-open question about header vs. body placement in favor of
  body-only. `engine_soniox.go` still sends both (the header is
  harmless if Soniox ignores it), but body-only is now the
  higher-confidence, production-verified pattern.

Three new tests added: `TestBuildResultKeepsFinalizedTextWhenBatchMixesProvisional`,
`TestBuildResultSkipsEndSentinel`, plus `TestSonioxTwoWayConfig` extended
to assert `enable_speaker_diarization`/`stt-rt-v5`. Still no real API
key available — this cross-check raises confidence well above "docs
only" but a live smoke test remains the last step before production
use.

## Live e2e smoke test (2026-09-19) — real Soniox account, real speech audio

Ran the actual production code path (`internal/pipeline.Start` +
`internal/stt.SonioxEngine`, not a mock) against a real Soniox account
with real English speech audio (Windows SAPI TTS, converted to 16kHz
mono PCM), for both one-way (en→vi) and two-way modes. The account key
used for this test has been discarded by the user after testing.

**Found and fixed three real bugs that a live test — and only a live
test — could catch:**

1. **Wrong WebSocket URL.** `defaultSonioxURL` was
   `wss://api.soniox.com/transcribe-websocket`; the real endpoint is
   `wss://stt-rt.soniox.com/transcribe-websocket` (`api.soniox.com` is
   the REST API host, `stt-rt.soniox.com` is real-time STT,
   `tts-rt.soniox.com` is real-time TTS — three different subdomains).
   Every prior test used a mock server so this never surfaced.
2. **No real-time pacing + abrupt disconnect discarded all results.**
   `capture.FileSource` dumped an entire audio file's chunks as fast as
   the reader could consume them (no pacing), and `SonioxEngine.Stream`
   force-closed the WebSocket connection immediately after the last
   chunk was written. Combined, a real session got zero wall-clock time
   to process anything before being cut off — the very first live test
   produced 0 utterances despite a valid key and correct config. Fixed
   both: `FileSource` now paces chunk delivery to the audio's real-time
   duration (it exists specifically to "stand in for a microphone," and
   a mic naturally paces itself), and `SonioxEngine` now signals
   end-of-audio with an empty binary frame (matching a real Soniox
   client's disconnect sequence) and waits for the server to close the
   connection itself (flushing final results first) rather than forcing
   it, with a bounded grace-period fallback so `Pipeline.Stop()` can
   never hang indefinitely on an unresponsive server.
3. **Original text and its translation arrive as separate WebSocket
   messages.** Confirmed live: Soniox sends one message with only
   `translation_status: "original"` tokens, then a *separate* following
   message with only `translation_status: "translation"` tokens for the
   same utterance — not combined in one message, which is what every
   existing test's mock assumed. `Stream`'s reader loop now holds a
   finalized original result until either a following translation-only
   message arrives (merged in) or a new original message supersedes it
   (flushed untranslated, so a missing/slow translation never drops the
   transcript). Before this fix, every one-way-translated utterance
   reached the pipeline with `Translated` always empty.

**Confirmed correct as originally implemented** (no changes needed):
the `pcm_s16le`/`sample_rate`/`num_channels` config fields, the
`tokens[]` response shape and per-token field names
(`text`/`is_final`/`speaker`/`translation_status`/`language`), the
`one_way`/`two_way` translation config shapes, `enable_speaker_diarization`
actually producing speaker labels, and (from the earlier M9 fix) sending
`api_key` in the JSON config body.

**Verified live, end-to-end, with real output:**
- One-way (en→vi): 4 utterances, e.g. text `"Hello, this is a test
  meeting"` → translated `"Xin chào, đây là một cuộc họp thử nghiệm"`,
  each carrying `speaker: "1"` from diarization.
- Two-way (en/vi pair, English-only audio): same 4 utterances, correct
  translations, `Lang` left empty per the earlier native-engine fix
  (confirmed: no incorrect language stamped).

New regression test: `TestSonioxPairsSeparateOriginalAndTranslationMessages`
(hermetic, mock WS server sending the two-message shape observed live).

**Not tested live**: two-way with genuinely bilingual audio (both
speakers actually switching languages) — only English-only audio was
available for this session; the code path is the same as one-way for a
native engine (Soniox resolves direction server-side regardless), so
this is a low-risk gap, but worth a follow-up smoke test if bilingual
audio becomes available.

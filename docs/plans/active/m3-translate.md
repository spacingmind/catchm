# Plan: M3 — translation layer (TranslateGemma via Ollama)

- Status: active
- Created: 2026-09-18

## Goal

Give engines that have no native translation (whisper.cpp: neither
`StreamEngine` nor `BatchEngine` support it) a translated utterance, by
running a local LLM translation step after transcription. Soniox already
translates natively via its `TranslateTo` option — this layer must not
double-translate its output. Per `docs/research-stt-translate.md`, the
model is TranslateGemma served locally through Ollama: a small model
(4B) for live low-latency translation, a larger one (12B) for batch
refine where latency doesn't matter.

## Scope

- `internal/translate` (new package): `Translator` interface —
  `Translate(ctx context.Context, text, targetLang string) (string, error)`.
  `OllamaTranslator{Host, Model string}` implements it by POSTing to
  Ollama's `/api/generate` endpoint (`stream: false`, a prompt that
  instructs a translation-only response with no commentary) and returning
  `response.Response` trimmed. Test against a fake HTTP server
  (httptest), not a real Ollama instance.
- `internal/pipeline`: `Start` gains a `translator translate.Translator`
  parameter (nil-able — nil preserves exact M2/M2.5 behavior with no
  translation step). When translator is non-nil, `opts.TranslateTo != ""`,
  and a result's `Translated` field arrives empty (i.e. the engine didn't
  translate it itself — this is how Soniox's native translation is left
  untouched), call `translator.Translate(ctx, res.Text, opts.TranslateTo)`
  and use its result as the utterance's `Translated` field before
  `store.Append`. A translation error must not drop the utterance — log
  is out of scope for this package, so on error just append the
  utterance untranslated (`Translated: ""`).
- `internal/batch`: `Refine` gains the same `translator translate.Translator`
  parameter (nil-able, same semantics: only fills utterances whose
  `Translated` is still empty after `TranscribeFile`, never overwrites a
  non-empty one, swallows translation errors per-utterance the same way).
- `internal/config`: add a `Translate` section:
  ```yaml
  translate:
    host: http://127.0.0.1:11434   # Ollama
    live_model: translategemma:4b
    batch_model: translategemma:12b
  ```
  Model tag names are placeholders (not verified against a real Ollama
  library listing) — document this as a decision, same posture as the
  whisper.cpp HTTP protocol shape in M2.
- `internal/server`: add a swappable `newTranslator func(cfg *config.Config, model string) (translate.Translator, error)`
  field on `Server` (same seam pattern as `newEngine`/`newBatchEngine`),
  used to build a live translator (`cfg.Translate.LiveModel`) in
  `startPipeline` and a batch translator (`cfg.Translate.BatchModel`) in
  `handleRefine`, but **only when the request's `translate_to` /
  meeting's `TranslateTo` is non-empty** — no translator is constructed
  (and no Ollama dependency introduced) for meetings that don't ask for
  translation.

Out of scope: streaming/token-by-token translation, translating partial
(non-final) results, a translation cache, retry/backoff on Ollama
errors, any model other than TranslateGemma, and changing how Soniox's
own native translation works.

## Acceptance criteria

1. `task build && task test && task lint` pass; the Ollama client has a
   unit test against a fake HTTP server (no network, no real Ollama).
2. `TestOllamaTranslate` — fake server asserts the request body contains
   the target language and source text, returns a canned
   `{"response": "..."}`; the client returns that text trimmed.
3. `TestPipelineTranslatesWhenEngineLacksNativeTranslation` — fake
   whisper engine (produces `Result{Translated: ""}`) + a fake
   `Translator` wired into the pipeline: the appended utterance has
   `Translated` set from the fake translator's output.
4. `TestPipelineDoesNotDoubleTranslate` — a fake engine that already
   fills `Result.Translated` (simulating Soniox) + a fake `Translator`
   that would return an obviously different string if called: assert
   the appended utterance's `Translated` is the engine's own value, and
   optionally that the fake translator was never invoked.
5. `TestPipelineNilTranslatorUnaffected` — `Start` called with a nil
   translator behaves exactly like the existing M2
   `TestPipelineLifecycle` (no translation attempted, no panic).
6. `TestRefineTranslatesUtterances` — batch engine returns utterances
   with empty `Translated`; `Refine` with a translator fills them; a
   second case where the batch engine already filled `Translated` (e.g.
   a hypothetical future batch engine with native translation) is left
   untouched.
7. `TestPipelineTranslationErrorKeepsUtterance` — translator returns an
   error: the utterance is still appended with `Translated: ""`, no
   panic, no goroutine leak (reuse the `runtime.NumGoroutine` pattern
   from `TestPipelineLifecycle`).

## Decisions

(fill in as the implementer makes calls — expected topics: exact Ollama
request/response shape used, prompt wording, whether the live/batch
model split needs two separate `Translator` instances or one struct with
swappable model field, error-swallowing signature — return value vs
logging vs silent.)

## Progress

- [ ] internal/translate Translator interface + OllamaTranslator
- [ ] internal/pipeline translation wiring (skip when engine already translated)
- [ ] internal/batch translation wiring
- [ ] internal/config Translate section
- [ ] internal/server newTranslator seam wired into startPipeline + handleRefine
- [ ] tests

## Validation

(to be filled as criteria are confirmed)

# Plan: M3 — translation layer (TranslateGemma via Ollama)

- Status: complete
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

- **Ollama request shape**: `{"model": ..., "prompt": ..., "stream": false}`
  POSTed as JSON to `{host}/api/generate`. No other fields (no `options`,
  no `system` field) — the prompt alone carries the instruction, matching
  the "simplest reasonable call" posture used for the whisper.cpp HTTP
  shape in M2.
- **Prompt wording**: `"Translate the following text to %s. Respond with
  only the translation, no commentary or explanation.\n\n%s"`, with the
  target language and source text substituted in. Not tuned against a
  real TranslateGemma model (none was available); documented here as a
  placeholder for the same reason the model tag names are placeholders.
- **Response shape**: `{"response": "..."}`, decoded and returned via
  `strings.TrimSpace`. This is Ollama's documented non-streaming
  `/api/generate` response envelope.
- **`OllamaTranslator` shape**: a plain struct with exported `Host` and
  `Model` string fields (per the spec's literal signature), constructed
  directly with a struct literal — no `NewOllamaTranslator` constructor,
  since there's no invariant to enforce and the struct has no unexported
  state. `Translate` uses `http.DefaultClient` rather than holding a
  `*http.Client` field, since there's no keep-alive/timeout tuning need
  here unlike `WhisperEngine` (which disables keep-alives because chunks
  are frequent and short-lived — a single translation call per utterance
  doesn't have that pressure).
- **Live vs batch model split**: one `Translator` implementation
  (`OllamaTranslator`) with a `Model` field, not two types. The server
  builds two separate instances via `newTranslator(cfg, model)` — one
  with `cfg.Translate.LiveModel` in `startPipeline`, one with
  `cfg.Translate.BatchModel` in `handleRefine` — rather than one
  `Translator` juggling two models internally.
- **Error-swallowing signature**: no signature change — `Translate`
  still returns `(string, error)` like any other fallible call. Both
  `pipeline.Start`'s result-handling goroutine and `batch.Refine`'s
  per-utterance loop check the error inline and simply leave
  `Translated` as `""` on failure (discarding the error), matching the
  spec's "log is out of scope for this package" note. No retry.
- **Nil-interface trap in tests**: pipeline tests pass a `*fakeTranslator`
  through a `translate.Translator` variable that starts as a true nil
  interface (only assigned when the test's `*fakeTranslator` pointer is
  non-nil) — passing a typed-nil pointer directly would make `Start`'s
  `translator != nil` check always true. Documented as a comment at the
  call site (`internal/pipeline/pipeline_test.go`'s `startScripted`).
- **`newTranslator` construction failures are non-fatal**: if
  `s.newTranslator(...)` errors (e.g. malformed host), `startPipeline`
  and `handleRefine` both proceed with a nil translator rather than
  aborting — transcription must not fail just because translation setup
  did, consistent with the spec's "a translation error must not drop the
  utterance" intent applied one level up, and with `startPipeline`'s
  existing best-effort posture (a meeting always gets created/captured
  even if some optional piece can't start).
- **`handleRefine` translator gating**: the meeting is fetched once via
  `s.surface.GetMeeting(id)` purely to read `TranslateTo` before deciding
  whether to construct a batch translator; `batch.Refine` still does its
  own `store.Get` internally. This is a harmless duplicate read (no
  mutation, no lock) traded for keeping the "don't construct a translator
  unless requested" check outside of `batch.Refine`'s signature (which
  takes a translator, not a boolean/meeting).
- **Config defaults**: `host: http://127.0.0.1:11434`,
  `live_model: translategemma:4b`, `batch_model: translategemma:12b` —
  copied verbatim from the spec's YAML example. As the spec notes, these
  tag names are unverified against a real Ollama library listing, same
  posture as M2's whisper.cpp HTTP protocol shape.

## Progress

- [x] internal/translate Translator interface + OllamaTranslator
- [x] internal/pipeline translation wiring (skip when engine already translated)
- [x] internal/batch translation wiring
- [x] internal/config Translate section
- [x] internal/server newTranslator seam wired into startPipeline + handleRefine
- [x] tests

## Validation

- `go build ./... && go vet ./... && gofmt -l $(git ls-files '*.go') &&
  go test ./... -count=1` — clean (also verified with `-race`).
- `task build && task test && task lint` — clean.
- All 7 acceptance-criteria test scenarios implemented and passing:
  `TestOllamaTranslate` (internal/translate), plus a
  `TestOllamaTranslateServerError` companion for the non-200 path;
  `TestPipelineTranslatesWhenEngineLacksNativeTranslation`,
  `TestPipelineDoesNotDoubleTranslate`,
  `TestPipelineNilTranslatorUnaffected`,
  `TestPipelineTranslationErrorKeepsUtterance` (internal/pipeline,
  including the `runtime.NumGoroutine` leak check reused from
  `TestPipelineLifecycle`); `TestRefineTranslatesUtterances`
  (internal/batch).
- Extra coverage beyond the 7 named scenarios, exercising the
  `newTranslator` server seam directly:
  `TestStartPipelineOnlyConstructsTranslatorWhenRequested` and
  `TestHandleRefineOnlyConstructsTranslatorWhenRequested`
  (internal/server) — assert the translator factory is never invoked for
  meetings with no `translate_to`, and is invoked with the right model
  name (`LiveModel` / `BatchModel`) when one is set.
- No real network, Ollama, or whisper dependency introduced by any new
  test — all run against `httptest` fakes or in-process fakes.
- All existing M1/M2/M2.5 tests still pass unmodified in behavior; only
  call sites were updated for the new `translator` parameter
  (`pipeline.Start`, `batch.Refine`, both production call sites in
  `internal/server/server.go` and all test call sites).

## Live verification (2026-09-20)

Installed real Ollama (v0.34.2) on Windows via winget, pulled the real
`translategemma:4b` model (3.3GB, confirmed to exist and download
successfully — the model tag name was not a placeholder after all),
and ran the actual production code path (`GET /api/translate/status`,
`POST /api/translate/test`) against it — not a mock.

Confirmed working end-to-end:
- `ollama_reachable: true`, `live_model.installed: true` correctly
  detected via the real `/api/tags` call.
- Real translation, both directions, correct meaning:
  - en→vi: "We need to finish the project by next Friday." →
    "Chúng ta cần hoàn thành dự án vào tuần tới, thứ Sáu."
  - vi→en: "Xin chào, cuộc họp hôm nay rất hiệu quả." → "Hello, today's
    meeting was very productive."
  - en→vi: "The quarterly roadmap needs review before the deadline." →
    "Kế hoạch hàng quý cần được xem xét trước thời hạn."
- Response latency ~4s for a short sentence on this test machine — one
  data point, not a benchmark.

Not tested: the `translategemma:12b` batch model (an ~8GB download, not
pulled in this session), and the full pipeline integration through a
real `internal/stt` engine (whisper.cpp wasn't stood up for this pass)
— only the isolated `/api/translate/test` HTTP path was exercised. The
Ollama request/response shape and prompt wording used by
`OllamaTranslator` are now confirmed correct against a real server,
resolving the "placeholder, unverified" caveat noted above.

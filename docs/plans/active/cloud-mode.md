# Plan: Cloud / Self-host / Custom setup, bring-your-own-key providers

- Status: proposed
- Created: 2026-11-27
- Branch: `feat/cloud-mode` (one PR per phase below)
- Research: [cloud-mode-providers.md](cloud-mode-providers.md) (API notes and
  sources for every provider named here)

## Why

Today every default is self-hosted: Whisper for transcription, Ollama for
translation and summaries. A new user has to run a whisper.cpp server and
Ollama before anything works, and those setup steps show up all over the UI
(Settings, Get started, Start meeting, Import, meeting page, live status).
Most users would rather paste an API key from a provider. CatchM's main
target becomes **Cloud**: the user brings keys for third-party providers.
Self-host stays fully supported as its own setup.

## Decisions (user, 2026-11-27)

- **First run asks for a setup:** **Cloud**, **Self-host** or **Custom**.
  Custom lets the user pick a provider for each module, in the style of the
  better-t-stack stack builder (`better-t-stack.dev/new`): modules as
  sections, options as cards, presets that fill every module.
- **Cloud hides self-host.** Under Cloud, Whisper and Ollama appear nowhere:
  not in Settings, Get started, Start meeting, Import, the meeting page or
  live hints. Under Self-host the cloud providers are hidden the same way,
  except the CatchM Cloud account and sync, which aren't providers. Custom
  shows everything.
- **Transcription providers:** Soniox (existing), Deepgram Nova-3,
  ElevenLabs Scribe, Speechmatics, Gladia. AssemblyAI is left out: its
  streaming has no Vietnamese.
- **Translation is its own module.** A transcription provider doesn't need
  its own translation. Soniox, Speechmatics (pairs with English only) and
  Gladia can translate on their own, and when they can it's used
  automatically. Every other case goes through the Translation module's
  provider.
- **LLM:** one generic **OpenAI-compatible** provider with presets (OpenAI,
  Google Gemini, OpenRouter, Anthropic, Groq, DeepSeek, Cloudflare). It
  serves both translation and summaries.
- **Translation APIs:** **Google Cloud Translation** (500,000 characters a
  month free, then $20 per 1M) and **DeepL** (API Free: 500,000 characters
  a month). DeepL ships only if its API lists Vietnamese as a target.

## Modules and options

| Module | Cloud options | Self-host options |
|---|---|---|
| **Transcription** (live, plus re-transcribe and import) | Soniox · Deepgram · ElevenLabs · Speechmatics · Gladia | Whisper (whisper.cpp server) |
| **Translation** | Google Translate · DeepL · LLM (OpenAI-compatible) | Ollama |
| **Summaries** | CatchM Cloud (free) · LLM (OpenAI-compatible) | Ollama |

Notes on the table:

- Transcription covers both live and batch. Every cloud provider above has
  both a streaming API and a file API, so one choice drives both paths.
- On a card, translation that the transcription provider handles itself
  appears as a badge, not a separate option. The Translation section says:
  "Soniox / Speechmatics / Gladia translate on their own; this is used
  otherwise and for re-translation".

| Preset | Transcription | Translation | Summaries |
|---|---|---|---|
| **Cloud** | Soniox | Google Translate | CatchM Cloud |
| **Self-host** | Whisper | Ollama | Ollama |
| **Custom** | the user picks each one | | |

## Config (wire/storage)

```yaml
setup: cloud            # cloud | selfhost | custom; empty = first run
engine: soniox          # whisper | soniox | deepgram | elevenlabs | speechmatics | gladia
soniox:      {api_key, url}
deepgram:    {api_key}
elevenlabs:  {api_key}
speechmatics: {api_key}
gladia:      {api_key}
translate:
  provider: google      # google | deepl | llm | ollama
  google: {api_key}
  deepl:  {api_key}     # api-free.deepl.com for keys ending ":fx", else api.deepl.com
  host / live_model / batch_model   # Ollama, unchanged
llm:                    # shared by translate.provider=llm and summary.provider=openai
  preset: openai        # openai | gemini | openrouter | anthropic | groq | deepseek | cloudflare | custom
  base_url, api_key, fast_model, strong_model
  account_id            # cloudflare only
summary:
  provider: catchm-cloud   # ollama | catchm-cloud | openai (= the llm block)
```

Rules for existing installs and for keeping secrets out of the UI:

- **Migration:** an existing config with no `setup` becomes `custom`, so
  nothing an existing user sees changes. Its `summary.openai.*` moves into
  `llm` with `preset: cloudflare` when the base URL is Cloudflare,
  otherwise `custom`. A brand-new install gets `setup: ""`, and the web UI
  shows the setup chooser.
- **Keys:** keys are write-only over the API, the same way the Soniox key
  works today. `GET /api/config` returns only `*_set: true`, never the key.

## UI

### First run: setup chooser

This replaces the start of Get started. There are three large `ChoiceCard`s:

- **Cloud (recommended):** "Paste an API key, nothing to install".
- **Self-host:** "Runs on this computer: Whisper + Ollama".
- **Custom:** "Pick each part".

Picking Cloud or Self-host fills every module from the preset. Custom
opens the builder. After that choice, Get started lists only the steps that
the chosen providers need, for example "Add your Soniox key" and "Add your
Google Translate key".

### Builder

The builder appears in Settings → **Setup** (a new first section) and in
first-run Custom. Its layout follows better-t-stack:

- **Top:** a preset `SegmentedControl` (Cloud / Self-host / Custom). Picking
  a preset fills every module. Editing any module switches the preset to
  Custom.
- **Then one section per module:** an h2 with an icon (`SettingsSection`
  style), then a grid of option cards (`grid gap-3`, 2–3 columns).
- **Each card shows:**
  - the provider name;
  - one line on what it's good at;
  - price per hour;
  - badges: "Speakers", "Translates itself", "Free tier", "Default";
  - "Key added ✓" when its key is already set.
- **Selected card:** `border-primary bg-primary/5`, the existing convention.
- **Unusable cards:** a dashed border and `opacity-60`, with the reason as
  text. Example: "Whisper can't import MP3, WAV only".
- **Under the selected card:** the inline setup it needs. For a key that's
  a password field, Save, and a "Get a key" link to the provider's console.
  For Self-host options, that's the existing Whisper/Ollama setup blocks.
- **Hiding:** in Cloud and Self-host, cards from the other side are hidden,
  not disabled.

`ChoiceCard` moves out of `SettingsSummaries.tsx` into a shared primitive.
It gains `disabled` + `reason` and an optional `badges` slot. Everything
else follows `docs/ui-conventions.md`.

### Hiding self-host everywhere else (Cloud setup)

- **Start meeting:**
  - the engine picker lists only the configured providers;
  - no "Translation needs Ollama" message;
  - the "not ready" hint links to Settings → Setup.
- **Import:** the Whisper option is hidden, and so is the "Free and private"
  line.
- **Meeting page:**
  - the summary setup card offers CatchM Cloud / LLM only;
  - "Improve with Soniox" becomes "Re-transcribe with <provider>";
  - the error matching on "whisper" stays, but only in Self-host and Custom.
- **Live status:** the Whisper hints show only when the engine is Whisper.
- **Get started:** no Ollama or Whisper steps unless they're chosen.
- **Settings:** the Transcription, Translation and Summaries pages show only
  the settings for the chosen provider, plus the Advanced section.

## Backend

- **Config:**
  - `config.Setup`, the provider blocks and `llm`, plus the migration and
    validation (unknown engine or provider → 400).
  - `/api/config` exposes `*_set` flags.
  - New endpoint `POST /api/providers/{name}/verify`, a cheap key check per
    provider (the endpoints are listed in the research doc).
- **STT engines** (`internal/stt`): each provider gets a streaming engine
  and a batch engine. They implement `StreamEngine` / `BatchEngine`, are
  registered in `server.newEngine` / `newBatchEngine`, and report themselves
  through `/api/stt/status` (`configured` = key set).
  - **Deepgram:**
    - streaming: `wss://api.deepgram.com/v1/listen`, binary PCM, KeepAlive;
      when the language is empty it uses `language=multi`;
    - speakers via `diarize_model=latest`;
    - batch: `POST /v1/listen?utterances=true`.
  - **ElevenLabs:**
    - streaming: `scribe_v3_live`, audio as base64 JSON chunks batched to
      100–250 ms, `commit_strategy=vad`;
    - speakers: joined through `diarized_transcript.segment_index`;
    - batch: `scribe_v3` multipart;
    - language code `vie`.
  - **Speechmatics:**
    - streaming: StartRecognition with `enhanced` + diarization, or
      `melia-1` multi when the language is empty;
    - translation via `translation_config` (English pairs only), matched to
      transcripts by time;
    - batch: `/v2/jobs`.
  - **Gladia:**
    - streaming: `POST /v2/live`, then WebSocket binary PCM;
    - translation supported, live speakers not supported;
    - sessions are rotated at 3 h;
    - batch: upload + `/v2/pre-recorded` (with speakers).
- **Translation:** `internal/translate` gets a `Translator` per provider:
  `Google` (v2 REST, batching finals), `DeepL`, `LLM` (OpenAI-compatible,
  streamed plain text, no JSON mode) and the existing `Ollama`. The server
  picks by `translate.provider`, and the pipeline still prefers the
  engine's own translation.
- **Summaries:** the `openai` provider reads the `llm` block. It
  understands each preset's JSON mode (Anthropic accepts only `json_schema`
  strict; DeepSeek only `json_object` and the prompt must say "json";
  Cloudflare JSON mode only on listed models), and it omits
  `temperature`/`top_p` for Anthropic.
- **Unit tests** for each engine run against a fake WebSocket/HTTP server
  that replays the documented messages. **Real-key tests** are opt-in
  (`CATCHM_<PROVIDER>_KEY` set), like `CATCHM_TAP_TEST`.

## Phases (one PR each, in order)

1. **Setup + hiding:** the `setup` config and migration, the first-run
   chooser, the builder in Settings → Setup with the existing providers
   (Soniox, Whisper, Ollama, CatchM Cloud, Cloudflare), and every self-host
   surface hidden under Cloud. No new providers yet.
2. **Translation providers + generic LLM:** Google Translate, DeepL (if
   Vietnamese is confirmed), the LLM translator, the `llm` block with
   presets, and summaries moved onto it.
3. **Deepgram**
4. **ElevenLabs**
5. **Speechmatics**
6. **Gladia**
7. **Release** (with `tag-release`, after the user confirms).

## Acceptance criteria

1. **First run.** On a fresh `CATCHM_HOME`, the app opens the setup chooser.
   - Cloud → Settings and Get started show no Whisper, Ollama or model
     download anywhere.
   - Self-host → the same holds for Soniox, Deepgram, ElevenLabs,
     Speechmatics, Gladia, Google, DeepL and LLM.
   - Custom → every option is shown.
2. **Existing installs.** An existing config (no `setup`) opens as Custom
   with every previous choice intact, and a meeting still starts with the
   engine it used before.
3. **Builder.**
   - A preset fills all three modules, and editing any module switches the
     preset to Custom.
   - Unusable options show their reason.
   - The selected card shows its key field.
   - Verify reports valid or invalid against the provider's real endpoint.
4. **Each new transcription provider (phases 3–6)**, with a real key:
   - a live meeting gives partial and final lines, plus speakers where the
     provider supports them;
   - a Vietnamese and an English sample transcribe sensibly;
   - re-transcribe and import work;
   - a bad key gives a clear capture error that names the provider;
   - ending the meeting closes the session cleanly with no hanging
     goroutines (`-race` test against the fake server).
5. **Translation.**
   - With Deepgram or ElevenLabs plus Google, DeepL or LLM translation, the
     translated lines appear live.
   - With Soniox, Speechmatics (English pair) or Gladia, the provider's own
     translation is used and the Translation provider isn't called.
6. **Summaries.** An LLM preset (at least OpenAI and Gemini with real keys)
   produces a summary; Anthropic works through its `json_schema`-only path.
7. **No key ever leaves the server through the API.** `GET /api/config`
   returns `*_set` flags only.
8. **Checks.** `task lint` and `task test` pass, and the macOS CI job passes.

## Out of scope

- OpenAI realtime transcription (no diarization; its models are changing).
- Per-provider usage/cost tracking in the UI.
- Storing keys in the OS keychain (keys stay in `config.yaml` like Soniox's
  today; revisit with signing/notarization).

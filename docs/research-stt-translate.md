# Research: STT & Translation engines (M2/M3 reference)

- Status: reference
- Date: 2026-09-18
- Source: Perplexity research (pplx)

## Two separate STT subsystems

Realtime STT (live context) and batch STT (post-meeting refine) have
different requirements and are separate engines:

| | Realtime (live) | Batch (post-meeting) |
|---|---|---|
| Purpose | Instant context, live enrich | Canonical transcript for long-term store |
| Requirement | Low latency (1-3s), streaming PCM → utterance | Max accuracy, latency irrelevant |
| Input | PCM stream | Full audio file (requires storing raw audio per meeting) |
| Engines | Apple SpeechTranscriber, Soniox WS, whisper.cpp stream | whisper.cpp large-v3 + VAD + chunking, or high-quality cloud |
| Output | utterances.jsonl (append-only, v1) | utterances.v2.jsonl (does not touch v1) |

Meeting dir layout gains: `audio.wav` (raw, for batch) and
`utterances.v2.jsonl` (batch output). Enrichment splits into live enrich
(cursor-following, coarse) and final enrich (after batch refine, the
official frontmatter).

## STT engine shortlist

| Engine | Type | Diarization | Translate | Cost | Notes |
|---|---|---|---|---|---|
| Apple SpeechTranscriber (SpeechAnalyzer, macOS 26+) | local | no | no | free | Long-form streaming, on-device, 42 locales/22 langs incl. vi; punctuation yes; requires Swift helper sidecar (Go has no bindings); runs as user-session agent, not headless daemon |
| whisper.cpp server | local | no | no | free | Cross-platform default; batch file mode for refinement |
| Soniox | cloud | yes (built-in) | yes | ~$0.12/hr | Best cloud value for meetings; diarization included |
| Qwen LiveTranslate Flash | cloud | no (server VAD) | yes | free preview | 60 langs; source lang must be explicit; Singapore region only |
| OpenAI Realtime (gpt-realtime-translate) | cloud | no | yes + voice | ~$4/hr | Premium; native audio out |
| Groq Whisper / Deepgram | cloud | partial | no | cheap | Batch transcribe of old meeting files, later |

Tiering: free-private (Apple/whisper.cpp) → free-cloud (Qwen) →
cheap-cloud (Soniox) → premium (OpenAI). Engine chosen per meeting.

## Translation after STT (separate layer)

vieneu.io is a Vietnamese TTS project, not MT. ProtonX has Vietnamese
text-correction/OCR models (non-commercial license), not translation.
Actual local MT options:

| Model | Size | Vietnamese | Local | Live-subtitle suitability |
|---|---|---|---|---|
| TranslateGemma (Google) | 4B/12B/27B | yes (55 langs) | GGUF/Ollama, Apache-2.0 | Best default: 4B live, 12B batch |
| Hy-MT2 (Tencent) | 1.8B/7B/30B MoE | yes (33 langs) | official GGUF, llama.cpp | Ultra-low-latency: 1.8B Q4 fastest |
| Qwen (general instruct) | various | decent | GGUF/Ollama | Fallback; not translation-specialized, unpredictable output |
| Vistral-7B-MT | 7B | vi→en mainly | GGUF | Older, one direction |
| ViT5/envit5 | 0.2B | vi→en | transformers | Small but below Google Translate quality |

Recommendation: `internal/translate` as its own layer —
TranslateGemma-4B via Ollama/llama.cpp for live, TranslateGemma-12B or
Hy-MT2-7B for batch re-translate at meeting end.

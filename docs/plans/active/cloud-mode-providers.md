# Cloud-mode providers: API notes

Research for [cloud-mode.md](cloud-mode.md), compiled 2026-11-27 from each vendor's
official docs (links per section). Prices are the vendors' list prices on that
date, indicative only. "unverified" marks what the docs didn't settle; check it
against the live API while implementing.

## Deepgram Nova-3 (agent report, verified 2026-11-27, developers.deepgram.com)
Stream: wss://api.deepgram.com/v1/listen?encoding=linear16&sample_rate=16000&channels=1&model=nova-3&language=vi
  &interim_results=true&smart_format=true&punctuate=true&utterance_end_ms=1000&endpointing=300[&diarize_model=latest][&keyterm=X...]
Auth header: Authorization: Token <KEY>. Handshake errors: HTTP 4xx + dg-error header; 429 = concurrency.
Auto language: streaming has NO detection -> language=multi (55 langs incl vi; endpointing=100 recommended; language_hint=vi repeatable only with multi).
Results msg: {"type":"Results","is_final":bool,"speech_final":bool,"channel":{"alternatives":[{"transcript":..,"words":[{"punctuated_word","speaker":0,"start","end"}]}]}}
  is_final=false -> partial; accumulate is_final=true text, flush utterance on speech_final OR {"type":"UtteranceEnd"} (don't rely on speech_final alone).
Client: binary audio; {"type":"KeepAlive"} text every 3-5s when idle (10s silence -> close NET-0001); {"type":"Finalize"}; {"type":"CloseStream"} -> final Results + Metadata + close.
Close codes: 1008 DATA-0000 bad audio; 1011 NET-0000/0001/0002.
Diarization streaming: diarize_model=latest (do not also send diarize); words[].speaker int; segment may mix speakers -> split by word.
Keyterms: keyterm=<term> repeated, nova-3 only, 500 tokens total (20-50 terms), no weights/commas.
Translation: none.
Batch: POST https://api.deepgram.com/v1/listen?model=nova-3&language=vi&diarize_model=latest&utterances=true&smart_format=true&punctuate=true ; body raw file bytes Content-Type audio/wav etc; detect_language=true ok for batch.
  results.utterances[]{start,end,transcript,speaker}; max 2GB; >10min processing -> 504 (downsample/chunk).
Key check: GET https://api.deepgram.com/v1/auth/token (Authorization: Token) – unverified status code.
Price PAYG: stream mono $0.29/h, multi $0.35/h, +diarize $0.12/h, +keyterm $0.08/h; batch ~$0.26/h; $200 free credit. Concurrency 150 streaming / 50 batch per project.

## ElevenLabs Scribe (agent report, 2026-11-27, elevenlabs.io/docs)
Models: realtime scribe_v3_live (current; v2_realtime default/previous); batch scribe_v3.
WS: wss://api.elevenlabs.io/v1/speech-to-text/realtime?model_id=scribe_v3_live&audio_format=pcm_16000&commit_strategy=vad[&language_code=vie][&diarize=true][&keyterms=..][&include_timestamps=true]
Auth header xi-api-key. Wait for {"message_type":"session_started"} before audio.
Audio: JSON text frames {"message_type":"input_audio_chunk","audio_base_64":"<b64>","commit":false,"sample_rate":16000}; batch 20ms frames to 100-250ms. previous_text only on first chunk.
Commit: same with audio_base_64:"" commit:true. Server autocommits ~36s. Close: final commit, wait last committed_transcript, close normally.
Server: partial_transcript{text} (dedupe empty/repeat) -> Final=false; committed_transcript{text,segment_index?} -> Final=true;
  diarized_transcript{segment_index,words[{text,type,speaker_id:"speaker_0"}]} arrives AFTER committed -> join on segment_index; speakers_merged{from,to} chain.
Errors: {"message_type":"auth_error|quota_exceeded|rate_limited|session_time_limit_exceeded|input_error|...","error":".."} then close; reconnect on session_time_limit_exceeded. Idle 15s -> close.
Vietnamese: "vie" (639-3 safest). Realtime diarization only scribe_v3_live. Lang "" -> omit language_code.
Keyterms realtime: repeat keyterms, 100 terms / 1000 chars. +$0.05/h.
Translation: none.
Batch: POST https://api.elevenlabs.io/v1/speech-to-text multipart: model_id=scribe_v3, file, language_code?, diarize, keyterms[], timestamps_granularity=word -> {language_code,text,words[{text,type,start,end,speaker_id}]}. max 5GB/15h.
Key check: GET /v1/user (xi-api-key) — scoped keys may lack permission; treat 401/403 as invalid, or validate via WS auth_error.
Price: realtime $0.22/h, batch $0.20/h. Concurrency realtime Free 6 / Starter 9 / Creator 15 / Pro 30.

## Speechmatics (agent report, 2026-11-27, docs.speechmatics.com)
RT: wss://global.rt.speechmatics.com/v2 (eu./us./au. pinned). Auth header Authorization: Bearer <KEY>.
StartRecognition {"message":"StartRecognition","audio_format":{"type":"raw","encoding":"pcm_s16le","sample_rate":16000},
 "transcription_config":{"language":"vi","model":"enhanced","enable_partials":true,"max_delay":2.0,"diarization":"speaker","additional_vocab":[{"content":term}]},
 "translation_config":{"target_languages":["en"],"enable_partials":true}}  (operating_point deprecated -> model standard|enhanced|melia-1|oak-1)
Audio: binary frames (seq_no counted). End: {"message":"EndOfStream","last_seq_no":N} -> read until EndOfTranscript.
Server: RecognitionStarted; AddPartialTranscript/AddTranscript {metadata{transcript,start_time,end_time},results[{type word|punctuation,attaches_to,alternatives[{content,speaker:"S1"|"UU",language}]}]}
 finals never revised; partial = words since last final. Use metadata.transcript; split by speaker change.
 AddTranslation/AddPartialTranslation {language, results[{start_time,end_time,content,speaker}]} sentence-level; correlate by time.
 Error{type: not_authorised|quota_exceeded|invalid_language...} then close (4001 auth, 4005 quota). Warning, Info.
Translation pairs: only to/from English (vi<->en ok). Not on melia/oak.
Auto language RT: no "auto"; melia-1 + language "multi" (no translation, no vocab, needs ForceEndOfUtterance; vi unverified). Lang "" -> melia-1 multi.
Idle: 3min no audio and no ping -> close; session max 48h.
Batch: https://eu1.asr.api.speechmatics.com/v2 POST /jobs multipart config(JSON)+data_file -> {id}; GET /jobs/{id} status; GET /jobs/{id}/transcript?format=json-v2 (results + translations{en:[..]}); language "auto" ok in batch. <1GB.
Key check: GET https://eu1.asr.api.speechmatics.com/v2/jobs?limit=1 -> 200 / 401.
Price: realtime standard $0.45/h, enhanced $0.80/h; translation +$0.65/h; diarization included; $100 credit. Concurrency Free 2, Pro 50.

## Gladia v2 (agent report, 2026-11-27, docs.gladia.io)
Init: POST https://api.gladia.io/v2/live (x-gladia-key) body {encoding:"wav/pcm",bit_depth:16,sample_rate:16000,channels:1,model:"solaria-1",
  language_config:{languages:["vi"]|[] auto, code_switching}, realtime_processing:{custom_vocabulary:true,custom_vocabulary_config:{vocabulary:[..]},
  translation:true, translation_config:{target_languages:["en"], model:"base"(low latency; default batch), match_original_utterances:true, context}},
  messages_config:{receive_partial_transcripts:true,...}} -> 201 {id,url}. Errors 400/401/422/429.
WS (url): binary PCM frames; {"type":"stop_recording"} -> server flushes, post-processing, close 1000. Session cap 3h -> rotate. Reconnect same url on unexpected close.
Server: {type:"transcript",data:{id,is_final,utterance:{text,language,start,end,channel}}} partial/final share data.id; NO speaker.
  {type:"translation",data:{utterance_id,translated_utterance:{text},target_language}} (utterance_id==data.id unverified; finals only assumed).
  errors embedded in acks/translation as error:{status_code,message}.
Live diarization: NOT supported (async only).
Vietnamese: vi supported solaria-1 (live), translation yes; NOT solaria-3.
TwoWay: languages:[A,B], code_switching:true, target_languages:[A,B], keep target != utterance.language.
Batch: POST /v2/upload (multipart audio) -> audio_url; POST /v2/pre-recorded {audio_url,model:"solaria-1",language_config,diarization:true,translation..} -> {id}; poll GET /v2/pre-recorded/{id} status done -> result.transcription.utterances[]{text,speaker,start,end,language}, result.translation.results[]. Limit 1000MB/135min.
Key check: GET https://api.gladia.io/v2/pre-recorded?limit=1 -> 200 / 401.
Price: live €0.68/h (+€0.05/h translation); async solaria-1 €0.27/h. Free €50 credit. Concurrency Free 1 live, paid 30.

## OpenAI-compatible presets (agent report, 2026-11-27)
| Preset | Base URL | Key check | JSON | Live translate model | Summary model |
|---|---|---|---|---|---|
| OpenAI | https://api.openai.com/v1 | GET /models | json_object + json_schema | gpt-6-luna ($0.10/$0.50) | gpt-6.2-sol ($2/$10), gpt-6.1-terra ($1/$5) |
| Gemini | https://generativelanguage.googleapis.com/v1beta/openai/ | GET /models | json_schema (json_object undocumented) | gemini-2.5-flash-lite / gemini-3.1-flash-lite | gemini-3.8-flash |
| OpenRouter | https://openrouter.ai/api/v1 | GET /key | per provider; send provider.require_parameters | anthropic/claude-haiku-5.5 | anthropic/claude-sonnet-5.5 |
| Anthropic | https://api.anthropic.com/v1/ | GET /models? | json_schema strict ONLY (json_object -> 400); omit temperature/top_p | claude-haiku-5-5 | claude-sonnet-5-5 |
| Groq | https://api.groq.com/openai/v1 | GET /models | json_object/json_schema (no streaming w/ structured) | openai/gpt-oss-20b | openai/gpt-oss-120b |
| DeepSeek | https://api.deepseek.com | GET /models? | json_object only, prompt must say "json"; blank content => retry | deepseek-flash | deepseek-v4-pro |
| Cloudflare | https://api.cloudflare.com/client/v4/accounts/{id}/ai/v1 | tokens/verify? | JSON mode only listed models (qwen3-30b not listed) | @cf/qwen/qwen3-30b-a3b-fp8 | same |
Universal key check: 1-token chat completion (max_tokens:1) -> 200 vs 401/403.
Live translation: stream plain text, no response_format. JSON only for summaries.
Gemini free tier may use data for training (UI note). Anthropic compat endpoint "not production-ready"; thinking on by default (latency).

## Translation APIs

- **Google Cloud Translation** (Basic v2, API key): `POST https://translation.googleapis.com/language/translate/v2?key=KEY`
  `{q:[...], target:"vi", source?:"en", format:"text"}` → `data.translations[].translatedText`.
  First 500,000 characters/month free (Basic + Advanced combined), then $20 per 1M characters; input and output
  characters are metered separately. Needs a Google Cloud project with billing enabled.
  ([pricing](https://cloud.google.com/translate/pricing))
- **DeepL API** (Free plan: 500,000 characters/month): `POST https://api-free.deepl.com/v2/translate`
  (Pro: `api.deepl.com`), header `Authorization: DeepL-Auth-Key KEY`. Vietnamese is supported by DeepL
  Translator ([blog](https://www.deepl.com/zh/blog/vietnamese-thai-hebrew-launch)), but API support is unverified:
  check `GET /v3/languages` (`usable_as_target`) before shipping it.
  ([API Free](https://support.deepl.com/hc/en-us/articles/360021200939-DeepL-API-Free),
  [languages](https://developers.deepl.com/docs/getting-started/supported-languages))

## Not chosen

- **AssemblyAI**: streaming covers only 6 languages, none of them Vietnamese.
- **OpenAI realtime transcription**: no diarization, and the model lineup is in flux (several transcription
  models shut down 2027-02-26) ([guide](https://developers.openai.com/api/docs/guides/realtime-transcription)).
  Revisit later.

## Sources

- Deepgram: [streaming](https://developers.deepgram.com/reference/speech-to-text/listen-streaming),
  [models/languages](https://developers.deepgram.com/docs/models-languages-overview),
  [endpointing](https://developers.deepgram.com/docs/understand-endpointing-interim-results),
  [diarization](https://developers.deepgram.com/docs/diarization), [keyterm](https://developers.deepgram.com/docs/keyterm),
  [pre-recorded](https://developers.deepgram.com/reference/speech-to-text/listen-pre-recorded),
  [pricing](https://deepgram.com/pricing), [rate limits](https://developers.deepgram.com/reference/api-rate-limits)
- ElevenLabs: [realtime API](https://elevenlabs.io/docs/api-reference/speech-to-text/v-1-speech-to-text-realtime),
  [capabilities](https://elevenlabs.io/docs/capabilities/speech-to-text), [models](https://elevenlabs.io/docs/overview/models),
  [batch](https://elevenlabs.io/docs/api-reference/speech-to-text/convert), [pricing](https://elevenlabs.io/pricing/api)
- Speechmatics: [realtime API](https://docs.speechmatics.com/api-ref/realtime-transcription-websocket),
  [languages](https://docs.speechmatics.com/speech-to-text/languages),
  [translation](https://docs.speechmatics.com/speech-to-text/features/translation),
  [batch jobs](https://docs.speechmatics.com/api-ref/batch/create-a-new-job), [pricing](https://www.speechmatics.com/pricing)
- Gladia: [live init](https://docs.gladia.io/api-reference/v2/live/init),
  [live WebSocket](https://docs.gladia.io/api-reference/v2/live/websocket),
  [languages](https://docs.gladia.io/chapters/language/supported-languages),
  [pre-recorded](https://docs.gladia.io/api-reference/v2/pre-recorded/init), [pricing](https://www.gladia.io/pricing)
- LLM presets: [OpenAI](https://developers.openai.com/api/docs/models), [Gemini OpenAI compat](https://aistudio.google.com/docs/openai),
  [OpenRouter](https://openrouter.ai/docs/api-reference/overview), [Anthropic OpenAI SDK compat](https://platform.claude.com/docs/en/api/openai-sdk),
  [Groq](https://console.groq.com/docs/openai), [DeepSeek](https://api-docs.deepseek.com/),
  [Cloudflare](https://developers.cloudflare.com/workers-ai/configuration/open-ai-compatibility/)
- Soniox (existing engine): [Vietnamese translation](https://soniox.com/speech-translation/vietnamese)

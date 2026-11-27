import i18next from "i18next"

// Typed client for the CatchM REST API served by internal/server.
//
// Every export here corresponds 1:1 to a route registered in
// internal/server/server.go's routes(), and every field name mirrors the
// exact JSON the corresponding Go handler reads/writes (see that file —
// this is not guessed).

/** A transcript line, as produced by internal/meeting.Utterance. */
export interface Utterance {
  seq: number
  /** RFC3339 timestamp string. */
  at: string
  speaker?: string
  text: string
  translated?: string
  lang?: string
  /** End of the utterance (RFC3339), when the engine placed it in the audio. */
  end?: string
  /** The input that carried it, in meetings that recorded both. */
  source?: "mic" | "system"
  extra?: unknown
}

/** internal/meeting.ActionItem. */
export interface ActionItem {
  text: string
  owner?: string
  due?: string
  status?: string
}

/** internal/meeting.Decision. */
export interface Decision {
  text: string
  scope?: string
}

/** internal/meeting.Meeting. */
export interface Meeting {
  schema_version: number
  id: string
  title: string
  /** RFC3339 timestamp string. */
  started_at: string
  /** RFC3339 timestamp string, absent while the meeting is still live. */
  ended_at?: string | null
  participants?: string[]
  langs?: string[]
  engine?: string
  /** M19 §4: which input(s) this meeting is (or was) capturing from. */
  audio_source?: AudioSource
  lang?: string
  translate_to?: string
  /** When true, lang and translate_to are the two conversation languages. */
  two_way?: boolean
  tags?: string[]
  decisions?: Decision[]
  action_items?: ActionItem[]
  terms?: Record<string, string>
  summary?: string
  notes?: string[]
  refined?: boolean
  /** Which engine re-transcribed the meeting (an SttEngine); empty for engine "none". */
  refined_with?: string
  /** True when the meeting was created from an uploaded recording. */
  imported?: boolean
  /** Confirmed speaker names by key (lib/speakers.ts). */
  speakers?: Record<string, string>
  guests?: Guest[]
  /** The speaker key the user said is them. */
  me?: string
}

/**
 * internal/capture.Device. Note: the Go struct has no `json` tags, so the
 * wire keys are the exported Go field names verbatim (capitalized) — this
 * is not a typo.
 */
export interface Device {
  ID: string
  Name: string
  IsDefault: boolean
}

/** Bring-your-own-key transcription providers (internal/server
 *  cloudEngines). Each has `<id>_api_key_set` in AppConfig and a
 *  write-only `<id>_api_key` in UpdateConfigRequest; the UI's catalog
 *  of them is lib/setup.ts CLOUD_ENGINES. */
export type CloudSttEngine = "soniox" | "deepgram" | "elevenlabs" | "speechmatics" | "gladia"

export type SttEngine = "whisper" | CloudSttEngine

/** UI text language preference ("system" follows the OS language).
 *  Not to be confused with the STT Lang below. */
export type UILanguage = "system" | "en" | "vi"

/** M19 §4: where the audio comes from. Default "mic" when omitted. */
export type AudioSource = "mic" | "system" | "both"

/** Body for POST /api/meetings. */
export interface StartMeetingRequest {
  title: string
  device?: string
  /** M19: mic device ID (unchanged meaning). */
  engine?: SttEngine | ""
  lang?: string
  translate_to?: string
  /** When true, lang and translate_to are the two conversation languages. */
  two_way?: boolean
  tags?: string[]
  /** Project display name, added as the meeting's `project:` tag. */
  project?: string
  /** M19 §4: "mic" | "system" | "both". Ignored by pre-M19 backends. */
  source?: AudioSource
}

/** Classification of a capture start/switch failure — the stable wire
 *  value of capture_error_kind (internal/server/server.go's
 *  CaptureErrorKind), which the UI switches on instead of matching the
 *  human-readable capture_error text. */
export type CaptureErrorKind =
  | "missing_api_key"
  | "device_unavailable"
  | "unsupported"
  | "engine"
  | "internal"

/**
 * POST /api/meetings response — a Meeting, plus M19's capture_error when
 * the meeting was created but the engine or audio source failed to start
 * (absent on success). Never swallow it: the UI must surface it.
 * capture_error_kind (present exactly when capture_error is) says which
 * class of failure it is so callers can act without string-matching the
 * message.
 */
export interface StartMeetingResponse extends Meeting {
  capture_error?: string
  capture_error_kind?: CaptureErrorKind
  /** Source "both" whose microphone couldn't open (e.g. a Mac mini with
   *  no mic): recording continues on computer audio only. Absent
   *  otherwise; never set together with capture_error. */
  mic_error?: string
}

/** Body for PATCH /api/meetings/{id} — at least one field required. */
export interface UpdateMeetingRequest {
  title?: string
  tags?: string[]
  /** Merged into the confirmed names; "" removes one. */
  speakers?: Record<string, string>
  /** Replaces the guest list. */
  guests?: Guest[]
  /** The speaker key that is the user; "" forgets it. */
  me?: string
}

/** internal/meeting.Guest: someone invited, pasted from a calendar. */
export interface Guest {
  name?: string
  email?: string
}

/** Response body for GET /api/meetings/{id}/transcript. */
export interface TranscriptResponse {
  utterances: Utterance[]
  cursor: number
}

export interface TranslateModelStatus {
  name: string
  installed: boolean
}

/** GET /api/translate/status for Ollama: whether it runs and has the
 *  models (ready = the live model is downloaded). */
export interface OllamaTranslateStatus {
  provider: "ollama"
  ready: boolean
  ollama_reachable: boolean
  live_model: TranslateModelStatus
  batch_model: TranslateModelStatus
}

/** GET /api/translate/status for Google, the LLM and "none": nothing is
 *  probed; ready says whether text translation works, reason (English,
 *  only when not ready) what's missing. */
export interface CloudTranslateStatus {
  provider: Exclude<TranslateProvider, "ollama">
  ready: boolean
  reason?: string
}

/** Response body for GET /api/translate/status; narrow on provider. */
export type TranslateStatus = OllamaTranslateStatus | CloudTranslateStatus

export type TranslateTarget = "live" | "batch"

/** Which Ollama model POST /api/translate/pull downloads: the live or
 *  batch translation model, or the summary model (summary.ollama_model). */
export type PullTarget = TranslateTarget | "summary"

/** One engine's readiness; reason is user-facing and set only when not ready. */
export interface SttEngineStatus {
  ready: boolean
  reason?: string
}

/** GET /api/stt/status response: the configured engine, plus one
 *  readiness entry per engine (Whisper and every cloud engine). */
export type SttStatus = { default_engine: SttEngine } & Record<SttEngine, SttEngineStatus>

/** Body for POST /api/translate/pull. */
export interface PullTranslateModelRequest {
  target: PullTarget
}

/** One decoded line of the NDJSON stream from POST /api/translate/pull. */
export interface PullProgressEvent {
  status?: string
  error?: string
}

/** Body for POST /api/translate/test. */
export interface TestTranslateRequest {
  text: string
  target_lang: string
  target?: TranslateTarget
}

/** Response body for POST /api/translate/test. */
export interface TestTranslateResponse {
  translated: string
}

/** config.SummaryConfig's provider: local Ollama (default), CatchM Cloud
 *  (transcript sent unencrypted, needs cloud_consent), or any
 *  OpenAI-compatible server. */
export type SummaryProvider = "ollama" | "catchm-cloud" | "openai"

/** The summary block of GET/PUT /api/config responses. Provider
 *  "openai" uses the shared llm block (AppConfig.llm). */
export interface SummaryConfig {
  provider: SummaryProvider
  ollama_model: string
  cloud_consent: boolean
}

/** llm.preset: an OpenAI-compatible service with known server and
 *  default models (Go config.LLMPresets), or "custom" (base_url as
 *  given). "" until one is chosen. */
export type LlmPreset =
  | "openai"
  | "gemini"
  | "openrouter"
  | "anthropic"
  | "groq"
  | "deepseek"
  | "zai"
  | "cloudflare"
  | "custom"

/** The shared OpenAI-compatible LLM (translate.provider "llm" uses the
 *  fast model, summary.provider "openai" the strong one). The key is
 *  never included; see AppConfig.llm_api_key_set. */
export interface LlmConfig {
  preset: LlmPreset | ""
  /** Custom preset only. */
  base_url: string
  /** Cloudflare preset only. */
  account_id: string
  /** "" = the preset's default (see resolved). */
  fast_model: string
  strong_model: string
  /** After the preset's defaults: what the server will actually call.
   *  base_url is "" while it can't be named yet (custom without a URL,
   *  Cloudflare without an account ID). */
  resolved: { base_url: string; fast_model: string; strong_model: string }
}

/** GET /api/summary/status response (always 200). ready/reason describe
 *  the active provider; reason is user-facing text, present only when
 *  not ready. The backend sends every provider block, but callers must
 *  treat them as optional. The cloud quota fields are absent when
 *  logged out or when catchm-api's quota endpoint can't be reached. */
export interface SummaryStatus {
  provider: SummaryProvider
  ready: boolean
  reason?: string
  ollama?: { reachable: boolean; model: string; installed: boolean }
  cloud?: {
    logged_in: boolean
    consent: boolean
    remaining_today?: number
    limit?: number
    /** RFC3339 timestamp string. */
    reset_at?: string
  }
  /** The shared LLM (summary provider "openai") has a server, model and key. */
  openai?: { configured: boolean }
  /** Host facts for model recommendations, in GiB rounded to one
   *  decimal. free_disk_gb is the volume holding Ollama's models. Each
   *  field is omitted when it can't be read. */
  system?: { total_ram_gb?: number; free_disk_gb?: number }
}

/** Classification of a summary failure on POST /api/meetings/{id}/refine
 *  (internal/summarize.Kind). consent_required answers 409 and means
 *  nothing was sent; quota_exceeded answers 429 with reset_at. */
export type SummaryErrorKind =
  | "summary_not_ready"
  | "consent_required"
  | "quota_exceeded"
  | "summary_failed"

/** Which setup the user chose (docs/plans/completed/cloud-mode.md): ""
 *  until the first-run chooser answers; configs from before setups
 *  load as "custom". */
export type Setup = "" | "cloud" | "selfhost" | "custom"

/** translate.provider: Ollama on this computer, Google Cloud
 *  Translation, the shared LLM (AppConfig.llm), or "none" — no text
 *  translator, only the transcription engine's own translation (Soniox). */
export type TranslateProvider = "ollama" | "google" | "llm" | "none"

/** `<engine>_api_key_set`: true when that cloud engine's API key is
 *  stored; the key itself is never sent. */
type CloudKeySetFlags = { [E in CloudSttEngine as `${E}_api_key_set`]: boolean }

/** `<engine>_api_key`: write-only; omitted/empty preserves the stored key. */
type CloudKeyFields = { [E in CloudSttEngine as `${E}_api_key`]?: string }

/** Sanitized config shape shared by GET and PUT /api/config responses. */
export interface AppConfig extends CloudKeySetFlags {
  setup: Setup
  engine: string
  lang: string
  ui_language: UILanguage
  whisper_url: string
  soniox: { url: string }
  translate: {
    provider: TranslateProvider
    /** True when a Google Cloud Translation key is stored; never the key. */
    google_api_key_set: boolean
    host: string
    live_model: string
    batch_model: string
  }
  llm: LlmConfig
  /** True when the shared LLM's API key is stored; never the key. */
  llm_api_key_set: boolean
  summary: SummaryConfig
  /** Who the user is: their name, the emails that find them in a guest
   *  list, and what people call them. */
  profile: { name: string; emails: string[]; aliases: string[] }
}

/**
 * Body for PUT /api/config — a PARTIAL update: the server applies
 * setIfNonEmpty per field, so only included fields change and omitted or
 * blank ones are left alone. Every field is optional; send just what a
 * page changed, never a full snapshot (a stale snapshot would silently
 * overwrite newer values saved elsewhere). The `<engine>_api_key` fields
 * are write-only and never present in responses; omitting/emptying one
 * preserves the stored key.
 */
export interface UpdateConfigRequest extends CloudKeyFields {
  /** "" is ignored (partial update); the chooser sends a real setup. */
  setup?: Exclude<Setup, "">
  engine?: string
  lang?: string
  ui_language?: UILanguage
  whisper_url?: string
  soniox?: { url: string }
  translate?: {
    provider?: TranslateProvider
    /** Write-only; omitted/empty preserves the stored key. */
    google_api_key?: string
    host?: string
    live_model?: string
    batch_model?: string
  }
  /**
   * Unlike the rest, the llm strings are settable to "": a model back to
   * the preset's default, a custom URL cleared. Omitted = as-is.
   * api_key is write-only; omitted/empty preserves the stored key, except
   * that moving the LLM to another server (preset, base_url or
   * account_id) without a new key drops it (a key is never sent to a
   * server it wasn't entered for).
   */
  llm?: {
    preset?: LlmPreset
    base_url?: string
    account_id?: string
    fast_model?: string
    strong_model?: string
    api_key?: string
  }
  /** Partial like the rest. cloud_consent: false revokes consent. */
  summary?: {
    provider?: SummaryProvider
    ollama_model?: string
    cloud_consent?: boolean
  }
  /** "" clears the name; lists replace the stored ones. */
  profile?: { name?: string; emails?: string[]; aliases?: string[] }
}

/** Stable codes the Go server attaches to user-reachable errors
 *  (internal/server/server.go's httpErrorCode), mapped to dictionary
 *  keys. Codes outside this map fall back to the raw English message. */
const ERROR_KEYS = {
  meeting_not_found: "errors.meetingNotFound",
  meeting_active: "errors.meetingActive",
  no_audio: "errors.noAudio",
  action_item_not_found: "errors.actionItemNotFound",
  no_live_pipeline: "errors.noLivePipeline",
  summary_required: "errors.summaryRequired",
  file_too_large: "errors.fileTooLarge",
  whisper_wav_only: "errors.whisperWavOnly",
  file_required: "errors.fileRequired",
  invalid_credentials: "errors.invalidCredentials",
  email_taken: "errors.emailTaken",
  rate_limited: "errors.rateLimited",
  cloud_storage_full: "errors.cloudStorageFull",
  invalid_recovery_code: "errors.invalidRecoveryCode",
  not_logged_in: "errors.notLoggedIn",
  logout_required: "errors.logoutRequired",
  session_ended: "errors.sessionEnded",
  session_not_found: "errors.sessionNotFound",
  invalid_code: "errors.invalidCode",
  already_verified: "errors.alreadyVerified",
  email_unavailable: "errors.emailUnavailable",
  totp_already_enabled: "errors.totpAlreadyEnabled",
  totp_not_enabled: "errors.totpNotEnabled",
  totp_not_set_up: "errors.totpNotSetUp",
  cloud_unsupported: "errors.cloudUnsupported",
  wrong_password: "errors.wrongPassword",
  same_password: "errors.samePassword",
} as const

/** The user-facing text for an error body: translated when the server
 *  sent a known code, otherwise the original English message. */
const localized = (message: string, code: string | undefined): string =>
  code && Object.hasOwn(ERROR_KEYS, code) ? i18next.t(ERROR_KEYS[code as keyof typeof ERROR_KEYS]) : message

/** Thrown by every client function below on a non-2xx response. message
 *  is user-facing (translated when the server sent a known code);
 *  serverMessage and body keep the original English for logs. body is
 *  the parsed JSON error body when there was one. */
export class ApiError extends Error {
  status: number
  body?: Record<string, unknown>
  /** The server's stable error code, when it sent one (ERROR_KEYS). */
  code?: string
  /** Original English message from the server, untranslated. */
  serverMessage: string
  constructor(message: string, status: number, body?: Record<string, unknown>) {
    const code = typeof body?.code === "string" ? body.code : undefined
    super(localized(message, code))
    this.name = "ApiError"
    this.status = status
    this.body = body
    this.code = code
    this.serverMessage = message
  }
}

/** Thrown by refineMeeting when the summary step failed: the error body
 *  carries a kind (the transcription errors stay plain ApiErrors).
 *  .message is the user-facing text. */
export class RefineError extends ApiError {
  kind: SummaryErrorKind
  /** RFC3339 timestamp string; set for quota_exceeded. */
  reset_at?: string
  /** Set when the server kept the meeting despite the failure (import). */
  meeting_id?: string
  constructor(message: string, status: number, kind: SummaryErrorKind, resetAt?: string, body?: Record<string, unknown>) {
    super(message, status, body)
    this.name = "RefineError"
    this.kind = kind
    this.reset_at = resetAt
    if (typeof body?.meeting_id === "string") this.meeting_id = body.meeting_id
  }
}

/** The ApiError for a non-2xx response: the JSON body's error/code when
 *  there is one, else the status text. */
async function apiErrorFrom(res: Response): Promise<ApiError> {
  let message = res.statusText || i18next.t("errors.requestFailed", { status: res.status })
  let errBody: Record<string, unknown> | undefined
  try {
    const body: unknown = await res.json()
    if (body && typeof body === "object") {
      errBody = body as Record<string, unknown>
      if (typeof errBody.error === "string") {
        message = errBody.error
      }
    }
  } catch {
    // Body wasn't JSON (or was empty) — fall back to statusText above.
  }
  return new ApiError(message, res.status, errBody)
}

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
  })
  if (!res.ok) {
    throw await apiErrorFrom(res)
  }
  if (res.status === 204) {
    return undefined as T
  }
  return (await res.json()) as T
}

/** GET /healthz */
export function getHealthz(): Promise<{ status: string }> {
  return apiFetch("/healthz")
}

/** GET /api/devices — degrades to [] server-side if no audio backend. */
export async function getDevices(): Promise<Device[]> {
  return (await apiFetch<Device[] | null>("/api/devices")) ?? []
}

/** GET /api/meetings (optionally GET /api/meetings?q=...) */
export async function listMeetings(q?: string): Promise<Meeting[]> {
  const qs = q ? `?q=${encodeURIComponent(q)}` : ""
  return (await apiFetch<Meeting[] | null>(`/api/meetings${qs}`)) ?? []
}

/** GET /api/audio/sources — M19 contract (docs/plans/active/m19-audio-sources.md §4).
 *  Not implemented yet until M19 merges; every field is optional and callers
 *  must degrade gracefully when the endpoint or fields are missing. */
export interface AudioSources {
  mic_devices?: Device[]
  system?: { supported?: boolean; reason?: string }
}

/** GET /api/audio/sources — degrades to null when the endpoint doesn't
 *  exist yet (M19 not merged). */
export function getAudioSources(): Promise<AudioSources> {
  return apiFetch("/api/audio/sources")
}

/** POST /api/meetings/{id}/source — switches a live meeting's audio
 *  source mid-session (M19 §4, M21 §1). 404 when the meeting has no
 *  live pipeline; 400 for a bad source value; 422 when the new source
 *  can't open (device busy, unsupported, ...) — the old input stays
 *  active, and the thrown ApiError's .message is the human-readable
 *  reason and .status is 422, so callers can show it inline without
 *  losing the current selection. */
export function setMeetingSource(id: string, source: AudioSource, device?: string): Promise<{ source: AudioSource; device: string }> {
  return apiFetch(`/api/meetings/${id}/source`, {
    method: "POST",
    body: JSON.stringify({ source, device }),
  })
}

/** One input's level + drop diagnostics, from GET /api/meetings/{id}/levels. */
export interface InputLevels {
  rms: number
  peak: number
  dropped_chunks: number
  dropped_samples: number
  /** M25 (macOS system audio): true once the tap has delivered nothing
   *  but silence for 10 s since it started — usually a missing "System
   *  Audio Recording" permission, or simply nothing playing yet.
   *  Latches false for good once real audio arrives. Absent otherwise. */
  no_signal?: boolean
}

/** M21 §2: an engine's connection state, present only when the running
 *  engine tracks one truthfully (see stt.EngineStatusReporter — Whisper
 *  always reports "streaming", Soniox reports "streaming"/"error"; no
 *  engine currently produces "connecting"/"reconnecting", so treat this
 *  as an open string, not an exhaustive union). */
export interface EngineLevelsStatus {
  state: string
  error?: string
}

/** GET /api/meetings/{id}/levels — polled at ~5 Hz while the Live view
 *  is mounted and visible. mic/system are present only while that input
 *  is active; engine is present only when the running engine reports a
 *  state (M21 §2). 404 when the meeting has no live pipeline. */
export interface MeetingLevels {
  dropped_ticks: number
  mic?: InputLevels
  system?: InputLevels
  engine?: EngineLevelsStatus
}

export function getMeetingLevels(id: string): Promise<MeetingLevels> {
  return apiFetch(`/api/meetings/${id}/levels`)
}

/** POST /api/meetings */
export function startMeeting(body: StartMeetingRequest): Promise<StartMeetingResponse> {
  return apiFetch("/api/meetings", {
    method: "POST",
    body: JSON.stringify(body),
  })
}

/** GET /api/meetings/{id} */
export function getMeeting(id: string): Promise<Meeting> {
  return apiFetch(`/api/meetings/${encodeURIComponent(id)}`)
}

/** internal/summarize.GeneratedInfo: how an analysis was produced. */
export interface AnalysisGenerated {
  /** RFC3339 timestamp string. */
  at?: string
  provider?: string
  model?: string
  /** Which transcript was read: the batch one ("v2") or the live one. */
  transcript?: "v2" | "live"
  /** The meeting's language hint, or "auto". */
  analysis_lang?: string
}

/** internal/summarize.Topic. key is a canonical English slug, e.g. "topic:pricing-page". */
export interface AnalysisTopic {
  id: string
  title: string
  key: string
  /** Utterance seqs (in the transcript named by generated.transcript). */
  evidence: number[]
}

/** internal/summarize.AnalysisDecision. topic is a topic id. */
export interface AnalysisDecision {
  id: string
  text: string
  topic?: string
  scope?: string
  evidence: number[]
}

/** internal/summarize.AnalysisAction. owner is a person key ("person:long")
 *  when it names a listed person, else plain text. due is YYYY-MM-DD only;
 *  any other stated deadline is due_text. */
export interface AnalysisAction {
  id: string
  text: string
  topic?: string
  owner?: string
  due?: string
  due_text?: string
  evidence: number[]
}

/** internal/summarize.Person. */
export interface AnalysisPerson {
  key: string
  name: string
  aliases?: string[]
  /** The transcript's speaker label, when known. */
  speaker?: string
}

export type EntityKind = "project" | "product" | "org" | "tool" | "term" | "place"

/** internal/summarize.Entity. key is "<kind>:<slug>". */
export interface AnalysisEntity {
  key: string
  name: string
  kind: EntityKind
  evidence: number[]
}

/** internal/summarize.FlowNext: an edge to another step, optionally a branch. */
export interface FlowNext {
  to: string
  condition?: string
}

/** internal/summarize.FlowStep. order is 1..n; actor, like Flow.actor, is
 *  a person key when it names a listed person, else a role as plain text. */
export interface FlowStep {
  id: string
  order: number
  text: string
  actor?: string
  /** Utterance seqs, where the model could tell; else use the flow's. */
  evidence?: number[]
  next?: FlowNext[]
}

/** internal/summarize.Flow. */
export interface Flow {
  id: string
  name: string
  actor?: string
  trigger?: string
  evidence: number[]
  steps: FlowStep[]
}

/** internal/summarize.Analysis: a meeting's analysis.json (v1). */
export interface Analysis {
  version: number
  generated: AnalysisGenerated
  summary: string
  topics: AnalysisTopic[]
  decisions: AnalysisDecision[]
  actions: AnalysisAction[]
  people: AnalysisPerson[]
  entities: AnalysisEntity[]
  /** Empty until the flows pass runs. */
  flows: Flow[]
  /** The validator's log: "auto-corrected: …" / "dropped: …". */
  issues: string[]
}

/** internal/summarize.MindNode. ref is "topic:<id>", "decision:<id>" or
 *  "action:<id>"; absent on the root and on the "Other" branch. */
export interface MindNode {
  label: string
  ref?: string
  children?: MindNode[]
}

export interface MeetingAnalysis {
  analysis: Analysis
  mindmap: MindNode
}

/** GET /api/meetings/{id}/analysis — the analysis sidecar and its derived
 *  mind map. Rejects with a 404 ApiError when the meeting has no analysis
 *  yet (or doesn't exist). */
export function getMeetingAnalysis(id: string): Promise<MeetingAnalysis> {
  return apiFetch(`/api/meetings/${encodeURIComponent(id)}/analysis`)
}

/** The body of GET /analysis and POST /flows. */
export type MeetingAnalysisResponse = MeetingAnalysis

/** POST /api/meetings/{id}/flows — runs the on-demand flows pass (one or
 *  more LLM calls, can take a while) and answers the updated analysis;
 *  no flows is a valid result. Throws RefineError when the error body has
 *  a kind (provider not ready, CatchM Cloud unsupported, model failed);
 *  "Create a summary first." (409) is a plain ApiError. */
export async function findFlows(id: string): Promise<MeetingAnalysisResponse> {
  try {
    return await apiFetch<MeetingAnalysisResponse>(`/api/meetings/${encodeURIComponent(id)}/flows`, {
      method: "POST",
    })
  } catch (err) {
    throw withKind(err)
  }
}

/** PATCH /api/meetings/{id} — update title and/or tags. */
export function updateMeeting(id: string, body: UpdateMeetingRequest): Promise<Meeting> {
  return apiFetch(`/api/meetings/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(body),
  })
}

/** POST /api/guests/parse — reads a pasted guest list for review. */
export async function parseGuests(text: string): Promise<Guest[]> {
  const res = await apiFetch<{ guests: Guest[] }>(`/api/guests/parse`, {
    method: "POST",
    body: JSON.stringify({ text }),
  })
  return res.guests ?? []
}

/** DELETE /api/meetings/{id} — resolves on 204. */
export function deleteMeeting(id: string): Promise<void> {
  return apiFetch(`/api/meetings/${encodeURIComponent(id)}`, {
    method: "DELETE",
  })
}

/** GET /api/meetings/{id}/transcript?cursor=N */
/** Which transcript to read: the live one (default), the batch one a
 *  refine wrote, or v2-when-present. The response says which it served. */
export type TranscriptSource = "live" | "v2" | "best"

export async function getTranscript(
  id: string,
  cursor: number,
  source?: TranscriptSource,
): Promise<TranscriptResponse & { source: "live" | "v2" }> {
  const q = source && source !== "live" ? `&source=${source}` : ""
  const res = await apiFetch<TranscriptResponse & { source?: "live" | "v2" }>(
    `/api/meetings/${encodeURIComponent(id)}/transcript?cursor=${cursor}${q}`,
  )
  return { utterances: res.utterances ?? [], cursor: res.cursor, source: res.source ?? "live" }
}

/** Export formats served by GET /api/meetings/{id}/export. */
export type ExportFormat = "txt" | "md" | "docx"

/** GET /api/meetings/{id}/export?format=…&lang=… — for browser
 *  downloads via an <a download>; the desktop shell saves natively. */
export function exportUrl(id: string, format: ExportFormat, lang: string): string {
  return `/api/meetings/${encodeURIComponent(id)}/export?format=${format}&lang=${encodeURIComponent(lang)}`
}

/** GET /api/meetings/{id}/export as text, for clipboard copies. */
export async function fetchExportText(id: string, format: "txt" | "md", lang: string): Promise<string> {
  const res = await fetch(exportUrl(id, format, lang))
  if (!res.ok) {
    throw await apiErrorFrom(res)
  }
  return res.text()
}

/** POST /api/meetings/{id}/notes */
export function addNote(id: string, note: string): Promise<{ status: string }> {
  return apiFetch(`/api/meetings/${encodeURIComponent(id)}/notes`, {
    method: "POST",
    body: JSON.stringify({ note }),
  })
}

/** POST /api/meetings/{id}/end */
export function endMeeting(id: string): Promise<{ status: string }> {
  return apiFetch(`/api/meetings/${encodeURIComponent(id)}/end`, {
    method: "POST",
  })
}

/**
 * POST /api/meetings/{id}/refine — synchronous, can take minutes (batch
 * transcription, then the summary). Throws RefineError when the summary
 * step failed (the body has a kind), else a plain ApiError.
 */
export type RefineEngine = "auto" | "none" | SttEngine

/** Refine's answer: the meeting, plus — in summarize "if_ready" mode —
 *  why the summary wasn't rewritten, when it wasn't. */
export type RefineResult = Meeting & { summary_skipped?: string }

export async function refineMeeting(
  id: string,
  opts?: { engine?: RefineEngine; summarize?: "always" | "if_ready" },
): Promise<RefineResult> {
  const body: Record<string, string> = {}
  if (opts?.engine) body.engine = opts.engine
  if (opts?.summarize) body.summarize = opts.summarize
  try {
    return await apiFetch<RefineResult>(`/api/meetings/${encodeURIComponent(id)}/refine`, {
      method: "POST",
      body: Object.keys(body).length > 0 ? JSON.stringify(body) : undefined,
    })
  } catch (err) {
    throw withKind(err)
  }
}

/** withKind turns an ApiError whose body carries a kind into a RefineError;
 *  anything else is returned as is. */
function withKind(err: unknown): unknown {
  const kind = err instanceof ApiError ? err.body?.kind : undefined
  if (err instanceof ApiError && typeof kind === "string") {
    const resetAt = typeof err.body?.reset_at === "string" ? err.body.reset_at : undefined
    return new RefineError(err.message, err.status, kind as SummaryErrorKind, resetAt, err.body)
  }
  return err
}

/** importRecording opts: everything except file, which is the first argument. */
export interface ImportRecordingOptions {
  title?: string
  lang?: string
  translate_to?: string
  two_way?: boolean
  engine?: RefineEngine
  /** Project display name, added as the meeting's `project:` tag. */
  project?: string
}

/**
 * POST /api/meetings/import (multipart) — streams the file with
 * XMLHttpRequest so onUploadProgress can track it (0..1). On failure,
 * the server may have kept the meeting: the thrown RefineError-style
 * error then carries meeting_id.
 */
export function importRecording(
  file: File,
  opts: ImportRecordingOptions,
  onUploadProgress?: (fraction: number) => void,
): Promise<Meeting> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open("POST", "/api/meetings/import")
    xhr.responseType = "json"
    if (onUploadProgress) {
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) onUploadProgress(e.loaded / e.total)
      }
    }
    const fail = () => {
      const body = (xhr.response ?? undefined) as Record<string, unknown> | undefined
      const fallback = i18next.t("errors.requestFailed", { status: xhr.status })
      let message = xhr.statusText || fallback
      const kind = body?.kind
      if (typeof body?.error === "string") message = body.error
      if (typeof kind === "string") {
        const resetAt = typeof body?.reset_at === "string" ? body.reset_at : undefined
        reject(new RefineError(message, xhr.status, kind as SummaryErrorKind, resetAt, body))
        return
      }
      reject(new ApiError(message, xhr.status, body))
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(xhr.response as Meeting)
      } else {
        fail()
      }
    }
    xhr.onerror = () => reject(new ApiError(i18next.t("errors.importNetwork"), xhr.status))
    xhr.onabort = () => reject(new ApiError(i18next.t("errors.importCancelled"), xhr.status))
    // Fields before the file: the server reads parts in order, so a bad
    // engine/format choice is rejected before the whole upload is sent.
    const form = new FormData()
    if (opts.title) form.append("title", opts.title)
    if (opts.lang) form.append("lang", opts.lang)
    if (opts.translate_to) form.append("translate_to", opts.translate_to)
    if (opts.two_way !== undefined) form.append("two_way", String(opts.two_way))
    if (opts.engine) form.append("engine", opts.engine)
    if (opts.project) form.append("project", opts.project)
    form.append("file", file)
    xhr.send(form)
  })
}

/** internal/tools.ActionItemRef: an ActionItem flattened together with the
 *  meeting it came from and its index in that meeting's action_items list. */
export interface ActionItemRef extends ActionItem {
  meeting_id: string
  meeting_title: string
  /** The meeting's project display name; absent when it has none. */
  meeting_project?: string
  index: number
}

/** GET /api/action-items — every meeting's action items in one flat list. */
export async function listActionItems(): Promise<ActionItemRef[]> {
  return (await apiFetch<ActionItemRef[] | null>("/api/action-items")) ?? []
}

/**
 * Projects (docs/plans/active/projects.md): a project is the tag
 * `project:<Display Name>` in a meeting's tags, at most one per meeting.
 * These mirror internal/meeting's ProjectOf / WithProject.
 */
export const PROJECT_TAG_PREFIX = "project:"

/** Longest project name the server accepts, in characters. */
export const MAX_PROJECT_NAME = 80

/** Trim and collapse inner whitespace; case is kept (it's the display name). */
export function normalizeProject(name: string): string {
  return name.trim().split(/\s+/).filter(Boolean).join(" ")
}

/** Matching key: projects compare case-insensitively after normalizing. */
export function projectKey(name: string): string {
  return normalizeProject(name).toLowerCase()
}

/** True for a `project:` tag (prefix matched case-insensitively). */
export function isProjectTag(tag: string): boolean {
  return tag.trim().slice(0, PROJECT_TAG_PREFIX.length).toLowerCase() === PROJECT_TAG_PREFIX
}

/** The meeting's project display name, or "" when it has none. */
export function projectOf(tags: readonly string[] | undefined): string {
  for (const t of tags ?? []) {
    if (!isProjectTag(t)) continue
    const name = normalizeProject(t.trim().slice(PROJECT_TAG_PREFIX.length))
    if (name) return name
  }
  return ""
}

/** tags with any project tag replaced by `name` (put first), or removed
 *  when name is empty. The other tags keep their order. */
export function withProject(tags: readonly string[] | undefined, name: string): string[] {
  const n = normalizeProject(name)
  const rest = (tags ?? []).filter((t) => !isProjectTag(t))
  return n ? [PROJECT_TAG_PREFIX + n, ...rest] : rest
}

/** The tags that aren't the project tag. */
export function plainTags(tags: readonly string[] | undefined): string[] {
  return (tags ?? []).filter((t) => !isProjectTag(t))
}

/** GET /api/projects entry — derived from meetings' project tags. */
export interface Project {
  name: string
  meetings: number
  last_meeting_at: string
}

/** GET /api/projects — most recent meeting first. */
export async function listProjects(): Promise<Project[]> {
  return (await apiFetch<Project[] | null>("/api/projects")) ?? []
}

/** POST /api/projects/rename — moves every meeting in `from` to `to`
 *  (merging when `to` exists already). Resolves to how many moved. */
export function renameProject(from: string, to: string): Promise<{ renamed: number }> {
  return apiFetch("/api/projects/rename", {
    method: "POST",
    body: JSON.stringify({ from, to }),
  })
}

/** PATCH /api/meetings/{id}/action-items/{index} — resolves to the updated meeting. */
export function updateActionItemStatus(
  meetingId: string,
  index: number,
  status: string,
): Promise<Meeting> {
  return apiFetch(
    `/api/meetings/${encodeURIComponent(meetingId)}/action-items/${index}`,
    {
      method: "PATCH",
      body: JSON.stringify({ status }),
    },
  )
}


/** GET /api/config */
export function getConfig(): Promise<AppConfig> {
  return apiFetch("/api/config")
}

/** Fired on window with the saved AppConfig (event.detail) after every
 *  successful PUT /api/config, so views that hold their own copy (the
 *  first-run chooser in App, Get started) see a new setup at once. */
export const CONFIG_EVENT = "catchm:config"

/** PUT /api/config — resolves to the sanitized, reloaded config. */
export async function updateConfig(body: UpdateConfigRequest): Promise<AppConfig> {
  const c = await apiFetch<AppConfig>("/api/config", {
    method: "PUT",
    body: JSON.stringify(body),
  })
  window.dispatchEvent(new CustomEvent<AppConfig>(CONFIG_EVENT, { detail: c }))
  return c
}

/** GET /api/translate/status */
export function getTranslateStatus(): Promise<TranslateStatus> {
  return apiFetch("/api/translate/status")
}

/** A provider whose key POST /api/providers/{name}/verify can check
 *  (cloud engines only where CLOUD_ENGINES says `verifiable`). */
export type VerifiableProvider = "google" | "llm" | CloudSttEngine

/** POST /api/providers/{name}/verify answer: ok, or the provider's own
 *  error message (English, as the provider sent it). */
export interface VerifyResult {
  ok: boolean
  error?: string
}

/**
 * POST /api/providers/{name}/verify — checks `apiKey` (a key typed but
 * not saved yet) or, when omitted, the stored key, with the provider's
 * cheapest real call. Throws ApiError (400) when there's nothing to
 * check (no key, or the LLM has no provider/model yet).
 */
export function verifyProvider(name: VerifiableProvider, apiKey?: string): Promise<VerifyResult> {
  return apiFetch(`/api/providers/${name}/verify`, {
    method: "POST",
    body: JSON.stringify(apiKey ? { api_key: apiKey } : {}),
  })
}

/** Why GET /api/system judges self-hosting slow or not recommended
 *  (internal/sysinfo/selfhost.go), most serious first. gpu_unknown is
 *  advisory only: the verdict doesn't count it. */
export type SelfHostReason =
  | "ram_low"
  | "ram_minimum"
  | "ram_unknown"
  | "disk_low"
  | "disk_tight"
  | "cpu_few"
  | "gpu_unknown"

/** GET /api/system: this computer, and whether it can run Whisper +
 *  Ollama well. Sizes in GiB, omitted when they can't be read. */
export interface MachineCheck {
  total_ram_gb?: number
  free_disk_gb?: number
  cpu_cores: number
  os: string
  arch: string
  apple_silicon: boolean
  selfhost: "good" | "slow" | "insufficient"
  selfhost_reasons: SelfHostReason[]
}

/** GET /api/system — the machine check behind the Self-host setup. */
export function getMachineCheck(): Promise<MachineCheck> {
  return apiFetch("/api/system")
}

/** GET /api/stt/status — whether each transcription engine is ready. */
export function getSttStatus(): Promise<SttStatus> {
  return apiFetch("/api/stt/status")
}

/** GET /api/summary/status — whether summaries can run with the chosen provider. */
export function getSummaryStatus(): Promise<SummaryStatus> {
  return apiFetch("/api/summary/status")
}

/**
 * POST /api/translate/pull — streams newline-delimited JSON progress events
 * ({"status": "..."} or a terminal {"error": "..."}) and calls onProgress
 * once per parsed line. Resolves once the stream ends.
 */
export async function pullTranslateModel(
  target: PullTarget,
  onProgress: (event: PullProgressEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch("/api/translate/pull", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ target } satisfies PullTranslateModelRequest),
    signal,
  })
  if (!res.ok || !res.body) {
    throw new ApiError(res.statusText || i18next.t("errors.requestFailed", { status: res.status }), res.status)
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buf = ""

  const consumeLine = (line: string) => {
    const trimmed = line.trim()
    if (!trimmed) return
    onProgress(JSON.parse(trimmed) as PullProgressEvent)
  }

  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buf += decoder.decode(value, { stream: true })
    let idx: number
    while ((idx = buf.indexOf("\n")) >= 0) {
      consumeLine(buf.slice(0, idx))
      buf = buf.slice(idx + 1)
    }
  }
  consumeLine(buf)
}

/** POST /api/translate/test */
export function testTranslate(body: TestTranslateRequest): Promise<TestTranslateResponse> {
  return apiFetch("/api/translate/test", {
    method: "POST",
    body: JSON.stringify(body),
  })
}

/**
 * Cloud sync (M13c's /api/cloud/* routes). All crypto lives
 * server-side in internal/server/cloud.go; these are plain calls.
 */

/** internal/cloudsync.SyncResult — Go marshals nil slices as null, not []. */
export interface SyncResult {
  pushed: string[] | null
  pulled: string[] | null
  conflicted: string[] | null
}

/**
 * GET /api/cloud/status response. Union: when logged_in is false the
 * other fields are absent (handler writes only logged_in).
 * last_sync_at is the zero time ("0001-01-01T00:00:00Z") when no sync
 * has run — render as "never".
 */
export interface CloudStatus {
  logged_in: boolean
  email?: string
  logged_in_at?: string
  last_sync_at?: string
  /** The catchm-cloud instance this install talks to (M18). */
  api_base_url?: string
  /**
   * True when account params (email, salt, KDF) that a server change
   * would clear are on disk. Missing on older backends — treat as true.
   */
  account_stored?: boolean
  /** Two-factor is on, as far as this device knows (logged in only). */
  totp_enabled?: boolean
}

/** Which code the server wants before it signs in: one emailed to the
 *  account's address, or one from an authenticator app. */
export type CloudChallenge = "email_code" | "totp"

/**
 * The challenge in a failed login/recover (401 {code:"challenge",
 * challenge, reason}), or null. reason is "invalid_code" when a code was
 * sent and was wrong; empty or "totp_required" on the first ask.
 */
export function cloudChallengeOf(err: unknown): { challenge: CloudChallenge; reason: string } | null {
  if (!(err instanceof ApiError) || err.code !== "challenge") return null
  const challenge = err.body?.challenge
  if (challenge !== "email_code" && challenge !== "totp") return null
  return { challenge, reason: typeof err.body?.reason === "string" ? err.body.reason : "" }
}

/** POST /api/cloud/signup — recovery_code appears exactly once, in this
 *  response. verification_required: the account was made unverified and
 *  a code was emailed; log in with it (no session yet). A password in
 *  known breaches answers 409 {code:"password_breached", count} unless
 *  acceptBreached. */
export function cloudSignup(
  email: string,
  password: string,
  opts: { acceptBreached?: boolean } = {},
): Promise<{ email: string; recovery_code: string; verification_required?: boolean }> {
  return apiFetch("/api/cloud/signup", {
    method: "POST",
    body: JSON.stringify({ email, password, accept_breached: opts.acceptBreached || undefined }),
  })
}

/** POST /api/cloud/login. A 401 {code:"challenge"} (cloudChallengeOf)
 *  asks for emailCode or totpCode; retry with it. */
export function cloudLogin(
  email: string,
  password: string,
  codes: { emailCode?: string; totpCode?: string } = {},
): Promise<{ email: string }> {
  return apiFetch("/api/cloud/login", {
    method: "POST",
    body: JSON.stringify({ email, password, email_code: codes.emailCode, totp_code: codes.totpCode }),
  })
}

/** POST /api/cloud/login {resend:true} — email a new verification code.
 *  429 {code:"resend_throttled", retry_after} when asked too soon. */
export function cloudResendCode(email: string, password: string): Promise<{ resent: boolean }> {
  return apiFetch("/api/cloud/login", {
    method: "POST",
    body: JSON.stringify({ email, password, resend: true }),
  })
}

/** GET /api/cloud/sessions entry. Times are Unix milliseconds; current
 *  is this device's own session. */
export interface CloudSession {
  id: string
  device_name: string
  created_at: number
  last_seen_at: number
  current: boolean
}

/** GET /api/cloud/sessions — the account's signed-in devices. */
export function cloudSessions(): Promise<{ sessions: CloudSession[] }> {
  return apiFetch("/api/cloud/sessions")
}

/** DELETE /api/cloud/sessions/{id}. signed_out: it was this device's
 *  session, so this device is logged out now. */
export function cloudRevokeSession(id: string): Promise<{ signed_out: boolean }> {
  return apiFetch(`/api/cloud/sessions/${encodeURIComponent(id)}`, { method: "DELETE" })
}

/** POST /api/cloud/sessions/revoke-others — every session but this one. */
export function cloudRevokeOtherSessions(): Promise<{ revoked: number }> {
  return apiFetch("/api/cloud/sessions/revoke-others", { method: "POST" })
}

/** POST /api/cloud/totp/setup — a new authenticator secret (not on until
 *  cloudTotpEnable confirms a code). */
export function cloudTotpSetup(): Promise<{ secret: string; otpauth_uri: string }> {
  return apiFetch("/api/cloud/totp/setup", { method: "POST" })
}

/** POST /api/cloud/totp/enable — 403 {code:"invalid_code"} for a wrong code. */
export function cloudTotpEnable(code: string): Promise<{ totp_enabled: boolean }> {
  return apiFetch("/api/cloud/totp/enable", { method: "POST", body: JSON.stringify({ code }) })
}

/** POST /api/cloud/totp/disable — 403 {code:"invalid_code"} for a wrong code. */
export function cloudTotpDisable(code: string): Promise<{ totp_enabled: boolean }> {
  return apiFetch("/api/cloud/totp/disable", { method: "POST", body: JSON.stringify({ code }) })
}

/** POST /api/cloud/logout */
export function cloudLogout(): Promise<{ status: string }> {
  return apiFetch("/api/cloud/logout", { method: "POST" })
}

/** GET /api/cloud/status */
export function cloudStatus(): Promise<CloudStatus> {
  return apiFetch("/api/cloud/status")
}

/**
 * PUT /api/cloud/server — set which catchm-cloud instance this install
 * talks to (M18). Only while logged out: 409 "log out before changing
 * the cloud server" otherwise; 400 with the validation reason (e.g.
 * "http is only allowed for localhost"). Changing servers clears the
 * stored account params — the new server needs a fresh signup/login.
 */
export function cloudServer(apiBaseURL: string): Promise<{ api_base_url: string }> {
  return apiFetch("/api/cloud/server", {
    method: "PUT",
    body: JSON.stringify({ api_base_url: apiBaseURL }),
  })
}

/** POST /api/cloud/sync */
export function cloudSync(): Promise<SyncResult> {
  return apiFetch("/api/cloud/sync", { method: "POST" })
}

/** POST /api/cloud/recover — returns the NEW rotated recovery code (one-time display, same contract as signup).
 *  An unverified account answers the email_code challenge like login. */
/** POST /api/cloud/change-password: a new password for the signed-in
 *  user, proven by the current one; the recovery code is unchanged.
 *  409 password_breached asks to confirm (acceptBreached). */
export function cloudChangePassword(
  currentPassword: string,
  newPassword: string,
  opts: { signOutOthers?: boolean; acceptBreached?: boolean } = {},
): Promise<{ signed_out: number }> {
  return apiFetch("/api/cloud/change-password", {
    method: "POST",
    body: JSON.stringify({
      current_password: currentPassword,
      new_password: newPassword,
      sign_out_others: opts.signOutOthers ?? false,
      accept_breached: opts.acceptBreached || undefined,
    }),
  })
}

export function cloudRecover(
  email: string,
  recoveryCode: string,
  newPassword: string,
  emailCode?: string,
): Promise<{ email: string; recovery_code: string; two_factor_disabled?: boolean }> {
  return apiFetch("/api/cloud/recover", {
    method: "POST",
    body: JSON.stringify({ email, recovery_code: recoveryCode, new_password: newPassword, email_code: emailCode }),
  })
}

/**
 * Local MCP server (Settings → MCP). MCP clients either run
 * `<stdio.command> mcp` themselves (works while the app is closed;
 * refuses to start while `enabled` is off) or connect to the local
 * HTTP listener at `http.url` with `Authorization: Bearer <http.token>`.
 */
export interface McpSettings {
  enabled: boolean
  allow_writes: boolean
  http: {
    enabled: boolean
    port: number
    /** Empty until Local HTTP is first turned on. */
    token: string
    /** The listener is up (MCP and Local HTTP both on, port opened). */
    running: boolean
    /** Set while running, e.g. "http://127.0.0.1:4655/mcp". */
    url: string
    /** Why the listener isn't running (e.g. the port is taken). */
    error: string
  }
  stdio: { command: string; args: string[] }
}

/** PUT /api/mcp body: a partial update; omitted fields stay as they are. */
export interface UpdateMcpRequest {
  enabled?: boolean
  allow_writes?: boolean
  http_enabled?: boolean
  /** 1024–65535. */
  port?: number
}

/** GET /api/mcp */
export function getMcp(): Promise<McpSettings> {
  return apiFetch("/api/mcp")
}

/** PUT /api/mcp — turning Local HTTP on creates a token if there isn't
 *  one; the HTTP listener restarts on every change. */
export function updateMcp(patch: UpdateMcpRequest): Promise<McpSettings> {
  return apiFetch("/api/mcp", { method: "PUT", body: JSON.stringify(patch) })
}

/** POST /api/mcp/token — a new HTTP token; apps using the old one stop working. */
export function regenerateMcpToken(): Promise<McpSettings> {
  return apiFetch("/api/mcp/token", { method: "POST" })
}

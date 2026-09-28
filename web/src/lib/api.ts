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
  /** Which engine re-transcribed the meeting ("soniox" | "whisper"); empty for engine "none". */
  refined_with?: string
  /** True when the meeting was created from an uploaded recording. */
  imported?: boolean
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

export type SttEngine = "whisper" | "soniox"

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
}

/** Body for PATCH /api/meetings/{id} — at least one field required. */
export interface UpdateMeetingRequest {
  title?: string
  tags?: string[]
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

/** Response body for GET /api/translate/status. */
export interface TranslateStatus {
  ollama_reachable: boolean
  live_model: TranslateModelStatus
  batch_model: TranslateModelStatus
}

export type TranslateTarget = "live" | "batch"

/** Which Ollama model POST /api/translate/pull downloads: the live or
 *  batch translation model, or the summary model (summary.ollama_model). */
export type PullTarget = TranslateTarget | "summary"

/** One engine's readiness; reason is user-facing and set only when not ready. */
export interface SttEngineStatus {
  ready: boolean
  reason?: string
}

/** GET /api/stt/status response. */
export interface SttStatus {
  default_engine: SttEngine
  whisper: SttEngineStatus
  soniox: SttEngineStatus
}

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

/** The summary block of GET/PUT /api/config responses. The OpenAI API
 *  key is never included; see AppConfig.summary_openai_api_key_set. */
export interface SummaryConfig {
  provider: SummaryProvider
  ollama_model: string
  openai: { base_url: string; model: string }
  cloud_consent: boolean
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

/** Sanitized config shape shared by GET and PUT /api/config responses. */
export interface AppConfig {
  engine: string
  lang: string
  whisper_url: string
  soniox: { url: string }
  /** True when a Soniox API key is stored; the key itself is never sent. */
  soniox_api_key_set: boolean
  translate: {
    host: string
    live_model: string
    batch_model: string
  }
  summary: SummaryConfig
  /** True when an OpenAI-compatible API key is stored; the key itself is never sent. */
  summary_openai_api_key_set: boolean
}

/**
 * Body for PUT /api/config — a PARTIAL update: the server applies
 * setIfNonEmpty per field, so only included fields change and omitted or
 * blank ones are left alone. Every field is optional; send just what a
 * page changed, never a full snapshot (a stale snapshot would silently
 * overwrite newer values saved elsewhere). soniox_api_key is write-only
 * and never present in responses; omitting/emptying it preserves the
 * stored key.
 */
export interface UpdateConfigRequest {
  engine?: string
  lang?: string
  whisper_url?: string
  soniox?: { url: string }
  /** Never present in responses. Omitted/empty preserves the stored key. */
  soniox_api_key?: string
  translate?: {
    host: string
    live_model: string
    batch_model: string
  }
  /**
   * Partial like the rest: omitted/empty strings keep the stored value.
   * openai.api_key is write-only; omitted/empty preserves the stored key,
   * except that changing openai.base_url without a new key clears it (a
   * key is never sent to a server it wasn't entered for).
   * cloud_consent: false revokes consent.
   */
  summary?: {
    provider?: SummaryProvider
    ollama_model?: string
    openai?: { base_url?: string; model?: string; api_key?: string }
    cloud_consent?: boolean
  }
}

/** Thrown by every client function below on a non-2xx response. body is
 *  the parsed JSON error body when there was one. */
export class ApiError extends Error {
  status: number
  body?: Record<string, unknown>
  constructor(message: string, status: number, body?: Record<string, unknown>) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.body = body
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

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
  })
  if (!res.ok) {
    let message = res.statusText || `request failed with status ${res.status}`
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
    throw new ApiError(message, res.status, errBody)
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

/** PATCH /api/meetings/{id} — update title and/or tags. */
export function updateMeeting(id: string, body: UpdateMeetingRequest): Promise<Meeting> {
  return apiFetch(`/api/meetings/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(body),
  })
}

/** DELETE /api/meetings/{id} — resolves on 204. */
export function deleteMeeting(id: string): Promise<void> {
  return apiFetch(`/api/meetings/${encodeURIComponent(id)}`, {
    method: "DELETE",
  })
}

/** GET /api/meetings/{id}/transcript?cursor=N */
export async function getTranscript(id: string, cursor: number): Promise<TranscriptResponse> {
  const res = await apiFetch<TranscriptResponse>(
    `/api/meetings/${encodeURIComponent(id)}/transcript?cursor=${cursor}`,
  )
  return { utterances: res.utterances ?? [], cursor: res.cursor }
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
export type RefineEngine = "auto" | "soniox" | "whisper" | "none"

export async function refineMeeting(id: string, opts?: { engine?: RefineEngine }): Promise<Meeting> {
  try {
    return await apiFetch<Meeting>(`/api/meetings/${encodeURIComponent(id)}/refine`, {
      method: "POST",
      body: opts?.engine ? JSON.stringify({ engine: opts.engine }) : undefined,
    })
  } catch (err) {
    const kind = err instanceof ApiError ? err.body?.kind : undefined
    if (err instanceof ApiError && typeof kind === "string") {
      const resetAt = typeof err.body?.reset_at === "string" ? err.body.reset_at : undefined
      throw new RefineError(err.message, err.status, kind as SummaryErrorKind, resetAt, err.body)
    }
    throw err
  }
}

/** importRecording opts: everything except file, which is the first argument. */
export interface ImportRecordingOptions {
  title?: string
  lang?: string
  translate_to?: string
  two_way?: boolean
  engine?: RefineEngine
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
      let message = xhr.statusText || `import failed with status ${xhr.status}`
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
    xhr.onerror = () => reject(new ApiError("Import request failed (network error).", xhr.status))
    xhr.onabort = () => reject(new ApiError("Import was cancelled.", xhr.status))
    // Fields before the file: the server reads parts in order, so a bad
    // engine/format choice is rejected before the whole upload is sent.
    const form = new FormData()
    if (opts.title) form.append("title", opts.title)
    if (opts.lang) form.append("lang", opts.lang)
    if (opts.translate_to) form.append("translate_to", opts.translate_to)
    if (opts.two_way !== undefined) form.append("two_way", String(opts.two_way))
    if (opts.engine) form.append("engine", opts.engine)
    form.append("file", file)
    xhr.send(form)
  })
}

/** internal/tools.ActionItemRef: an ActionItem flattened together with the
 *  meeting it came from and its index in that meeting's action_items list. */
export interface ActionItemRef extends ActionItem {
  meeting_id: string
  meeting_title: string
  index: number
}

/** GET /api/action-items — every meeting's action items in one flat list. */
export async function listActionItems(): Promise<ActionItemRef[]> {
  return (await apiFetch<ActionItemRef[] | null>("/api/action-items")) ?? []
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

/** PUT /api/config — resolves to the sanitized, reloaded config. */
export function updateConfig(body: UpdateConfigRequest): Promise<AppConfig> {
  return apiFetch("/api/config", {
    method: "PUT",
    body: JSON.stringify(body),
  })
}

/** GET /api/translate/status */
export function getTranslateStatus(): Promise<TranslateStatus> {
  return apiFetch("/api/translate/status")
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
    throw new ApiError(res.statusText || "pull request failed", res.status)
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
}

/** POST /api/cloud/signup — recovery_code appears exactly once, in this response. */
export function cloudSignup(email: string, password: string): Promise<{ email: string; recovery_code: string }> {
  return apiFetch("/api/cloud/signup", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  })
}

/** POST /api/cloud/login */
export function cloudLogin(email: string, password: string): Promise<{ email: string }> {
  return apiFetch("/api/cloud/login", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  })
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

/** POST /api/cloud/recover — returns the NEW rotated recovery code (one-time display, same contract as signup). */
export function cloudRecover(
  email: string,
  recoveryCode: string,
  newPassword: string,
): Promise<{ email: string; recovery_code: string }> {
  return apiFetch("/api/cloud/recover", {
    method: "POST",
    body: JSON.stringify({ email, recovery_code: recoveryCode, new_password: newPassword }),
  })
}

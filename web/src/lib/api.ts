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
  lang?: string
  translate_to?: string
  decisions?: Decision[]
  action_items?: ActionItem[]
  terms?: Record<string, string>
  summary?: string
  notes?: string[]
  refined?: boolean
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

/** Body for POST /api/meetings. */
export interface StartMeetingRequest {
  title: string
  device?: string
  engine?: SttEngine | ""
  lang?: string
  translate_to?: string
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

/** Body for POST /api/translate/pull. */
export interface PullTranslateModelRequest {
  target: TranslateTarget
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
}

/** Body for PUT /api/config — like AppConfig but with the write-only
 *  soniox_api_key (optional; omitting/emptying it preserves the stored key). */
export interface UpdateConfigRequest {
  engine: string
  lang?: string
  whisper_url: string
  soniox: { url: string }
  /** Never present in responses. Omitted/empty preserves the stored key. */
  soniox_api_key?: string
  translate: {
    host: string
    live_model: string
    batch_model: string
  }
}

/** Thrown by every client function below on a non-2xx response. */
export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.name = "ApiError"
    this.status = status
  }
}

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
  })
  if (!res.ok) {
    let message = res.statusText || `request failed with status ${res.status}`
    try {
      const body: unknown = await res.json()
      if (body && typeof body === "object" && "error" in body && typeof (body as { error: unknown }).error === "string") {
        message = (body as { error: string }).error
      }
    } catch {
      // Body wasn't JSON (or was empty) — fall back to statusText above.
    }
    throw new ApiError(message, res.status)
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

/** POST /api/meetings */
export function startMeeting(body: StartMeetingRequest): Promise<Meeting> {
  return apiFetch("/api/meetings", {
    method: "POST",
    body: JSON.stringify(body),
  })
}

/** GET /api/meetings/{id} */
export function getMeeting(id: string): Promise<Meeting> {
  return apiFetch(`/api/meetings/${encodeURIComponent(id)}`)
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

/** POST /api/meetings/{id}/refine — synchronous, can take several seconds. */
export function refineMeeting(id: string): Promise<Meeting> {
  return apiFetch(`/api/meetings/${encodeURIComponent(id)}/refine`, {
    method: "POST",
  })
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

/**
 * POST /api/translate/pull — streams newline-delimited JSON progress events
 * ({"status": "..."} or a terminal {"error": "..."}) and calls onProgress
 * once per parsed line. Resolves once the stream ends.
 */
export async function pullTranslateModel(
  target: TranslateTarget,
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

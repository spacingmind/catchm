/**
 * Download sizes (GB) of the Ollama models CatchM suggests, for setup
 * copy like "a one-time download of about 6.6 GB". Ollama's status
 * endpoints don't report sizes, so this is a local table; a model that
 * isn't listed has no known size and callers leave it out.
 */
const MODEL_DOWNLOAD_GB: Record<string, number> = {
  // Translation (Settings → Translation).
  "translategemma:4b": 3.3,
  "translategemma:12b": 8.1,
  // Summaries (the presets in Settings → Summaries).
  "qwen3.5:4b": 3.4,
  "qwen3.5:9b": 6.6,
  "gemma4:12b": 7.6,
  "qwen3.5:27b": 17,
}

/** The download size of `model` in GB, or null when it isn't known. */
export function modelDownloadGb(model: string | undefined | null): number | null {
  if (!model) return null
  return MODEL_DOWNLOAD_GB[model.trim()] ?? null
}

/** "6.6 GB", "17 GB": one decimal below 10 GB, whole numbers above. */
export function formatGb(gb: number): string {
  const n = gb >= 10 ? Math.round(gb) : Math.round(gb * 10) / 10
  return `${n} GB`
}

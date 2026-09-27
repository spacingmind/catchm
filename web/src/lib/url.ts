/**
 * True when v parses as an absolute URL whose scheme is one of
 * `protocols` (given with or without the trailing colon, e.g. "wss" or
 * "wss:"). Stricter than `new URL` alone, which happily accepts
 * "localhost:8080" as scheme "localhost:".
 */
export function isAbsoluteUrl(v: string, protocols: string[]): boolean {
  let url: URL
  try {
    url = new URL(v.trim())
  } catch {
    return false
  }
  const allowed = protocols.map((p) => (p.endsWith(":") ? p : `${p}:`).toLowerCase())
  return allowed.includes(url.protocol) && url.host !== ""
}

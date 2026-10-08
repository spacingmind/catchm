// Download data for the page. Fetched once at build time from the public
// update API (GitHub-Releases-shaped JSON); a hard-coded v0.5.0 snapshot
// stands in whenever the API is down or its shape surprises us, so the
// build never fails. The response `body` is ignored on purpose: it links
// to the private repo.

const API = "https://catchm-api.spacingmind.com/update/releases/latest";
const DOWNLOAD_PREFIX = "https://catchm-api.spacingmind.com/update/download/";

export type Download = {
  name: string;
  url: string;
  size: number;
  sha256Url: string;
};

export type Release = {
  version: string; // "0.5.0"
  publishedAt: string; // ISO
  windows: Download;
  mac: Download;
  source: "api" | "fallback";
};

const base = `${DOWNLOAD_PREFIX}v0.5.0/`;

export const FALLBACK_RELEASE: Release = {
  version: "0.5.0",
  publishedAt: "2026-10-05T15:04:31Z",
  windows: {
    name: "catchm-desktop-amd64-installer.exe",
    url: `${base}catchm-desktop-amd64-installer.exe`,
    size: 8103775,
    sha256Url: `${base}catchm-desktop-amd64-installer.exe.sha256`,
  },
  mac: {
    name: "catchm-desktop-arm64.dmg",
    url: `${base}catchm-desktop-arm64.dmg`,
    size: 6159338,
    sha256Url: `${base}catchm-desktop-arm64.dmg.sha256`,
  },
  source: "fallback",
};

type ApiAsset = { name?: unknown; size?: unknown; browser_download_url?: unknown };

function pick(assets: ApiAsset[], suffix: string): Download | null {
  const file = assets.find(
    (a) => typeof a.name === "string" && a.name.endsWith(suffix),
  );
  const sha = assets.find(
    (a) => typeof a.name === "string" && a.name === `${file?.name}.sha256`,
  );
  if (!file || !sha) return null;
  const url = file.browser_download_url;
  const sha256Url = sha.browser_download_url;
  if (
    typeof url !== "string" ||
    typeof sha256Url !== "string" ||
    typeof file.size !== "number" ||
    !url.startsWith(DOWNLOAD_PREFIX) ||
    !sha256Url.startsWith(DOWNLOAD_PREFIX)
  ) {
    return null;
  }
  return { name: file.name as string, url, size: file.size, sha256Url };
}

export async function getRelease(): Promise<Release> {
  // CATCHM_RELEASE_API lets the fallback path be exercised locally.
  const endpoint = process.env.CATCHM_RELEASE_API || API;
  try {
    const res = await fetch(endpoint, {
      signal: AbortSignal.timeout(10_000),
      headers: { accept: "application/json" },
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const data = (await res.json()) as {
      tag_name?: unknown;
      published_at?: unknown;
      draft?: unknown;
      prerelease?: unknown;
      assets?: unknown;
    };
    if (data.draft || data.prerelease) throw new Error("not a stable release");
    const tag = data.tag_name;
    if (typeof tag !== "string" || !/^v\d+\.\d+\.\d+$/.test(tag)) {
      throw new Error(`unexpected tag ${String(tag)}`);
    }
    if (
      typeof data.published_at !== "string" ||
      Number.isNaN(Date.parse(data.published_at)) ||
      !Array.isArray(data.assets)
    ) {
      throw new Error("unexpected shape");
    }
    const windows = pick(data.assets, "-installer.exe");
    const mac = pick(data.assets, ".dmg");
    if (!windows || !mac) throw new Error("missing assets");
    return {
      version: tag.slice(1),
      publishedAt: data.published_at,
      windows,
      mac,
      source: "api",
    };
  } catch (err) {
    console.warn(
      `[release] using fallback v${FALLBACK_RELEASE.version}: ${
        err instanceof Error ? err.message : String(err)
      }`,
    );
    return FALLBACK_RELEASE;
  }
}

export function formatSize(bytes: number): string {
  return `${(bytes / 1_000_000).toFixed(1)} MB`;
}

export function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString("en-US", {
    year: "numeric",
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  });
}

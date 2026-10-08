// Serves the static export in ../out and keeps everything that isn't the
// production site out of search engines: per-PR previews live on
// *.workers.dev, and `wrangler dev` on localhost.
const PRODUCTION_HOSTS = new Set(["catchm.spacingmind.com"]);
const NOINDEX = "noindex, nofollow";

interface Env {
  ASSETS: Fetcher;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    if (PRODUCTION_HOSTS.has(url.hostname)) return env.ASSETS.fetch(request);

    if (url.pathname === "/robots.txt") {
      return new Response("User-agent: *\nDisallow: /\n", {
        headers: {
          "content-type": "text/plain; charset=utf-8",
          "x-robots-tag": NOINDEX,
        },
      });
    }

    const response = await env.ASSETS.fetch(request);
    const headers = new Headers(response.headers);
    headers.set("X-Robots-Tag", NOINDEX);
    return new Response(response.body, {
      status: response.status,
      statusText: response.statusText,
      headers,
    });
  },
} satisfies ExportedHandler<Env>;

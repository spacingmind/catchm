# CatchM landing page

Static Next.js 15 + Tailwind v4 site for **catchm.spacingmind.com**, served
by a Cloudflare Worker (`catchm-web`) with static assets. Separate from
`web/` (the app UI); nothing here touches the Go code.

```sh
bun install
bun run dev         # http://localhost:3002
bun run typecheck
bun run build       # static export to out/
bun run preview     # build + wrangler dev (local Worker)
```

## How it works

- `src/lib/release.ts` reads the public update API
  (`catchm-api.spacingmind.com/update/releases/latest`) **at build time**
  for the version, date, sizes and download links. If the API is down or
  its shape changes, it falls back to a hard-coded v0.5.0, so a build
  never fails. Set `CATCHM_RELEASE_API` to point at another URL (used to
  test the fallback). Redeploy after a release to refresh the page.
- The server HTML carries both download buttons; the client only picks
  the visitor's OS afterwards (`src/lib/use-os.ts`).
- `worker/index.ts` serves `out/` and adds `X-Robots-Tag: noindex` plus a
  Disallow-all `robots.txt` on every host except `catchm.spacingmind.com`
  (PR previews, localhost).
- `public/_headers` sets security and cache headers.
- Screenshots in `public/shots/` are AVIF + WebP derived from
  `docs/screenshots/`. `scripts/og.html` is the source of the OG image.

## Testing the Worker locally

`wrangler dev` answers with the first route's host, so the production
host behaves like production. Pass `--host` to pretend to be a preview:

```sh
bunx wrangler dev                                        # production rules
bunx wrangler dev --host pr-1-catchm-web.x.workers.dev   # noindex
```

## Deploy

CI does it: `.github/workflows/site.yml` (production on push to `develop`,
a preview per pull request). It needs the `CLOUDFLARE_API_TOKEN` and
`CLOUDFLARE_ACCOUNT_ID` repo secrets. The custom domain comes from
`wrangler.jsonc`.

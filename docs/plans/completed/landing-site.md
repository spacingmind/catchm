# Public landing page (catchm.spacingmind.com)

- Status: done — live at catchm.spacingmind.com. v0.6.0 copy (Cloud BYOK, Beta / Coming soon chips) deployed 2026-11-27. The OG image still says "mind maps" (owner: leave it).

## Problem

CatchM has no public face. The repo is private, so the download links
live only in the app's update banner and the README. People need one
page that says what CatchM is and gives them the installer.

## Decisions (owner brief, 2026-10-08)

- **Self-contained `site/` folder.** It doesn't touch `web/` (the app UI)
  or any Go code.
- **Next.js 15 static export** (`output: "export"`) on Tailwind v4,
  served by a Cloudflare Worker with static assets (`catchm-web`, custom
  domain `catchm.spacingmind.com`, `preview_urls: true`). Same family as
  spacing-mind's site, without OpenNext: nothing here needs a server.
- **Downloads come from the public update API**
  (`catchm-api.spacingmind.com/update/releases/latest`), fetched at build
  time with a hard-coded v0.5.0 fallback so a build never fails. The
  response `body` is ignored (it links to the private repo). The page
  never links to the GitHub repo, releases or PRs.
- **Only shipped features.** Every claim is checked against
  `origin/develop` (v0.5.0 contains all of it). Not shipped, so left out
  or marked "coming soon": an MCP server, macOS system audio.
- **Page order (owner feedback, 2026-10-08):** after the meeting, not
  during it. Hero → how it works → summary → mind map → plain files →
  privacy → the rest → live transcript and translation → downloads. The
  mind map is shown as an SVG drawing of the real `MindMapView` (no
  screenshot of it exists), and CatchM Cloud's lack of topics (so: no map)
  is stated on the page.
- English only; short copy. Brand is chestnut `#6C3524` + cream
  `#FBF7EA`; light first, dark follows the system.
- **CI:** `.github/workflows/site.yml`, path-filtered to `site/**`.
  Production on push to `develop` (see Findings), per-PR preview via
  `wrangler versions upload --preview-alias pr-<n>` plus a PR comment.
- Previews send `X-Robots-Tag: noindex`; production doesn't. Static
  assets can't branch on host in `_headers`, so a small Worker script
  wraps the asset binding and adds the header for any host other than
  `catchm.spacingmind.com`.

## Findings

- `develop` is the integration branch and `master` only fast-forwards to
  release tags (CONTRIBUTING.md). Site copy changes shouldn't wait for an
  app release, and the site isn't versioned with the app, so production
  deploys from `develop`. The download data is re-fetched at each
  deploy, so a new release is picked up on the next site deploy (or a
  manual `workflow_dispatch` after tagging).
- Repo secrets: `CLOUDFLARE_ACCOUNT_ID` exists; `CLOUDFLARE_API_TOKEN`
  does not. The workflow can't deploy until the owner adds it.

## Acceptance criteria

- [x] `bun run typecheck` and `bun run build` pass in `site/` (from a clean
      checkout: no `.next`, no `next-env.d.ts`).
- [x] Content is visible with JS disabled; both download buttons render
      in the server HTML; the client only reorders/highlights.
- [x] Visible focus, alt text, `prefers-reduced-motion` respected; axe
      (WCAG 2 A/AA + best practice) clean at 1440 and 390, light and dark.
- [x] Zero console errors at 1440 and 390 px, light and dark.
- [x] `wrangler dev --host catchm.spacingmind.com`: no `X-Robots-Tag`; on
      a fake `pr-7-catchm-web.example.workers.dev` host: `X-Robots-Tag:
      noindex, nofollow` on pages, assets and 404s, and a Disallow-all
      `robots.txt`.
- [x] Download URLs return 200; with the API unreachable, or returning an
      unexpected shape or foreign hosts, the build falls back to v0.5.0.
- [x] sitemap.xml, robots.txt, OG image, favicon and JSON-LD
      `SoftwareApplication` present.
- [x] No link to the private repo anywhere in the built output.

## Not done on purpose

- No deploy, no DNS or custom-domain records, no Cloudflare or repo
  settings or secrets changes. `CLOUDFLARE_API_TOKEN` is missing from the
  repo secrets, so `site.yml` builds and typechecks but skips the
  Cloudflare steps (with a warning) until the owner adds it.
- `release.yml` doesn't yet dispatch `catchm-release` to refresh the page
  after a release; until it does, run `site` manually (workflow_dispatch).
- The screenshots come from `docs/screenshots/` (M20/M21) and show sample
  data. Retake them if the UI changes enough to look dated.

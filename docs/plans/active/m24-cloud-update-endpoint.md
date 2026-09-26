# M24: Serve updates from catchm-cloud + Free-plan CPU hardening

## Why

M23's updater calls `https://api.github.com/repos/spacingmind/catchm/releases/latest`
unauthenticated. The repo is **private**, so GitHub answers 404, the client maps
that to `ErrNoRelease`, and users silently never see an update. The mock-server
E2E in M23 could not catch this.

Decision (user, 2026-09-26): serve releases from `catchm-cloud` (Cloudflare
Worker + R2), and keep every Worker request under the **Free plan's 10 ms CPU**
limit.

## Part A: catchm-cloud: update endpoints

### Storage

- New R2 bucket, binding `RELEASES`, bucket name `catchm-releases`. Keep it
  separate from `BLOBS` (user data) so release objects never count against
  quotas and a bug in one path can't reach the other.
- Layout:
  - `v<X.Y.Z>/catchm-desktop-amd64-installer.exe`
  - `v<X.Y.Z>/catchm-desktop-amd64-installer.exe.sha256` (`sha256sum` format:
    `<hex>  <name>`)
  - `latest.json`: the manifest, written **last** by the publisher so a
    half-uploaded release is never advertised.
- `latest.json` shape (stored), asset paths relative:

  ```json
  {
    "tag_name": "v1.2.3",
    "name": "CatchM v1.2.3",
    "body": "release notes markdown",
    "published_at": "2026-09-26T00:00:00Z",
    "assets": [
      { "name": "catchm-desktop-amd64-installer.exe", "size": 12345678 },
      { "name": "catchm-desktop-amd64-installer.exe.sha256", "size": 101 }
    ]
  }
  ```

### Endpoints (public, unauthenticated, GET/HEAD only)

The response is **GitHub-Releases-shaped** on purpose: the M23 client
(`internal/update.GitHubClient`) already parses that shape, so the client
change is only its base URL.

1. `GET /update/releases/latest`
   - Reads `latest.json` from `RELEASES`. Missing → **404** `{"error":"no release"}`
     (the client already maps 404 → `ErrNoRelease`).
   - 200 body = stored manifest plus `"draft": false`, `"prerelease": false`,
     `"html_url": ""`, and each asset gets
     `"browser_download_url": "<request origin>/update/download/<tag_name>/<name>"`.
   - Stored JSON that fails to parse or lacks `tag_name` → 500 (log it), and
     never a half-built response.
   - `Cache-Control: public, max-age=300`.
2. `GET /update/download/:tag/:name`
   - `:tag` must match `^v[0-9]+\.[0-9]+\.[0-9]+$`; `:name` must be one of the two
     known asset names. Anything else → 404 (no path traversal, no arbitrary
     R2 reads).
   - Streams `object.body` straight from R2 (never `arrayBuffer()`), with
     `Content-Length`, `Content-Type: application/octet-stream`, `ETag`,
     `Cache-Control: public, max-age=31536000, immutable`.
   - Missing object → 404.
3. No publish endpoint on the Worker. Publishing is CI-only via
   `wrangler r2 object put` (Part C), so the Worker gets no write/auth
   surface for releases.
4. No rate limiter on these routes: they're cheap and cacheable, and the IP
   limiter budget belongs to auth routes.

## Part B: catchm-cloud: keep every request under 10 ms CPU

Measured (Node WebCrypto on the dev box): SHA-256 costs ~1.8 ms/MB, so 10 MB
is ~17 ms and already over the Free limit. The PUT path is the only
size-dependent CPU cost.

1. **PUT blob: stream to R2 and let R2 verify the checksum.** Replace
   `await request.arrayBuffer()` + `sha256Hex(body)` (`src/index.ts` ~431) with
   `env.BLOBS.put(r2Key, request.body, { sha256: checksumHeader })`.
   - Require `Content-Length` (R2 needs a known-length stream). Missing or
     malformed → 411/400. Use it as `size_bytes` for the quota reservation.
   - Validate `X-Checksum` is 64 lowercase hex **before** any work.
   - R2 checksum mismatch rejects the put → return **400 `checksum mismatch`**
     (same contract as today) and release the quota reservation. Per-write
     unique R2 keys already guarantee a failed put never clobbers the current
     object. Keep the version-guard / 409 / race semantics exactly as now.
     Existing `races.test.ts` and `quota.test.ts` must stay green.
2. **Per-blob size cap:** `MAX_BLOB_BYTES` var, default 5 MiB (5242880).
   `Content-Length` over the cap → **413** before reading the body.
3. **GET blob: stream** `object.body` instead of `await object.arrayBuffer()`
   (~line 648). Keep the headers.
4. **List blobs: paginate.** `LIMIT` (default 500, max 1000) +
   `?cursor=<last id>` using `WHERE id > ?` ordered by id. Response adds
   `"next_cursor"` (null when done). The client in `catchm` must follow
   `next_cursor`; check `internal/cloudsync` and update it in Part C if it
   lists blobs.
5. Don't add CPU-heavy work (hashing, compression, JSON over unbounded data)
   to any Worker path.

## Part C: catchm (client + release pipeline)

1. `cmd/catchm-desktop/update.go`: replace `updateRepo` with
   `var updateBaseURL = ""`, set via ldflags
   `-X main.updateBaseURL=<cloud origin>/update`. **Empty → background and
   manual checks are disabled** (manual check reports "updates not configured
   for this build"), so dev builds never hit the network. Keep the
   `CATCHM_UPDATE_BASE_URL` override. Don't read the base from the user's
   config: sync can point at a self-hosted server, but updates must come
   from the official origin.
2. `Taskfile.yml desktop:release`: accept `UPDATE_BASE_URL` and pass it in
   ldflags.
3. `.github/workflows/release.yml`: after building + computing the checksum,
   upload to R2 with `wrangler r2 object put catchm-releases/<key> --remote`
   (installer, then `.sha256`, then `latest.json` **last**). Secrets:
   `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID`. Variable: `UPDATE_BASE_URL`.
   Keep the GitHub release step as an internal record, since it's harmless on a
   private repo.
4. If `internal/cloudsync` lists blobs, follow `next_cursor` (Part B.4).
5. Move `docs/plans/active/m23-auto-update.md` to `docs/plans/completed/`.

## Validation

- catchm-cloud `npm run typecheck` + `npm test` green, with new tests for:
  latest (200 shape and absolute URLs, 404 when no manifest, 500 on bad
  manifest), download (valid stream, bad tag/name 404, missing 404), PUT
  (missing Content-Length, over cap 413, checksum mismatch 400 plus quota
  released, happy path unchanged), GET streams, list pagination.
- **Real `wrangler dev` E2E** (local, `--local` R2/D1): seed `RELEASES` with a
  fake manifest + installer + `.sha256`, then point a real catchm build's
  `internal/update` at it (`CATCHM_UPDATE_BASE_URL=http://127.0.0.1:8787/update`)
  and run `CheckForUpdate` + `DownloadAndVerify` from a Go test or small
  harness: success path, plus a deliberately wrong `.sha256` that must be
  rejected and never returned as an executable path. Also run a sync
  round-trip (signup, PUT, GET, list) against `wrangler dev`.
- catchm `go build ./... && go vet ./... && go test ./...` green; web build
  green.
- CPU: after deploy (separate step, needs `wrangler login` by the user), check
  Workers Metrics CPU p99 < 10 ms and no error 1102.

## Out of scope

- Deploying (wrangler isn't logged in; the user will run `wrangler login`
  and approve the first deploy).
- Cutting a real release tag.
- Delta updates, channels (beta), rollback.

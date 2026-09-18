# Research: Cloud sync (BYOS) reference

- Status: reference
- Date: 2026-09-18
- Source: Perplexity research (pplx)

## Precedents

| | Obsidian Sync | Standard Notes | Joplin Cloud |
|---|---|---|---|
| Price | $4-8/mo | $90-120/yr | €3-10/mo |
| E2EE | default | always | opt-in master key |
| Sync model | whole-file + delta transfer | item + snapshot/delta | item + 3-phase delta |
| Conflicts | LWW + version history + diff UI | LWW + conflict copy | LWW + conflict folder |
| CRDT | no | no | no |

None use CRDTs. For one-writer-per-meeting files, LWW + version history
+ conflict copies is the proven trade-off.

## Three self-host tiers

1. **BYOS (bring your own storage)** — client-side E2EE, push encrypted
   blobs to storage the user already has. Zero server operation.
   Precedent: Joplin sync targets (WebDAV, Nextcloud, S3, Dropbox,
   OneDrive).
2. **Folder sync escape hatch** — sync target is a local folder the user
   already syncs with Syncthing/iCloud/Dropbox. No network code at all.
3. **Own sync server** — Go server + accounts + encrypted blobs
   (Obsidian Sync model). Only if revenue is wanted later; same engine,
   one more target.

## Recommended architecture

```
internal/sync/
├─ sync.go            engine: manifest, LWW, conflict copy, versioning
├─ crypto.go          XChaCha20-Poly1305, passphrase → key via Argon2id
├─ target.go          interface SyncTarget { Put, Get, List, Delete }
├─ target_webdav.go   Nextcloud, Synology, NAS
├─ target_s3.go       R2 (free egress), B2, MinIO
└─ target_fs.go       local folder (Syncthing/iCloud/Dropbox folder)
```

Server is dumb storage; all sync + crypto logic lives in the Go client.

Protocol sketch: client manifest {file_id, version, checksum, mtime};
upload with if_version optimistic concurrency; server keeps highest
version + N prior versions; on conflict, canonical = server version,
local divergent copy saved as `<name>.catchm-conflict-<ts>.md`.

Whole-file first (meeting notes are tens-to-hundreds KB); delta transfer
deferred until real bandwidth complaints. JSONL is append-only and syncs
by cursor/seq, which is simpler than markdown.

## Caveats (from research)

- WebDAV behavior varies per server (locking, PROPFIND) — do conflict
  resolution at app level only; never trust server locking.
- S3: handle pagination, conditional writes (ETag If-Match), and
  provider differences in versioning.
- Filenames and metadata should also be encrypted or non-sensitive
  (file_id = content-derived random ID).

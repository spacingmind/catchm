// Package web embeds the built web UI (web/dist) so the Go binary can
// serve it directly, with no separate frontend server needed in
// production. `//go:embed` patterns can't escape their source file's own
// directory (no "../"), so this tiny package lives at the root of web/
// itself rather than under internal/ — that's the only way to embed
// web/dist without a build-time copy step or an unsupported symlink.
//
// `task web:build` (or `cd web && bun run build`) must run before
// `go build`/`go test` to produce real assets; a placeholder
// web/dist/index.html is committed so this package always compiles, even
// in a fresh checkout before the frontend has ever been built.
package web

import "embed"

// DistFS holds the built web UI, rooted at "dist" (i.e. DistFS's entries
// are "dist/index.html", "dist/assets/...", etc.). Callers typically strip
// the "dist" prefix with fs.Sub before serving it.
//
//go:embed all:dist
var DistFS embed.FS

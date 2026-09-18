package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	web "github.com/spacingmind/catchm/web"
)

// webUIHandler serves the embedded, built web UI (web/dist) with SPA
// fallback: any request path that isn't a real file under dist (e.g. a
// client-only view like "/whatever-the-UI-thinks-it-is-showing") serves
// index.html instead of 404ing, since the UI has no server-side routes to
// match against (see docs/plans/active/m4-web-ui.md's "no router"
// decision). Registered against Server.mux under "/", which Go's ServeMux
// only falls back to once none of the more specific /healthz or /api/*
// patterns match — so this never shadows the existing API.
func webUIHandler() http.Handler {
	sub, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		// dist is embedded at compile time via web/embed.go; a broken
		// embed directive would already fail the build, so this can't
		// happen at runtime.
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "."
		}
		if _, err := fs.Stat(sub, name); err != nil {
			// Unknown path (e.g. a hard refresh on client-only state):
			// fall back to the app shell instead of 404ing.
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

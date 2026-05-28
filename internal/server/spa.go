package server

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	lgb "github.com/fgjcarlos/lgb"
)

// serveSPA returns an http.Handler that serves files from assets and falls
// back to index.html for any path that does not resolve to a real file.
// Callers must supply an FS rooted at the SPA output directory (e.g. a sub-FS
// of frontend/dist).
//
// The handler does NOT need to special-case API routes: registerAPIRoutes
// installs method-qualified patterns on the same ServeMux, which take
// precedence over the bare "/" catch-all used to mount this handler.
func serveSPA(assets fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if f, err := assets.Open(path); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// Fallback: serve index.html so React Router can take over.
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

// mountSPA wires the embedded frontend/dist tree onto mux as the catch-all
// route. When the asset tree is unavailable (e.g. -tags no_embed builds with
// an empty embed.FS), the call logs a warning and leaves the mux unchanged so
// that requests for unknown paths fall through to the default 404 handler.
func (s *Server) mountSPA(mux *http.ServeMux) {
	sub, err := fs.Sub(lgb.Assets, "frontend/dist")
	if err != nil {
		s.log.Warn("SPA assets not available", slog.String("error", err.Error()))
		return
	}
	// If the sub-FS has no entries (e.g. no_embed build), skip mounting.
	if entries, err := fs.ReadDir(sub, "."); err != nil || len(entries) == 0 {
		s.log.Warn("SPA assets directory empty — skipping mount")
		return
	}
	mux.Handle("/", serveSPA(sub))
}

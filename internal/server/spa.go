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
// registerAPIRoutes installs method-qualified patterns on the same ServeMux,
// which take precedence over the bare "/" catch-all used to mount this
// handler. An /api request still reaches it when the path is unknown or the
// method is not registered for it (the catch-all wins over the mux's 405);
// both get a JSON 404 instead of the index.html shell.
func serveSPA(assets fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			writeAPIError(w, http.StatusNotFound, "not_found", "API route not found")
			return
		}
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
// route. When the asset tree lacks an index.html (e.g. backend-only builds
// where dist only contains the committed .gitkeep placeholder), the call logs
// a warning and leaves the mux unchanged so that requests for unknown paths
// fall through to the default 404 handler.
func (s *Server) mountSPA(mux *http.ServeMux) {
	sub, err := fs.Sub(lgb.Assets, "frontend/dist")
	if err != nil {
		s.log.Warn("SPA assets not available", slog.String("error", err.Error()))
		return
	}
	f, err := sub.Open("index.html")
	if err != nil {
		s.log.Warn("SPA assets missing index.html — skipping mount")
		return
	}
	_ = f.Close()
	mux.Handle("/", serveSPA(sub))
}

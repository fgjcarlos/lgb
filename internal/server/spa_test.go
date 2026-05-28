package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// spaTestFS returns a small fstest.MapFS that mimics the layout of
// frontend/dist after a Vite build: an index.html plus a sample JS asset.
func spaTestFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!doctype html><html><body><div id="root"></div></body></html>`),
		},
		"assets/app.js": &fstest.MapFile{
			Data: []byte(`console.log("hello");`),
		},
	}
}

// TestServeSPA_StaticFile verifies that a request for an existing asset path
// returns 200 with the file's bytes and the appropriate Content-Type. This
// covers SC-SPA-1.
func TestServeSPA_StaticFile(t *testing.T) {
	h := serveSPA(spaTestFS())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/javascript") && !strings.HasPrefix(ct, "application/javascript") {
		t.Errorf("expected JS Content-Type, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "console.log") {
		t.Errorf("expected JS body, got %q", rec.Body.String())
	}
}

// TestServeSPA_FallbackToIndex verifies that an arbitrary non-file path (a
// client-side route) returns 200 with the index.html body so that React Router
// can take over. This covers SC-SPA-2.
func TestServeSPA_FallbackToIndex(t *testing.T) {
	h := serveSPA(spaTestFS())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Errorf("expected index.html body containing root div, got %q", rec.Body.String())
	}
}

// TestServeSPA_APIRouteNotIntercepted mounts both API routes and the SPA on
// the same ServeMux and verifies that the API route is matched first — i.e.
// GET /api/tags/current returns the JSON payload, NOT the index.html shell.
// This covers SC-SPA-3.
func TestServeSPA_APIRouteNotIntercepted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/tags/current", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"pagination":{"limit":100,"offset":0,"count":0}}`)
	})
	mux.Handle("/", serveSPA(spaTestFS()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tags/current", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected application/json Content-Type, got %q", ct)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<div") || strings.Contains(body, "<html") {
		t.Errorf("API route returned HTML — SPA handler intercepted; body: %q", body)
	}
}

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/auth"
)

// TestWithMiddleware_SinglePassthrough verifies that a single middleware wraps
// the handler correctly and passes the request through.
func TestWithMiddleware_SinglePassthrough(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	var mwCalled bool
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mwCalled = true
			next.ServeHTTP(w, r)
		})
	}

	h := withMiddleware(inner, mw)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)

	if !mwCalled {
		t.Error("expected middleware to be called")
	}
	if !called {
		t.Error("expected inner handler to be called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// TestWithMiddleware_ChainOrder verifies that middleware is applied in the
// correct order: the first middleware in the list is the outermost wrapper,
// so it runs first on the way in. The chain order must be mw1 → mw2 → handler.
func TestWithMiddleware_ChainOrder(t *testing.T) {
	var order []string

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	})

	mw1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw1")
			next.ServeHTTP(w, r)
		})
	}

	mw2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw2")
			next.ServeHTTP(w, r)
		})
	}

	h := withMiddleware(inner, mw1, mw2)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)

	want := []string{"mw1", "mw2", "handler"}
	if len(order) != len(want) {
		t.Fatalf("expected call order %v, got %v", want, order)
	}
	for i, v := range want {
		if order[i] != v {
			t.Errorf("order[%d]: expected %q, got %q", i, v, order[i])
		}
	}
}

// TestWithMiddleware_NoMiddleware verifies that passing no middleware returns the
// handler unchanged (i.e., it still responds correctly).
func TestWithMiddleware_NoMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	h := withMiddleware(inner)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}
}

// ─── REST-TAGS-1: auth gate on /api/tags/current ─────────────────────────────

// TestCurrentTagsAuth_NoTokenReturns401 verifies that when a TokenService is
// configured, requests to GET /api/tags/current without a Bearer token are
// rejected with 401.
func TestCurrentTagsAuth_NoTokenReturns401(t *testing.T) {
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, baseURL, stop := startAPITestServerWithOpts(t, &snapshotPLCManager{},
		Opts{AuthTokens: tokens})
	defer stop()

	resp, err := http.Get(baseURL + "/api/tags/current")
	if err != nil {
		t.Fatalf("GET /api/tags/current: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// TestCurrentTagsAuth_ValidViewerTokenReturns200 verifies that a viewer-role
// token is accepted on GET /api/tags/current when a TokenService is
// configured.
func TestCurrentTagsAuth_ValidViewerTokenReturns200(t *testing.T) {
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, baseURL, stop := startAPITestServerWithOpts(t, &snapshotPLCManager{},
		Opts{AuthTokens: tokens})
	defer stop()

	token, err := tokens.Issue(1, "viewer1", auth.RoleViewer)
	if err != nil {
		t.Fatalf("issue viewer token: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/tags/current", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/tags/current: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// TestCurrentTagsAuth_NilTokenServiceAllowsAccess verifies the legacy
// behaviour: when no TokenService is configured (s.authTokens == nil), the
// endpoint is publicly accessible without a token.
func TestCurrentTagsAuth_NilTokenServiceAllowsAccess(t *testing.T) {
	_, baseURL, stop := startAPITestServer(t, &snapshotPLCManager{})
	defer stop()

	resp, err := http.Get(baseURL + "/api/tags/current")
	if err != nil {
		t.Fatalf("GET /api/tags/current: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ─── R75-2: Security headers on API responses ───────────────────────────────

// TestSecurityHeadersOnAPI asserts R75-2a: all four required security headers
// are present on an API response, and X-XSS-Protection is absent (R75-2c).
func TestSecurityHeadersOnAPI(t *testing.T) {
	_, baseURL, stop := startAPITestServerWithOptsAndCfgFn(t, &snapshotPLCManager{}, Opts{}, nil)
	defer stop()

	resp, err := http.Get(baseURL + "/api/doctor")
	if err != nil {
		t.Fatalf("GET /api/doctor: %v", err)
	}
	defer resp.Body.Close()

	assertSecurityHeaders(t, resp)

	// R75-2c: X-XSS-Protection MUST NOT be set (deprecated, introduces IE vulns).
	if v := resp.Header.Get("X-XSS-Protection"); v != "" {
		t.Errorf("X-XSS-Protection must not be present, got %q", v)
	}
}

// ─── R75-4: Historian auth flatten ──────────────────────────────────────────

// TestHistorianAuthFlat asserts R75-4: historian route enforces auth whenever
// authTokens != nil, regardless of histStore nil-ness.
func TestHistorianAuthFlat(t *testing.T) {
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)

	tests := []struct {
		name       string
		histStore  bool // true = real store; false = nil
		withToken  bool
		wantStatus int
	}{
		// R75-4a: no token + histStore nil → 401 (not 503)
		{name: "no_token_nil_store", histStore: false, withToken: false, wantStatus: http.StatusUnauthorized},
		// R75-4b: valid token + histStore nil → 503 (historian not configured)
		{name: "valid_token_nil_store", histStore: false, withToken: true, wantStatus: http.StatusServiceUnavailable},
		// R75-4c: no token + histStore set → 401
		{name: "no_token_real_store", histStore: true, withToken: false, wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			opts := Opts{AuthTokens: tokens}
			_, baseURL, stop := startAPITestServerWithOptsAndCfgFn(
				t, &snapshotPLCManager{}, opts, nil)
			defer stop()

			req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/historian/query?tag=Speed", nil)
			if tt.withToken {
				tok, err := tokens.Issue(1, "viewer1", auth.RoleViewer)
				if err != nil {
					t.Fatalf("issue token: %v", err)
				}
				req.Header.Set("Authorization", "Bearer "+tok)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET historian/query: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("expected %d, got %d", tt.wantStatus, resp.StatusCode)
			}
		})
	}
}

// assertSecurityHeaders checks the four required headers (R75-2a).
func assertSecurityHeaders(t *testing.T, resp *http.Response) {
	t.Helper()
	const wantCSP = "default-src 'self'; script-src 'self'; style-src 'self'; " +
		"img-src 'self' data:; connect-src 'self' ws: wss:; " +
		"font-src 'self'; object-src 'none'; frame-ancestors 'none'"

	checks := []struct{ header, want string }{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
		{"Content-Security-Policy", wantCSP},
	}
	for _, c := range checks {
		if got := resp.Header.Get(c.header); got != c.want {
			t.Errorf("header %q = %q; want %q", c.header, got, c.want)
		}
	}
}

// TestWithMiddleware_MiddlewareCanShortCircuit verifies that a middleware can
// reject a request without calling the inner handler (e.g., auth check).
func TestWithMiddleware_MiddlewareCanShortCircuit(t *testing.T) {
	innerCalled := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	blockingMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			// does NOT call next
		})
	}

	h := withMiddleware(inner, blockingMW)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)

	if innerCalled {
		t.Error("inner handler must not be called when middleware short-circuits")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

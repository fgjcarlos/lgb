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

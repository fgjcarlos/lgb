package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMiddleware_ValidToken(t *testing.T) {
	ts := NewTokenService("test-secret-32bytes-long!!", 8*time.Hour)
	token, _ := ts.Issue(1, "alice", RoleAdmin)

	handler := Middleware(ts)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok {
			t.Error("expected claims in context")
			return
		}
		if claims.Username != "alice" {
			t.Errorf("Username = %q; want alice", claims.Username)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
}

func TestMiddleware_MissingToken(t *testing.T) {
	ts := NewTokenService("test-secret-32bytes-long!!", 8*time.Hour)
	handler := Middleware(ts)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rec.Code)
	}
}

func TestMiddleware_InvalidToken(t *testing.T) {
	ts := NewTokenService("test-secret-32bytes-long!!", 8*time.Hour)
	handler := Middleware(ts)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rec.Code)
	}
}

func TestMiddleware_TokenFromQueryParam(t *testing.T) {
	ts := NewTokenService("test-secret-32bytes-long!!", 8*time.Hour)
	token, _ := ts.Issue(1, "alice", RoleAdmin)

	handler := Middleware(ts)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test?token="+token, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
}

func TestRequireRole_Allowed(t *testing.T) {
	ts := NewTokenService("test-secret-32bytes-long!!", 8*time.Hour)
	token, _ := ts.Issue(1, "alice", RoleAdmin)

	handler := Middleware(ts)(RequireRole(RoleAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/api/admin", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
}

func TestRequireRole_Forbidden(t *testing.T) {
	ts := NewTokenService("test-secret-32bytes-long!!", 8*time.Hour)
	token, _ := ts.Issue(1, "alice", RoleViewer)

	handler := Middleware(ts)(RequireRole(RoleAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	})))

	req := httptest.NewRequest("GET", "/api/admin", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403", rec.Code)
	}
}

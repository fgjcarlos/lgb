package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/auth"
	"github.com/fgjcarlos/lgb/internal/config"
	"github.com/fgjcarlos/lgb/internal/testutil"
)

// newAuthTestServer builds a *Server with real in-memory UserStore + TokenService
// suitable for login/refresh handler tests. Returns (srv, userStore, tokens, stop).
func newAuthTestServer(t *testing.T) (*Server, *auth.UserStore, *auth.TokenService, func()) {
	t.Helper()
	ctx := context.Background()
	store, err := auth.OpenUserStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open user store: %v", err)
	}
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"
	logger := testutil.NewLogger(t)
	srv := New(cfg, logger, nil, Opts{
		AuthTokens: tokens,
		UserStore:  store,
	})
	stop := func() {
		store.Close()
	}
	return srv, store, tokens, stop
}

// doLoginRequest posts JSON to handleLogin and returns the recorder.
func doLoginRequest(srv *Server, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	srv.handleLogin(rec, req)
	return rec
}

// doRefreshRequest posts to handleRefresh with an optional Authorization header.
func doRefreshRequest(srv *Server, bearerToken string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	srv.handleRefresh(rec, req)
	return rec
}

// ─── handleLogin tests ──────────────────────────────────────────────────────

func TestHandleLogin_ValidCredentials(t *testing.T) {
	srv, store, tokens, stop := newAuthTestServer(t)
	defer stop()

	ctx := context.Background()
	if _, err := store.Create(ctx, "alice", "secret", auth.RoleAdmin); err != nil {
		t.Fatalf("create user: %v", err)
	}

	rec := doLoginRequest(srv, `{"username":"alice","password":"secret"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Token == "" {
		t.Error("expected non-empty token")
	}
	if resp.ExpiresAt.IsZero() {
		t.Error("expected non-zero expires_at")
	}

	// Validate that the token is a real, verifiable JWT.
	claims, err := tokens.Validate(resp.Token)
	if err != nil {
		t.Fatalf("returned token is invalid: %v", err)
	}
	if claims.Username != "alice" {
		t.Errorf("expected username alice, got %s", claims.Username)
	}
}

func TestHandleLogin_WrongPassword(t *testing.T) {
	srv, store, _, stop := newAuthTestServer(t)
	defer stop()

	ctx := context.Background()
	if _, err := store.Create(ctx, "alice", "secret", auth.RoleAdmin); err != nil {
		t.Fatalf("create user: %v", err)
	}

	rec := doLoginRequest(srv, `{"username":"alice","password":"wrong"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d — body: %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unauthorized")
}

func TestHandleLogin_UnknownUser(t *testing.T) {
	srv, _, _, stop := newAuthTestServer(t)
	defer stop()

	rec := doLoginRequest(srv, `{"username":"ghost","password":"x"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d — body: %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unauthorized")
}

func TestHandleLogin_MissingBody(t *testing.T) {
	srv, _, _, stop := newAuthTestServer(t)
	defer stop()

	rec := doLoginRequest(srv, `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d — body: %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "bad_request")
}

func TestHandleLogin_InvalidJSON(t *testing.T) {
	srv, _, _, stop := newAuthTestServer(t)
	defer stop()

	rec := doLoginRequest(srv, `not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d — body: %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "bad_request")
}

// ─── handleRefresh tests ─────────────────────────────────────────────────────

func TestHandleRefresh_ValidToken(t *testing.T) {
	srv, _, tokens, stop := newAuthTestServer(t)
	defer stop()

	originalToken, err := tokens.Issue(1, "alice", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	rec := doRefreshRequest(srv, originalToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Token == "" {
		t.Error("expected non-empty token in refresh response")
	}
	if resp.ExpiresAt.IsZero() {
		t.Error("expected non-zero expires_at")
	}

	// The refreshed token must itself be valid with correct claims.
	newClaims, err := tokens.Validate(resp.Token)
	if err != nil {
		t.Fatalf("refreshed token is invalid: %v", err)
	}
	if newClaims.Username != "alice" {
		t.Errorf("expected username alice, got %s", newClaims.Username)
	}
	if newClaims.Role != auth.RoleAdmin {
		t.Errorf("expected role admin, got %s", newClaims.Role)
	}
}

func TestHandleRefresh_ExpiredToken(t *testing.T) {
	// Use a token service with a -1s TTL so any issued token is already expired.
	expiredTokens := auth.NewTokenService("test-secret-32bytes-long!!", -1*time.Second)

	cfg := testutil.MinimalConfig(t)
	cfg.Server.HTTPAddr = "127.0.0.1:0"
	logger := testutil.NewLogger(t)
	srv := New(cfg, logger, nil, Opts{AuthTokens: expiredTokens})

	expiredToken, _ := expiredTokens.Issue(1, "alice", auth.RoleAdmin)

	rec := doRefreshRequest(srv, expiredToken)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d — body: %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unauthorized")
}

func TestHandleRefresh_MissingAuthorizationHeader(t *testing.T) {
	srv, _, _, stop := newAuthTestServer(t)
	defer stop()

	rec := doRefreshRequest(srv, "") // no bearer token
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d — body: %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unauthorized")
}

// ─── Integration: routes are registered in the HTTP mux ─────────────────────

func TestAuthRoutes_LoginAndRefreshRegistered(t *testing.T) {
	ctx := context.Background()
	store, err := auth.OpenUserStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open user store: %v", err)
	}
	defer store.Close()

	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	if _, err := store.Create(ctx, "alice", "secret", auth.RoleAdmin); err != nil {
		t.Fatalf("create user: %v", err)
	}

	_, baseURL, stopSrv := startAPITestServerWithOpts(t, &snapshotPLCManager{},
		Opts{AuthTokens: tokens, UserStore: store})
	defer stopSrv()

	// POST /api/auth/login — should return 200 with a token.
	body := bytes.NewBufferString(`{"username":"alice","password":"secret"}`)
	resp, err := http.Post(baseURL+"/api/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/auth/login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", resp.StatusCode)
	}

	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if tokenResp.Token == "" {
		t.Fatal("login: expected non-empty token")
	}

	// POST /api/auth/refresh — should return 200 with a new token.
	refreshReq, _ := http.NewRequest(http.MethodPost, baseURL+"/api/auth/refresh", nil)
	refreshReq.Header.Set("Authorization", "Bearer "+tokenResp.Token)
	refreshResp, err := http.DefaultClient.Do(refreshReq)
	if err != nil {
		t.Fatalf("POST /api/auth/refresh: %v", err)
	}
	defer refreshResp.Body.Close()
	if refreshResp.StatusCode != http.StatusOK {
		t.Fatalf("refresh: expected 200, got %d", refreshResp.StatusCode)
	}
}

// POST /api/auth/refresh without a token should return 401.
func TestAuthRoutes_RefreshWithoutTokenReturns401(t *testing.T) {
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, baseURL, stopSrv := startAPITestServerWithOpts(t, &snapshotPLCManager{},
		Opts{AuthTokens: tokens})
	defer stopSrv()

	resp, err := http.Post(baseURL+"/api/auth/refresh", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/auth/refresh: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Error.Code != wantCode {
		t.Errorf("expected error code %q, got %q", wantCode, body.Error.Code)
	}
}

// Compile-time check: config.Config is used indirectly via testutil.MinimalConfig.
var _ *config.Config = (*config.Config)(nil)

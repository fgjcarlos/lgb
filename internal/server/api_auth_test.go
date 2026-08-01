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
	srv, store, tokens, stop := newAuthTestServer(t)
	defer stop()

	ctx := context.Background()
	user, err := store.Create(ctx, "alice", "correctpassword", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	originalToken, err := tokens.Issue(user.ID, user.Username, user.Role)
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

func TestHandleRefresh_DeletedUser_Returns401(t *testing.T) {
	srv, store, tokens, stop := newAuthTestServer(t)
	defer stop()

	ctx := context.Background()
	user, err := store.Create(ctx, "alice", "correctpassword", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	tok, err := tokens.Issue(user.ID, user.Username, user.Role)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	// Delete the user — the token is still valid but the account is gone.
	if err := store.Delete(ctx, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	rec := doRefreshRequest(srv, tok)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for deleted user refresh, got %d — body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleRefresh_DemotedUser_ReturnsNewRole(t *testing.T) {
	srv, store, tokens, stop := newAuthTestServer(t)
	defer stop()

	ctx := context.Background()
	// Create the user as admin, issue token, then demote to viewer.
	user, err := store.Create(ctx, "alice", "correctpassword", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	tok, err := tokens.Issue(user.ID, user.Username, auth.RoleAdmin)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	// Demote in DB — token still claims admin.
	if err := store.UpdateRole(ctx, user.ID, auth.RoleViewer); err != nil {
		t.Fatalf("update role: %v", err)
	}

	rec := doRefreshRequest(srv, tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for demoted user refresh, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	newClaims, err := tokens.Validate(resp.Token)
	if err != nil {
		t.Fatalf("new token invalid: %v", err)
	}
	if newClaims.Role != auth.RoleViewer {
		t.Errorf("expected refreshed token to carry role viewer, got %s", newClaims.Role)
	}
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

// ─── #78 session cookie tests ────────────────────────────────────────────────

// findSessionCookie returns the lgb_session cookie set by handleLogin /
// refreshed by handleRefresh. Returns nil when the response carries no
// such cookie (the assertion is the caller's responsibility).
func findSessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "lgb_session" {
			return c
		}
	}
	return nil
}

func TestHandleLogin_SetsSessionCookie(t *testing.T) {
	srv, store, _, stop := newAuthTestServer(t)
	defer stop()
	ctx := context.Background()
	if _, err := store.Create(ctx, "alice", "secret", auth.RoleAdmin); err != nil {
		t.Fatalf("create user: %v", err)
	}

	rec := doLoginRequest(srv, `{"username":"alice","password":"secret"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	cookie := findSessionCookie(rec)
	if cookie == nil {
		t.Fatal("expected lgb_session cookie to be set on login; got none")
	}
	if !cookie.HttpOnly {
		t.Error("lgb_session cookie must be HttpOnly (fix for #78 — no XSS exfiltration)")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("lgb_session SameSite = %v; want SameSite=Strict (fix for #78 — close CSRF surface)", cookie.SameSite)
	}
	if cookie.Secure {
		t.Error("lgb_session Secure must be false when TLS is disabled (test cfg)")
	}
	if cookie.Value == "" {
		t.Error("lgb_session cookie value (the JWT) must not be empty")
	}
	if cookie.Path != "/api" {
		t.Errorf("lgb_session Path = %q; want \"/api\" (scoped to API tree)", cookie.Path)
	}
}

func TestHandleRefresh_AcceptsSessionCookie(t *testing.T) {
	srv, store, _, stop := newAuthTestServer(t)
	defer stop()
	ctx := context.Background()
	if _, err := store.Create(ctx, "alice", "secret", auth.RoleAdmin); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Login first — capture the issued cookie.
	loginRec := doLoginRequest(srv, `{"username":"alice","password":"secret"}`)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", loginRec.Code)
	}
	cookie := findSessionCookie(loginRec)
	if cookie == nil {
		t.Fatal("login did not set lgb_session cookie")
	}

	// Refresh WITHOUT an Authorization header — the middleware must
	// accept the cookie transport.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "lgb_session", Value: cookie.Value})
	srv.handleRefresh(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh with cookie: expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	// A fresh cookie must be set so the browser extends its session. We
	// do not compare cookie values because consecutive TokenService.Issue
	// calls produce identical JWTs when uid/role/username/ttl do not
	// change (the issue-time is per-second and the claims hash to the
	// same signed string); what matters is that the Set-Cookie header is
	// re-emitted with a fresh Expires so the browser refreshes the cookie
	// lifetime.
	refreshed := findSessionCookie(rec)
	if refreshed == nil {
		t.Fatal("refresh did not set a fresh lgb_session cookie")
	}
	if refreshed.Expires.IsZero() {
		t.Error("refreshed cookie must carry a non-zero Expires (so the browser updates its lifetime)")
	}
}

func TestHandleLogout_ClearsSessionCookie(t *testing.T) {
	srv, store, _, stop := newAuthTestServer(t)
	defer stop()
	ctx := context.Background()
	if _, err := store.Create(ctx, "alice", "secret", auth.RoleAdmin); err != nil {
		t.Fatalf("create user: %v", err)
	}

	loginRec := doLoginRequest(srv, `{"username":"alice","password":"secret"}`)
	cookie := findSessionCookie(loginRec)
	if cookie == nil {
		t.Fatal("login did not set lgb_session cookie")
	}

	// Simulate an authenticated request that hits /api/auth/logout.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "lgb_session", Value: cookie.Value})
	// Auth middleware sets claims on the context; we shortcut it by
	// calling the handler directly (logout only reads the username
	// for audit if present, so no claims is fine).
	srv.handleLogout(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: expected 204, got %d", rec.Code)
	}

	cleared := findSessionCookie(rec)
	if cleared == nil {
		t.Fatal("logout must set a clearing Set-Cookie header")
	}
	if cleared.MaxAge >= 0 {
		t.Errorf("logout cookie MaxAge = %d; want < 0 so the browser drops it", cleared.MaxAge)
	}
	if !cleared.HttpOnly {
		t.Error("logout cookie must stay HttpOnly")
	}
}

func TestHandleMe_ReturnsClaimsFromContext(t *testing.T) {
	srv, store, tokens, stop := newAuthTestServer(t)
	defer stop()
	ctx := context.Background()
	if _, err := store.Create(ctx, "alice", "secret", auth.RoleAdmin); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, err := tokens.Issue(42, "alice", auth.RoleOperator)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	// handleMe reads claims from the request context — that is the
	// contract auth.Middleware fulfils in production. Inject the claims
	// manually here so the handler unit-test does not need the full
	// middleware chain.
	claims, err := tokens.Validate(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), claims))

	rec := httptest.NewRecorder()
	srv.handleMe(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		User struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode me response: %v", err)
	}
	if resp.User.ID != 42 || resp.User.Username != "alice" || resp.User.Role != "operator" {
		t.Errorf("me returned unexpected user: %+v", resp.User)
	}
	if resp.ExpiresAt.IsZero() {
		t.Error("me returned zero expires_at")
	}
}

func TestSessionCookie_ResolveUsesTLSToggle(t *testing.T) {
	srv, _, _, stop := newAuthTestServer(t)
	defer stop()

	// Default test config leaves TLS off → Secure must be false.
	if srv.cfg.Server.TLSEnabled {
		t.Fatal("test cfg should have TLS off")
	}
	if got := resolveSessionCookieConfig(srv); got.Secure {
		t.Error("Secure must be false when TLSEnabled=false (dev stack)")
	}
	if got := resolveSessionCookieConfig(srv); got.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite must be Strict; got %v", got.SameSite)
	}
}

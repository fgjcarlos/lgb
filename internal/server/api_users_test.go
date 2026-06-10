package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/auth"
)

// newUsersTestServerFull returns the server, store, tokens, baseURL, and stop.
func newUsersTestServerFull(t *testing.T) (*auth.UserStore, *auth.TokenService, string, func()) {
	t.Helper()
	ctx := context.Background()
	store, err := auth.OpenUserStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open user store: %v", err)
	}
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, baseURL, stopSrv := startAPITestServerWithOpts(t, &snapshotPLCManager{},
		Opts{AuthTokens: tokens, UserStore: store})
	stop := func() {
		stopSrv()
		_ = store.Close()
	}
	return store, tokens, baseURL, stop
}

func adminToken(t *testing.T, tokens *auth.TokenService, id int64, username string) string {
	t.Helper()
	tok, err := tokens.Issue(id, username, auth.RoleAdmin)
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	return tok
}

func viewerToken(t *testing.T, tokens *auth.TokenService, id int64, username string) string {
	t.Helper()
	tok, err := tokens.Issue(id, username, auth.RoleViewer)
	if err != nil {
		t.Fatalf("issue viewer token: %v", err)
	}
	return tok
}

func doRequest(t *testing.T, method, url, body, bearerToken string) *http.Response {
	t.Helper()
	var bodyReader *bytes.Buffer
	if body != "" {
		bodyReader = bytes.NewBufferString(body)
	} else {
		bodyReader = &bytes.Buffer{}
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

// ─── GET /api/users ──────────────────────────────────────────────────────────

func TestHandleListUsers_AdminGets200WithList(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, err := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	_, _ = store.Create(ctx, "viewer1", "pass", auth.RoleViewer)
	_, _ = store.Create(ctx, "viewer2", "pass", auth.RoleViewer)

	tok := adminToken(t, tokens, admin.ID, admin.Username)
	resp := doRequest(t, http.MethodGet, baseURL+"/api/users", "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Data []struct {
			ID           int64  `json:"id"`
			Username     string `json:"username"`
			Role         string `json:"role"`
			PasswordHash string `json:"password_hash"`
		} `json:"data"`
		Pagination struct {
			Count int `json:"count"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 3 {
		t.Errorf("expected 3 users, got %d", len(body.Data))
	}
	// Verify no password hash is exposed.
	for _, u := range body.Data {
		if u.PasswordHash != "" {
			t.Errorf("password_hash must not be exposed, got non-empty for user %q", u.Username)
		}
	}
}

func TestHandleListUsers_ViewerGets403(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	viewer, err := store.Create(ctx, "viewer1", "pass", auth.RoleViewer)
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}

	tok := viewerToken(t, tokens, viewer.ID, viewer.Username)
	resp := doRequest(t, http.MethodGet, baseURL+"/api/users", "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// ─── POST /api/users ─────────────────────────────────────────────────────────

func TestHandleCreateUser_AdminCreatesViewer201(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	tok := adminToken(t, tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodPost, baseURL+"/api/users",
		`{"username":"bob","password":"correctpassword","role":"viewer"}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var body struct {
		Data struct {
			Username     string `json:"username"`
			Role         string `json:"role"`
			PasswordHash string `json:"password_hash"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Username != "bob" {
		t.Errorf("expected username bob, got %q", body.Data.Username)
	}
	if body.Data.Role != "viewer" {
		t.Errorf("expected role viewer, got %q", body.Data.Role)
	}
	if body.Data.PasswordHash != "" {
		t.Error("password_hash must not be exposed in create response")
	}
}

func TestHandleCreateUser_DuplicateUsername409(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	_, _ = store.Create(ctx, "alice", "pass", auth.RoleViewer)
	tok := adminToken(t, tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodPost, baseURL+"/api/users",
		`{"username":"alice","password":"correctpassword","role":"viewer"}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "duplicate_username")
}

func TestHandleCreateUser_InvalidRole400(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	tok := adminToken(t, tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodPost, baseURL+"/api/users",
		`{"username":"bob","password":"pw","role":"superuser"}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "invalid_role")
}

func TestHandleCreateUser_PasswordTooShort400(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	tok := adminToken(t, tokens, admin.ID, admin.Username)

	// 11-char password — one short of the 12-char minimum.
	resp := doRequest(t, http.MethodPost, baseURL+"/api/users",
		`{"username":"bob","password":"tooshortpwd","role":"viewer"}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for short password, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "weak_password")
}

func TestHandleCreateUser_PasswordTooLong400(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	tok := adminToken(t, tokens, admin.ID, admin.Username)

	// 73-byte password — one byte over the 72-byte bcrypt limit.
	longPwd := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	body := fmt.Sprintf(`{"username":"bob","password":%q,"role":"viewer"}`, longPwd)
	resp := doRequest(t, http.MethodPost, baseURL+"/api/users", body, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for long password, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "weak_password")
}

func TestHandleUpdateUserPassword_PasswordTooShort400(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	target, _ := store.Create(ctx, "viewer1", "pass", auth.RoleViewer)
	tok := adminToken(t, tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodPut,
		fmt.Sprintf("%s/api/users/%d/password", baseURL, target.ID),
		`{"password":"tooshortpwd"}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for short password update, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "weak_password")
}

func TestHandleUpdateUserPassword_PasswordTooLong400(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	target, _ := store.Create(ctx, "viewer1", "pass", auth.RoleViewer)
	tok := adminToken(t, tokens, admin.ID, admin.Username)

	longPwd := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	body := fmt.Sprintf(`{"password":%q}`, longPwd)
	resp := doRequest(t, http.MethodPut,
		fmt.Sprintf("%s/api/users/%d/password", baseURL, target.ID),
		body, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for long password update, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "weak_password")
}

// ─── GET /api/users/{id} ─────────────────────────────────────────────────────

func TestHandleGetUser_Admin200(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	target, _ := store.Create(ctx, "viewer1", "pass", auth.RoleViewer)
	tok := adminToken(t, tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodGet, fmt.Sprintf("%s/api/users/%d", baseURL, target.ID), "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.ID != target.ID {
		t.Errorf("expected id %d, got %d", target.ID, body.Data.ID)
	}
}

func TestHandleGetUser_NotFound404(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	tok := adminToken(t, tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodGet, baseURL+"/api/users/999", "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "user_not_found")
}

// ─── PUT /api/users/{id}/role ────────────────────────────────────────────────

func TestHandleUpdateUserRole_Admin200(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	target, _ := store.Create(ctx, "viewer1", "pass", auth.RoleViewer)
	tok := adminToken(t, tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodPut,
		fmt.Sprintf("%s/api/users/%d/role", baseURL, target.ID),
		`{"role":"operator"}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Data struct {
			Role string `json:"role"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Role != "operator" {
		t.Errorf("expected role operator, got %q", body.Data.Role)
	}
}

func TestHandleUpdateUserRole_InvalidRole400(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	target, _ := store.Create(ctx, "viewer1", "pass", auth.RoleViewer)
	tok := adminToken(t, tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodPut,
		fmt.Sprintf("%s/api/users/%d/role", baseURL, target.ID),
		`{"role":"superuser"}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "invalid_role")
}

func TestHandleUpdateUserRole_NotFound404(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	tok := adminToken(t, tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodPut, baseURL+"/api/users/999/role",
		`{"role":"viewer"}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "user_not_found")
}

// ─── PUT /api/users/{id}/password ────────────────────────────────────────────

func TestHandleUpdateUserPassword_Admin204(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	target, _ := store.Create(ctx, "viewer1", "oldpass", auth.RoleViewer)
	tok := adminToken(t, tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodPut,
		fmt.Sprintf("%s/api/users/%d/password", baseURL, target.ID),
		`{"password":"newpassword123"}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestHandleUpdateUserPassword_NotFound404(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	tok := adminToken(t, tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodPut, baseURL+"/api/users/999/password",
		`{"password":"newpassword123"}`, tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "user_not_found")
}

// ─── DELETE /api/users/{id} ──────────────────────────────────────────────────

func TestHandleDeleteUser_Admin204(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	viewer, _ := store.Create(ctx, "viewer1", "pass", auth.RoleViewer)
	tok := adminToken(t, tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodDelete,
		fmt.Sprintf("%s/api/users/%d", baseURL, viewer.ID), "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestHandleDeleteUser_SelfDelete409(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	// Create a second admin so the last-admin guard doesn't trigger.
	admin1, _ := store.Create(ctx, "admin1", "password123456", auth.RoleAdmin)
	_, _ = store.Create(ctx, "admin2", "password123456", auth.RoleAdmin)

	// Authenticate as admin1 and try to delete their own account.
	tok := adminToken(t, tokens, admin1.ID, admin1.Username)
	resp := doRequest(t, http.MethodDelete,
		fmt.Sprintf("%s/api/users/%d", baseURL, admin1.ID), "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for self-delete, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "self_delete")

	// Verify the user still exists.
	if _, err := store.GetByID(ctx, admin1.ID); err != nil {
		t.Errorf("user should still exist after rejected self-delete, got: %v", err)
	}
}

func TestHandleDeleteUser_LastAdmin409(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	// Create two admins so we can obtain a valid admin token for the caller
	// without triggering the self-delete guard.
	admin1, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	admin2, _ := store.Create(ctx, "admin2", "pass", auth.RoleAdmin)

	// Remove admin1 from the DB directly so admin2 is now the last admin.
	// admin1's token is still valid (JWT not expired), so the middleware lets
	// the request through.
	if err := store.Delete(ctx, admin1.ID); err != nil {
		t.Fatalf("delete admin1 from store: %v", err)
	}

	tok := adminToken(t, tokens, admin1.ID, admin1.Username)
	resp := doRequest(t, http.MethodDelete,
		fmt.Sprintf("%s/api/users/%d", baseURL, admin2.ID), "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	assertHTTPErrorCode(t, resp, "last_admin")
}

func TestHandleDeleteUser_TwoAdmins204(t *testing.T) {
	store, tokens, baseURL, stop := newUsersTestServerFull(t)
	defer stop()
	ctx := context.Background()

	admin1, _ := store.Create(ctx, "admin1", "pass", auth.RoleAdmin)
	admin2, _ := store.Create(ctx, "admin2", "pass", auth.RoleAdmin)
	tok := adminToken(t, tokens, admin1.ID, admin1.Username)

	resp := doRequest(t, http.MethodDelete,
		fmt.Sprintf("%s/api/users/%d", baseURL, admin2.ID), "", tok)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func assertHTTPErrorCode(t *testing.T, resp *http.Response, wantCode string) {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Error.Code != wantCode {
		t.Errorf("expected error code %q, got %q", wantCode, body.Error.Code)
	}
}

// compile-time guard
var _ *httptest.ResponseRecorder = (*httptest.ResponseRecorder)(nil)

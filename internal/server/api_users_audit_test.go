package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/auth"
)

// usersAuditEnv is a user-management API server wired with a real audit log.
type usersAuditEnv struct {
	store    *auth.UserStore
	tokens   *auth.TokenService
	baseURL  string
	auditDir string
}

func newUsersAuditEnv(t *testing.T) *usersAuditEnv {
	t.Helper()
	ctx := context.Background()
	store, err := auth.OpenUserStore(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open user store: %v", err)
	}
	auditDir := t.TempDir()
	auditLog, err := auth.OpenAuditLogger(auditDir)
	if err != nil {
		t.Fatalf("open audit logger: %v", err)
	}
	tokens := auth.NewTokenService("test-secret-32bytes-long!!", time.Hour)
	_, baseURL, stopSrv := startAPITestServerWithOpts(t, &snapshotPLCManager{},
		Opts{AuthTokens: tokens, UserStore: store, AuditLog: auditLog})
	t.Cleanup(func() {
		stopSrv()
		_ = auditLog.Close()
		_ = store.Close()
	})
	return &usersAuditEnv{store: store, tokens: tokens, baseURL: baseURL, auditDir: auditDir}
}

// seedUser creates a user directly in the store (no audit) and returns it.
func (e *usersAuditEnv) seedUser(t *testing.T, name string, role auth.Role) *auth.User {
	t.Helper()
	u, err := e.store.Create(context.Background(), name, "password1234", role)
	if err != nil {
		t.Fatalf("seed user %s: %v", name, err)
	}
	return u
}

// assertSingleUserAudit asserts exactly one audit event with the given action,
// actor, target, a client IP, and that the detail never carries a password.
func assertSingleUserAudit(t *testing.T, events []auth.AuditEvent, action, actor string, target int64, wantDetail string) {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event, got %d: %+v", len(events), events)
	}
	ev := events[0]
	if ev.Action != action {
		t.Errorf("Action = %q; want %q", ev.Action, action)
	}
	if ev.Username != actor {
		t.Errorf("Username = %q; want %q", ev.Username, actor)
	}
	if ev.TargetID != target {
		t.Errorf("TargetID = %d; want %d", ev.TargetID, target)
	}
	if !strings.HasPrefix(ev.IP, "127.0.0.1:") {
		t.Errorf("IP = %q; want client RemoteAddr 127.0.0.1:<port>", ev.IP)
	}
	if ev.Detail != wantDetail {
		t.Errorf("Detail = %q; want %q", ev.Detail, wantDetail)
	}
	if strings.Contains(ev.Detail, "password1234") || strings.Contains(ev.Detail, "newpassword99") {
		t.Errorf("Detail leaks a password: %q", ev.Detail)
	}
}

func TestUserAudit_Create_EmitsUserCreate(t *testing.T) {
	env := newUsersAuditEnv(t)
	admin := env.seedUser(t, "root", auth.RoleAdmin)
	tok := adminToken(t, env.tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodPost, env.baseURL+"/api/users",
		`{"username":"bob","password":"password1234","role":"viewer"}`, tok)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	bob, err := env.store.GetByUsername(context.Background(), "bob")
	if err != nil {
		t.Fatalf("lookup bob: %v", err)
	}
	assertSingleUserAudit(t, readAuditEventsDCMD(t, env.auditDir),
		"user.create", "root", bob.ID, `username="bob" role=viewer`)
}

// TestUserAudit_Create_QuotesUsername pins that a crafted username cannot
// forge extra key=value pairs in the audit detail.
func TestUserAudit_Create_QuotesUsername(t *testing.T) {
	env := newUsersAuditEnv(t)
	admin := env.seedUser(t, "root", auth.RoleAdmin)
	tok := adminToken(t, env.tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodPost, env.baseURL+"/api/users",
		`{"username":"x role=admin","password":"password1234","role":"viewer"}`, tok)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Skipf("username with spaces rejected (%d): nothing to forge", resp.StatusCode)
	}
	events := readAuditEventsDCMD(t, env.auditDir)
	if len(events) != 1 || events[0].Detail != `username="x role=admin" role=viewer` {
		t.Fatalf("expected quoted username in detail, got %+v", events)
	}
}

func TestUserAudit_RoleChange_EmitsUserRoleChange(t *testing.T) {
	env := newUsersAuditEnv(t)
	admin := env.seedUser(t, "root", auth.RoleAdmin)
	bob := env.seedUser(t, "bob", auth.RoleViewer)
	tok := adminToken(t, env.tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodPut, fmt.Sprintf("%s/api/users/%d/role", env.baseURL, bob.ID),
		`{"role":"operator"}`, tok)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	assertSingleUserAudit(t, readAuditEventsDCMD(t, env.auditDir),
		"user.role_change", "root", bob.ID, `username="bob" role=operator`)
}

func TestUserAudit_PasswordReset_EmitsUserPasswordReset(t *testing.T) {
	env := newUsersAuditEnv(t)
	admin := env.seedUser(t, "root", auth.RoleAdmin)
	bob := env.seedUser(t, "bob", auth.RoleViewer)
	tok := adminToken(t, env.tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodPut, fmt.Sprintf("%s/api/users/%d/password", env.baseURL, bob.ID),
		`{"password":"newpassword99"}`, tok)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	assertSingleUserAudit(t, readAuditEventsDCMD(t, env.auditDir),
		"user.password_reset", "root", bob.ID, "")
}

func TestUserAudit_Delete_EmitsUserDelete(t *testing.T) {
	env := newUsersAuditEnv(t)
	admin := env.seedUser(t, "root", auth.RoleAdmin)
	bob := env.seedUser(t, "bob", auth.RoleOperator)
	tok := adminToken(t, env.tokens, admin.ID, admin.Username)

	resp := doRequest(t, http.MethodDelete, fmt.Sprintf("%s/api/users/%d", env.baseURL, bob.ID), "", tok)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	assertSingleUserAudit(t, readAuditEventsDCMD(t, env.auditDir),
		"user.delete", "root", bob.ID, `username="bob" role=operator`)
}

// TestUserAudit_RejectedMutations_EmitNothing pins that only successful
// mutations are audited: authorization denials, validation errors, missing
// targets and guard refusals leave the audit trail untouched.
func TestUserAudit_RejectedMutations_EmitNothing(t *testing.T) {
	env := newUsersAuditEnv(t)
	admin := env.seedUser(t, "root", auth.RoleAdmin)
	bob := env.seedUser(t, "bob", auth.RoleViewer)
	adminTok := adminToken(t, env.tokens, admin.ID, admin.Username)
	viewerTok := viewerToken(t, env.tokens, bob.ID, bob.Username)

	cases := []struct {
		name, method, path, body, tok string
		want                          int
	}{
		{"viewer create denied", http.MethodPost, "/api/users", `{"username":"eve","password":"password1234","role":"admin"}`, viewerTok, http.StatusForbidden},
		{"viewer role change denied", http.MethodPut, fmt.Sprintf("/api/users/%d/role", bob.ID), `{"role":"admin"}`, viewerTok, http.StatusForbidden},
		{"viewer password reset denied", http.MethodPut, fmt.Sprintf("/api/users/%d/password", admin.ID), `{"password":"newpassword99"}`, viewerTok, http.StatusForbidden},
		{"viewer delete denied", http.MethodDelete, fmt.Sprintf("/api/users/%d", admin.ID), "", viewerTok, http.StatusForbidden},
		{"duplicate create", http.MethodPost, "/api/users", `{"username":"bob","password":"password1234","role":"viewer"}`, adminTok, http.StatusConflict},
		{"invalid role", http.MethodPut, fmt.Sprintf("/api/users/%d/role", bob.ID), `{"role":"root"}`, adminTok, http.StatusBadRequest},
		{"role change missing user", http.MethodPut, "/api/users/9999/role", `{"role":"admin"}`, adminTok, http.StatusNotFound},
		{"password reset missing user", http.MethodPut, "/api/users/9999/password", `{"password":"newpassword99"}`, adminTok, http.StatusNotFound},
		{"self delete", http.MethodDelete, fmt.Sprintf("/api/users/%d", admin.ID), "", adminTok, http.StatusConflict},
	}
	for _, tc := range cases {
		resp := doRequest(t, tc.method, env.baseURL+tc.path, tc.body, tc.tok)
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("%s: expected %d, got %d", tc.name, tc.want, resp.StatusCode)
		}
	}
	if events := readAuditEventsDCMD(t, env.auditDir); len(events) != 0 {
		t.Fatalf("expected no audit events for rejected mutations, got %+v", events)
	}
}

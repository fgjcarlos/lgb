package server

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/fgjcarlos/lgb/internal/auth"
)

// Admin unlock of a username's login lockout (#138).

func postLogin(t *testing.T, baseURL, username, password string) int {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	resp, err := http.Post(baseURL+"/api/auth/login", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// lockOut fails the login of username until it is locked.
func lockOut(t *testing.T, baseURL, username string) {
	t.Helper()
	for i := 0; i < loginMaxFailures; i++ {
		postLogin(t, baseURL, username, "wrong-password")
	}
	if code := postLogin(t, baseURL, username, "password1234"); code != http.StatusTooManyRequests {
		t.Fatalf("setup: expected %q locked out (429), got %d", username, code)
	}
}

func unlockEvents(t *testing.T, auditDir string) []auth.AuditEvent {
	t.Helper()
	var out []auth.AuditEvent
	for _, ev := range readAuditEventsDCMD(t, auditDir) {
		if ev.Action == "login.unlock" {
			out = append(out, ev)
		}
	}
	return out
}

func unlockURL(baseURL, username string) string {
	return baseURL + "/api/auth/lockouts/" + url.PathEscape(username)
}

func TestLoginUnlock_AdminClearsLockoutAndAudits(t *testing.T) {
	env := newUsersAuditEnv(t)
	env.seedUser(t, "alice", auth.RoleAdmin)
	admin := env.seedUser(t, "root", auth.RoleAdmin)
	lockOut(t, env.baseURL, "alice")

	resp := doRequest(t, http.MethodDelete, unlockURL(env.baseURL, "alice"), "", adminToken(t, env.tokens, admin.ID, "root"))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unlock: expected 204, got %d", resp.StatusCode)
	}
	if code := postLogin(t, env.baseURL, "alice", "password1234"); code != http.StatusOK {
		t.Fatalf("login after unlock: expected 200, got %d", code)
	}
	events := unlockEvents(t, env.auditDir)
	if len(events) != 1 {
		t.Fatalf("expected 1 login.unlock event, got %d: %+v", len(events), events)
	}
	ev := events[0]
	if ev.Username != "root" || ev.Detail != `username="alice"` || ev.IP == "" {
		t.Fatalf("unexpected audit event: %+v", ev)
	}
}

// The response does not depend on whether the username exists or is locked.
func TestLoginUnlock_UniformForUnknownOrUnlockedUser(t *testing.T) {
	env := newUsersAuditEnv(t)
	admin := env.seedUser(t, "root", auth.RoleAdmin)
	tok := adminToken(t, env.tokens, admin.ID, "root")
	for _, name := range []string{"ghost", "root", "we/ird name"} {
		resp := doRequest(t, http.MethodDelete, unlockURL(env.baseURL, name), "", tok)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("unlock %q: expected 204, got %d", name, resp.StatusCode)
		}
	}
	if n := len(unlockEvents(t, env.auditDir)); n != 3 {
		t.Fatalf("expected 3 login.unlock events, got %d", n)
	}
}

func TestLoginUnlock_NonAdminRejectedLockoutKept(t *testing.T) {
	env := newUsersAuditEnv(t)
	env.seedUser(t, "alice", auth.RoleAdmin)
	viewer := env.seedUser(t, "vera", auth.RoleViewer)
	lockOut(t, env.baseURL, "alice")

	cases := []struct {
		token string
		want  int
	}{
		{viewerToken(t, env.tokens, viewer.ID, "vera"), http.StatusForbidden},
		{"", http.StatusUnauthorized},
	}
	for _, c := range cases {
		resp := doRequest(t, http.MethodDelete, unlockURL(env.baseURL, "alice"), "", c.token)
		_ = resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Fatalf("expected %d, got %d", c.want, resp.StatusCode)
		}
	}
	if code := postLogin(t, env.baseURL, "alice", "password1234"); code != http.StatusTooManyRequests {
		t.Fatalf("lockout must survive rejected unlocks: expected 429, got %d", code)
	}
	if n := len(unlockEvents(t, env.auditDir)); n != 0 {
		t.Fatalf("rejected unlocks must not be audited, got %d events", n)
	}
}

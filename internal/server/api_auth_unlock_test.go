package server

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
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
		t.Fatalf("setup: expected %.20q locked out (429), got %d", username, code)
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

// unlock posts a raw JSON body to the unlock endpoint and returns the status.
func unlock(t *testing.T, baseURL, body, token string) int {
	t.Helper()
	resp := doRequest(t, http.MethodPost, baseURL+"/api/auth/unlock", body, token)
	_ = resp.Body.Close()
	return resp.StatusCode
}

func unlockBody(username string) string { return fmt.Sprintf(`{"username":%q}`, username) }

func TestLoginUnlock_AdminClearsLockoutAndAudits(t *testing.T) {
	env := newUsersAuditEnv(t)
	env.seedUser(t, "alice", auth.RoleAdmin)
	admin := env.seedUser(t, "root", auth.RoleAdmin)
	lockOut(t, env.baseURL, "alice")

	if code := unlock(t, env.baseURL, unlockBody("alice"), adminToken(t, env.tokens, admin.ID, "root")); code != http.StatusNoContent {
		t.Fatalf("unlock: expected 204, got %d", code)
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

// Names a URL path cannot carry intact (dot segments, slashes) and names
// longer than the limiter key still unlock (#138).
func TestLoginUnlock_AnyUsernameShape(t *testing.T) {
	for _, name := range []string{".", "..", "a/b", "x y é", strings.Repeat("L", loginMaxKeyLen+44)} {
		env := newUsersAuditEnv(t) // fresh env: lockOut would otherwise hit the per-IP limit
		admin := env.seedUser(t, "root", auth.RoleAdmin)
		lockOut(t, env.baseURL, name)
		if code := unlock(t, env.baseURL, unlockBody(name), adminToken(t, env.tokens, admin.ID, "root")); code != http.StatusNoContent {
			t.Fatalf("unlock %.20q: expected 204, got %d", name, code)
		}
		if code := postLogin(t, env.baseURL, name, "wrong-password"); code != http.StatusUnauthorized {
			t.Fatalf("%.20q still locked after unlock: got %d", name, code)
		}
	}
}

// The response does not depend on whether the username exists or is locked.
func TestLoginUnlock_UniformForUnknownOrUnlockedUser(t *testing.T) {
	env := newUsersAuditEnv(t)
	admin := env.seedUser(t, "root", auth.RoleAdmin)
	tok := adminToken(t, env.tokens, admin.ID, "root")
	for _, name := range []string{"ghost", "root"} {
		if code := unlock(t, env.baseURL, unlockBody(name), tok); code != http.StatusNoContent {
			t.Fatalf("unlock %q: expected 204, got %d", name, code)
		}
	}
	if n := len(unlockEvents(t, env.auditDir)); n != 2 {
		t.Fatalf("expected 2 login.unlock events, got %d", n)
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
		if code := unlock(t, env.baseURL, unlockBody("alice"), c.token); code != c.want {
			t.Fatalf("expected %d, got %d", c.want, code)
		}
	}
	if code := postLogin(t, env.baseURL, "alice", "password1234"); code != http.StatusTooManyRequests {
		t.Fatalf("lockout must survive rejected unlocks: expected 429, got %d", code)
	}
	if n := len(unlockEvents(t, env.auditDir)); n != 0 {
		t.Fatalf("rejected unlocks must not be audited, got %d events", n)
	}
}

func TestLoginUnlock_BadBodyRejected(t *testing.T) {
	env := newUsersAuditEnv(t)
	admin := env.seedUser(t, "root", auth.RoleAdmin)
	tok := adminToken(t, env.tokens, admin.ID, "root")
	for _, body := range []string{"", "{", `{"username":""}`, `{"username":"` + strings.Repeat("a", 5<<10) + `"}`} {
		if code := unlock(t, env.baseURL, body, tok); code != http.StatusBadRequest {
			t.Fatalf("body %.20q: expected 400, got %d", body, code)
		}
	}
	if n := len(unlockEvents(t, env.auditDir)); n != 0 {
		t.Fatalf("rejected unlocks must not be audited, got %d events", n)
	}
}

// An unlock that cannot be audited does not happen (fail closed).
func TestLoginUnlock_AuditFailureKeepsLockout(t *testing.T) {
	env := newUsersAuditEnv(t)
	env.seedUser(t, "alice", auth.RoleAdmin)
	admin := env.seedUser(t, "root", auth.RoleAdmin)
	lockOut(t, env.baseURL, "alice")
	_ = env.auditLog.Close() // every later audit write fails

	if code := unlock(t, env.baseURL, unlockBody("alice"), adminToken(t, env.tokens, admin.ID, "root")); code != http.StatusInternalServerError {
		t.Fatalf("unlock with failing audit: expected 500, got %d", code)
	}
	if code := postLogin(t, env.baseURL, "alice", "password1234"); code != http.StatusTooManyRequests {
		t.Fatalf("lockout must survive an unaudited unlock: expected 429, got %d", code)
	}
}

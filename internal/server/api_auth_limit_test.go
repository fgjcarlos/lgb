package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fgjcarlos/lgb/internal/auth"
)

// Login brute-force protection tests (#74).

// fakeClock lets tests move the login limiter's notion of "now".
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newLimitTestServer(t *testing.T) (*Server, *fakeClock) {
	t.Helper()
	srv, store, _, stop := newAuthTestServer(t)
	t.Cleanup(stop)
	if _, err := store.Create(context.Background(), "alice", "secret", auth.RoleAdmin); err != nil {
		t.Fatalf("create user: %v", err)
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	srv.loginLimiter.now = clock.now
	return srv, clock
}

func loginFrom(srv *Server, remoteAddr, username, password string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	srv.handleLogin(rec, req)
	return rec
}

func expectStatus(t *testing.T, rec *httptest.ResponseRecorder, want int, step string) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s: expected %d, got %d — body: %s", step, want, rec.Code, rec.Body.String())
	}
}

func TestLoginLimit_LockoutAfterConsecutiveFailures(t *testing.T) {
	srv, _ := newLimitTestServer(t)

	for i := 1; i <= loginMaxFailures; i++ {
		expectStatus(t, loginFrom(srv, "192.0.2.1:1000", "alice", "wrong"), http.StatusUnauthorized, fmt.Sprintf("failure %d", i))
	}

	// Locked: even the correct password is refused with 429 + Retry-After.
	rec := loginFrom(srv, "192.0.2.1:1000", "alice", "secret")
	expectStatus(t, rec, http.StatusTooManyRequests, "locked with correct password")
	if got := rec.Header().Get("Retry-After"); got != "900" {
		t.Errorf("Retry-After = %q, want %q", got, "900")
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Error("locked login must not set a session cookie")
	}
	lockedBody := rec.Body.String()
	assertErrorCode(t, rec, "too_many_requests")

	// Unknown usernames lock the same way with an identical response (no enumeration).
	for i := 1; i <= loginMaxFailures; i++ {
		expectStatus(t, loginFrom(srv, "192.0.2.2:1000", "ghost", "wrong"), http.StatusUnauthorized, fmt.Sprintf("ghost failure %d", i))
	}
	rec = loginFrom(srv, "192.0.2.2:1000", "ghost", "wrong")
	expectStatus(t, rec, http.StatusTooManyRequests, "ghost locked")
	if rec.Body.String() != lockedBody {
		t.Errorf("locked responses differ:\n known:   %s\n unknown: %s", lockedBody, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "900" {
		t.Errorf("ghost Retry-After = %q, want %q", got, "900")
	}
}

func TestLoginLimit_LockoutExpires(t *testing.T) {
	srv, clock := newLimitTestServer(t)

	for i := 1; i <= loginMaxFailures; i++ {
		expectStatus(t, loginFrom(srv, "192.0.2.1:1000", "alice", "wrong"), http.StatusUnauthorized, fmt.Sprintf("failure %d", i))
	}
	clock.advance(loginLockout - time.Second)
	expectStatus(t, loginFrom(srv, "192.0.2.1:1000", "alice", "secret"), http.StatusTooManyRequests, "1s before expiry")

	clock.advance(time.Second)
	expectStatus(t, loginFrom(srv, "192.0.2.1:1000", "alice", "secret"), http.StatusOK, "after expiry")
}

func TestLoginLimit_SuccessResetsCounter(t *testing.T) {
	srv, _ := newLimitTestServer(t)

	for i := 1; i < loginMaxFailures; i++ {
		expectStatus(t, loginFrom(srv, "192.0.2.1:1000", "alice", "wrong"), http.StatusUnauthorized, fmt.Sprintf("first run failure %d", i))
	}
	expectStatus(t, loginFrom(srv, "192.0.2.1:1000", "alice", "secret"), http.StatusOK, "success resets")

	for i := 1; i < loginMaxFailures; i++ {
		expectStatus(t, loginFrom(srv, "192.0.2.1:1000", "alice", "wrong"), http.StatusUnauthorized, fmt.Sprintf("second run failure %d", i))
	}
	expectStatus(t, loginFrom(srv, "192.0.2.1:1000", "alice", "secret"), http.StatusOK, "not locked after reset")
}

func TestLoginLimit_PerIPCeiling(t *testing.T) {
	srv, clock := newLimitTestServer(t)

	// Distinct usernames so the per-username lockout never kicks in.
	for i := 1; i <= loginIPLimit; i++ {
		expectStatus(t, loginFrom(srv, "198.51.100.7:4000", fmt.Sprintf("user%d", i), "x"), http.StatusUnauthorized, fmt.Sprintf("attempt %d", i))
	}
	rec := loginFrom(srv, "198.51.100.7:4001", "alice", "secret") // new port, same IP
	expectStatus(t, rec, http.StatusTooManyRequests, "over the IP ceiling")
	assertErrorCode(t, rec, "too_many_requests")
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want %q", got, "60")
	}

	// Another IP is unaffected.
	expectStatus(t, loginFrom(srv, "198.51.100.8:4000", "alice", "secret"), http.StatusOK, "other IP")

	// The window rolls over.
	clock.advance(loginIPWindow)
	expectStatus(t, loginFrom(srv, "198.51.100.7:4000", "alice", "secret"), http.StatusOK, "after window")
}

func TestLoginLimit_FailedAttemptsAudited(t *testing.T) {
	srv, _ := newLimitTestServer(t)
	dir := t.TempDir()
	al, err := auth.OpenAuditLogger(dir)
	if err != nil {
		t.Fatalf("open audit logger: %v", err)
	}
	t.Cleanup(func() { al.Close() })
	srv.auditLog = al

	for i := 1; i <= loginMaxFailures+1; i++ {
		loginFrom(srv, "203.0.113.9:5555", "alice", "wrong")
	}
	loginFrom(srv, "203.0.113.9:5555", "ghost", "wrong")

	events := readAuditEvents(t, filepath.Join(dir, "events.jsonl"))
	if len(events) != loginMaxFailures+2 {
		t.Fatalf("expected %d audit events, got %d: %+v", loginMaxFailures+2, len(events), events)
	}
	for i, ev := range events {
		wantUser, wantDetail := "alice", "invalid credentials"
		if i == loginMaxFailures {
			wantDetail = "locked out"
		}
		if i == loginMaxFailures+1 {
			wantUser = "ghost"
		}
		if ev.Action != "login_failed" || ev.Username != wantUser || ev.IP != "203.0.113.9:5555" || ev.Detail != wantDetail {
			t.Errorf("event %d = %+v, want action login_failed user %s ip 203.0.113.9:5555 detail %q", i, ev, wantUser, wantDetail)
		}
	}
}

func readAuditEvents(t *testing.T, path string) []auth.AuditEvent {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open audit file: %v", err)
	}
	defer f.Close()
	var out []auth.AuditEvent
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev auth.AuditEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("decode audit line %q: %v", sc.Text(), err)
		}
		out = append(out, ev)
	}
	return out
}

func TestLoginLimit_ConcurrentAttemptsCapped(t *testing.T) {
	srv, _ := newLimitTestServer(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	counts := map[int]int{}
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code := loginFrom(srv, fmt.Sprintf("10.0.%d.1:1", i), "alice", "wrong").Code
			mu.Lock()
			counts[code]++
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if counts[http.StatusUnauthorized] != loginMaxFailures || counts[http.StatusTooManyRequests] != 40-loginMaxFailures {
		t.Fatalf("status counts = %v, want %d x 401 and %d x 429", counts, loginMaxFailures, 40-loginMaxFailures)
	}
}

func TestLoginLimit_IPv6GroupedBy64(t *testing.T) {
	srv, _ := newLimitTestServer(t)
	for i := 1; i <= loginIPLimit; i++ {
		expectStatus(t, loginFrom(srv, fmt.Sprintf("[2001:db8::%x]:1", i), fmt.Sprintf("user%d", i), "x"), http.StatusUnauthorized, fmt.Sprintf("attempt %d", i))
	}
	expectStatus(t, loginFrom(srv, "[2001:db8::ffff]:1", "alice", "secret"), http.StatusTooManyRequests, "same /64")
	expectStatus(t, loginFrom(srv, "[2001:db8:0:1::1]:1", "alice", "secret"), http.StatusOK, "other /64")
}

// A flood of distinct usernames must never lift an existing lockout; while the
// table is full of live entries, untracked usernames fail closed.
func TestLoginLimit_FullTableFailsClosed(t *testing.T) {
	l := newLoginLimiter()
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	l.now = clock.now

	for i := 0; i < loginMaxFailures; i++ {
		if wait := l.beginUser("alice"); wait != 0 {
			t.Fatalf("attempt %d refused early: %v", i+1, wait)
		}
	}
	for i := 0; i < loginMaxKeys*2; i++ {
		l.beginUser(fmt.Sprintf("u%d", i))
	}
	if wait := l.beginUser("alice"); wait <= 0 {
		t.Fatal("flood lifted alice's lockout")
	}
	if wait := l.beginUser("newcomer"); wait <= 0 {
		t.Fatal("untracked username allowed while the table is full")
	}
	if n := len(l.users.m); n > loginMaxKeys {
		t.Fatalf("tracked %d usernames, cap is %d", n, loginMaxKeys)
	}

	clock.advance(loginLockout)
	if wait := l.beginUser("newcomer"); wait != 0 {
		t.Fatalf("stale entries not pruned after the lockout window: %v", wait)
	}
}

func TestLoginLimit_UsernameKeyBounded(t *testing.T) {
	l := newLoginLimiter()
	l.beginUser(strings.Repeat("a", 4000))
	for k := range l.users.m {
		if len(k) > loginMaxKeyLen {
			t.Fatalf("tracked key length %d, cap is %d", len(k), loginMaxKeyLen)
		}
	}
}

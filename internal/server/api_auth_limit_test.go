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
	"github.com/fgjcarlos/lgb/internal/config"
)

// Login brute-force protection tests (#74).

// fakeClock lets tests move the login limiter's notion of "now".
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newLimitTestServer(t *testing.T, cfgFns ...func(*config.Config)) (*Server, *fakeClock) {
	t.Helper()
	srv, store, _, stop := newAuthTestServer(t, cfgFns...)
	t.Cleanup(stop)
	if _, err := store.Create(context.Background(), "alice", "secret", auth.RoleAdmin); err != nil {
		t.Fatalf("create user: %v", err)
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	srv.loginLimiter.now = clock.now
	return srv, clock
}

func loginFrom(srv *Server, remoteAddr, username, password string) *httptest.ResponseRecorder {
	return loginVia(srv, remoteAddr, "", username, password)
}

// loginVia is loginFrom with an X-Forwarded-For header (omitted when empty).
func loginVia(srv *Server, remoteAddr, xff, username, password string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
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

func TestLoginIPKey(t *testing.T) {
	cases := map[string]string{
		"192.0.2.1:1000":    "192.0.2.1",
		"192.0.2.1":         "192.0.2.1",
		"[2001:db8::1]:443": "2001:db8::/64",
		"2001:db8::ffff:1":  "2001:db8::/64",
		"2001:db8:0:1::1":   "2001:db8:0:1::/64",
	}
	for in, want := range cases {
		if got := loginIPKey(in); got != want {
			t.Errorf("loginIPKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// #137: with no trusted proxies, a spoofed X-Forwarded-For does not escape the
// peer's bucket; behind a trusted proxy each forwarded client gets its own.
func TestLoginLimit_TrustedProxy(t *testing.T) {
	t.Run("untrusted peer spoofing XFF shares its bucket", func(t *testing.T) {
		srv, _ := newLimitTestServer(t)
		for i := 1; i <= loginIPLimit; i++ {
			expectStatus(t, loginVia(srv, "10.0.0.1:1", fmt.Sprintf("203.0.113.%d", i), fmt.Sprintf("user%d", i), "x"), http.StatusUnauthorized, fmt.Sprintf("attempt %d", i))
		}
		expectStatus(t, loginVia(srv, "10.0.0.1:1", "198.51.100.1", "alice", "secret"), http.StatusTooManyRequests, "spoofed header ignored")
	})

	t.Run("trusted proxy splits buckets per client", func(t *testing.T) {
		srv, _ := newLimitTestServer(t, func(c *config.Config) { c.Server.TrustedProxies = []string{"10.0.0.0/8"} })
		dir := t.TempDir()
		al, err := auth.OpenAuditLogger(dir)
		if err != nil {
			t.Fatalf("open audit logger: %v", err)
		}
		t.Cleanup(func() { al.Close() })
		srv.auditLog = al

		for i := 1; i <= loginIPLimit; i++ {
			expectStatus(t, loginVia(srv, "10.0.0.1:1", "203.0.113.9", fmt.Sprintf("user%d", i), "x"), http.StatusUnauthorized, fmt.Sprintf("attempt %d", i))
		}
		expectStatus(t, loginVia(srv, "10.0.0.1:1", "6.6.6.6, 203.0.113.9, 10.0.0.2", "alice", "secret"), http.StatusTooManyRequests, "same client via chained hops")
		expectStatus(t, loginVia(srv, "10.0.0.1:1", "203.0.113.10", "alice", "secret"), http.StatusOK, "other client via same proxy")

		events := readAuditEvents(t, filepath.Join(dir, "events.jsonl"))
		if len(events) == 0 || events[0].Action != "login_failed" || events[0].IP != "203.0.113.9" {
			t.Fatalf("first audit event = %+v, want login_failed from 203.0.113.9", events)
		}
		if last := events[len(events)-1]; last.IP != "203.0.113.10" {
			t.Errorf("last audit event IP = %q, want 203.0.113.10", last.IP)
		}
	})

	t.Run("trusted proxy keeps IPv6 /64 grouping", func(t *testing.T) {
		srv, _ := newLimitTestServer(t, func(c *config.Config) { c.Server.TrustedProxies = []string{"10.0.0.0/8"} })
		for i := 1; i <= loginIPLimit; i++ {
			expectStatus(t, loginVia(srv, "10.0.0.1:1", fmt.Sprintf("2001:db8::%x", i), fmt.Sprintf("user%d", i), "x"), http.StatusUnauthorized, fmt.Sprintf("attempt %d", i))
		}
		expectStatus(t, loginVia(srv, "10.0.0.1:1", "2001:db8::ffff", "alice", "secret"), http.StatusTooManyRequests, "same /64")
		expectStatus(t, loginVia(srv, "10.0.0.1:1", "2001:db8:0:1::1", "alice", "secret"), http.StatusOK, "other /64")
	})
}

// A flood of distinct usernames must neither lift an existing lockout nor
// lock out untracked users; only a table where every key is blocking fails closed.
func TestLoginLimit_FullTableKeepsLockouts(t *testing.T) {
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
	if wait := l.beginUser("newcomer"); wait != 0 {
		t.Fatalf("untracked username refused while the table is full: %v", wait)
	}
	if n := len(l.users.m); n > loginMaxKeys {
		t.Fatalf("tracked %d usernames, cap is %d", n, loginMaxKeys)
	}

	// Full of blocking entries only: fail closed instead of evicting a lockout.
	for k := range l.users.m {
		l.users.m[k] = &attemptCounter{n: loginMaxFailures, t: clock.now()}
	}
	if wait := l.beginUser("latecomer"); wait <= 0 {
		t.Fatal("evicted a lockout to admit a new username")
	}

	clock.advance(loginLockout)
	if wait := l.beginUser("latecomer"); wait != 0 {
		t.Fatalf("stale entries not pruned after the lockout window: %v", wait)
	}
}

func TestLoginLimit_FullIPTableAdmitsNewClient(t *testing.T) {
	l := newLoginLimiter()
	for i := 0; i < loginMaxKeys; i++ {
		l.allowIP(fmt.Sprintf("ip%d", i))
	}
	if wait := l.allowIP("192.0.2.200"); wait != 0 {
		t.Fatalf("new client refused while the IP table is full: %v", wait)
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

// #140: a store fault (here a cancelled request context) is not a failed
// login: it answers 500, is not audited, and does not consume the budget.
func TestLoginLimit_StoreErrorNotCountedAsFailure(t *testing.T) {
	srv, _ := newLimitTestServer(t)
	dir := t.TempDir()
	al, err := auth.OpenAuditLogger(dir)
	if err != nil {
		t.Fatalf("open audit logger: %v", err)
	}
	t.Cleanup(func() { al.Close() })
	srv.auditLog = al

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 1; i <= loginMaxFailures+1; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"alice","password":"secret"}`)).WithContext(ctx)
		req.RemoteAddr = "192.0.2.1:1000"
		srv.handleLogin(rec, req)
		expectStatus(t, rec, http.StatusInternalServerError, fmt.Sprintf("store error %d", i))
		assertErrorCode(t, rec, "internal_error")
	}
	if events := readAuditEvents(t, filepath.Join(dir, "events.jsonl")); len(events) != 0 {
		t.Fatalf("store errors audited: %+v", events)
	}
	for i := 1; i < loginMaxFailures; i++ {
		expectStatus(t, loginFrom(srv, "192.0.2.1:1000", "alice", "wrong"), http.StatusUnauthorized, fmt.Sprintf("failure %d after recovery", i))
	}
	expectStatus(t, loginFrom(srv, "192.0.2.1:1000", "alice", "secret"), http.StatusOK, "not locked by store errors")
}

// #140: the audited username is bounded like the limiter key.
func TestLoginLimit_AuditedUsernameTruncated(t *testing.T) {
	srv, _ := newLimitTestServer(t)
	dir := t.TempDir()
	al, err := auth.OpenAuditLogger(dir)
	if err != nil {
		t.Fatalf("open audit logger: %v", err)
	}
	t.Cleanup(func() { al.Close() })
	srv.auditLog = al

	long := strings.Repeat("p", 3000)
	expectStatus(t, loginFrom(srv, "192.0.2.1:1000", long, "x"), http.StatusUnauthorized, "long username")
	events := readAuditEvents(t, filepath.Join(dir, "events.jsonl"))
	if len(events) != 1 || events[0].Username != long[:loginMaxKeyLen] {
		t.Fatalf("audited username len = %v, want one event of %d bytes", len(events), loginMaxKeyLen)
	}
}

// #140: bodies over 4 KiB are rejected with 400 and not counted against the username.
func TestLoginLimit_OversizedBodyRejected(t *testing.T) {
	srv, _ := newLimitTestServer(t)
	for i := 1; i <= loginMaxFailures; i++ {
		rec := loginFrom(srv, "192.0.2.1:1000", "alice", strings.Repeat("x", 4<<10))
		expectStatus(t, rec, http.StatusBadRequest, fmt.Sprintf("oversized %d", i))
		assertErrorCode(t, rec, "bad_request")
	}
	expectStatus(t, loginFrom(srv, "192.0.2.1:1000", "alice", "secret"), http.StatusOK, "username not charged")
}

// #140: only credential and lockout failures are audited; IP-limited (429)
// and malformed (400) requests are not, as documented in the OpenAPI spec.
func TestLoginLimit_IPLimitedAndMalformedNotAudited(t *testing.T) {
	srv, _ := newLimitTestServer(t)
	dir := t.TempDir()
	al, err := auth.OpenAuditLogger(dir)
	if err != nil {
		t.Fatalf("open audit logger: %v", err)
	}
	t.Cleanup(func() { al.Close() })
	srv.auditLog = al

	expectStatus(t, loginFrom(srv, "192.0.2.1:1000", "", "x"), http.StatusBadRequest, "malformed")
	for i := 1; i < loginIPLimit; i++ {
		expectStatus(t, loginFrom(srv, "192.0.2.1:1000", "alice", "secret"), http.StatusOK, fmt.Sprintf("attempt %d", i))
	}
	expectStatus(t, loginFrom(srv, "192.0.2.1:1000", "alice", "wrong"), http.StatusTooManyRequests, "IP limited")

	for _, ev := range readAuditEvents(t, filepath.Join(dir, "events.jsonl")) {
		if ev.Action == "login_failed" {
			t.Fatalf("unexpected login_failed audit: %+v", ev)
		}
	}
}

// #140: with the prune sweep gated, stale entries are still evictable, so a
// full table of expired lockouts admits a new key at once.
func TestLoginLimit_FullTableEvictsStale(t *testing.T) {
	l := newLoginLimiter()
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	l.now = clock.now
	for i := 0; i < loginMaxKeys; i++ {
		l.users.m[fmt.Sprintf("u%d", i)] = &attemptCounter{n: loginMaxFailures, t: clock.now()}
	}
	clock.advance(loginLockout)
	l.users.lastPrune = clock.now()
	if wait := l.beginUser("newcomer"); wait != 0 {
		t.Fatalf("stale lockouts not evicted: %v", wait)
	}
}

// #140: when every entry is blocking, eviction scans a bounded number of
// entries instead of the whole table.
func TestLoginLimit_EvictionScanBounded(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := counterMap{m: map[string]*attemptCounter{}, ttl: loginLockout, limit: loginMaxFailures, lastPrune: now}
	for i := 0; i < loginMaxKeys; i++ {
		c.m[fmt.Sprintf("u%d", i)] = &attemptCounter{n: loginMaxFailures, t: now}
	}
	scanned, ok := c.evictOne(now)
	if ok || scanned > loginEvictScan {
		t.Fatalf("evictOne = (%d, %v), want (<= %d, false)", scanned, ok, loginEvictScan)
	}
}

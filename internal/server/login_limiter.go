package server

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Login brute-force protection (#74). State is in-memory: it resets on
// restart, which is acceptable for a single-process gateway.
const (
	loginMaxFailures = 5                // consecutive failures per username before lockout
	loginLockout     = 15 * time.Minute // lockout window, measured from the last counted attempt
	loginIPLimit     = 20               // login attempts per client IP per window
	loginIPWindow    = time.Minute
	loginMaxKeys     = 100_000 // bound on tracked usernames / IPs
	loginMaxKeyLen   = 256     // usernames are tracked by this many leading bytes
	loginEvictScan   = 1024    // max entries examined per eviction when the table is full
)

// attemptCounter counts attempts for one key; it is stale once ttl has
// elapsed since t.
type attemptCounter struct {
	n int
	t time.Time
}

type counterMap struct {
	m         map[string]*attemptCounter
	ttl       time.Duration
	limit     int // a live entry with n >= limit is actively blocking its key
	lastPrune time.Time
}

// get returns the live counter for key, replacing a stale or missing one
// with a fresh counter started at now. When the table is full it drops a
// non-blocking entry, so a flood of keys can neither lift an active lockout
// nor lock out untracked clients. It returns nil only when every tracked key
// is blocking: callers must then refuse the attempt (fail closed).
func (c *counterMap) get(key string, now time.Time) *attemptCounter {
	if e, ok := c.m[key]; ok && now.Sub(e.t) < c.ttl {
		return e
	}
	delete(c.m, key)
	if len(c.m) >= loginMaxKeys && now.Sub(c.lastPrune) >= time.Minute {
		c.lastPrune = now
		for k, e := range c.m {
			if now.Sub(e.t) >= c.ttl {
				delete(c.m, k)
			}
		}
	}
	if len(c.m) >= loginMaxKeys {
		if _, ok := c.evictOne(now); !ok {
			return nil
		}
	}
	e := &attemptCounter{t: now}
	c.m[key] = e
	return e
}

// evictOne drops one non-blocking or stale entry, scanning at most
// loginEvictScan entries so a table full of lockouts cannot make every
// request walk it under the limiter lock. It reports how many entries it
// scanned and whether one was dropped.
// shortcut: a table that is >99% live lockouts may refuse a new key although
// an evictable entry exists; track a non-blocking count if that ever matters.
func (c *counterMap) evictOne(now time.Time) (scanned int, ok bool) {
	for k, e := range c.m { // map order is random, so the victim is too
		if scanned == loginEvictScan {
			break
		}
		scanned++
		if e.n < c.limit || now.Sub(e.t) >= c.ttl {
			delete(c.m, k)
			return scanned, true
		}
	}
	return scanned, false
}

type loginLimiter struct {
	mu    sync.Mutex
	now   func() time.Time
	users counterMap // per-username consecutive attempts; t = last attempt
	ips   counterMap // per-IP attempts; t = window start
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		now:   time.Now,
		users: counterMap{m: map[string]*attemptCounter{}, ttl: loginLockout, limit: loginMaxFailures},
		ips:   counterMap{m: map[string]*attemptCounter{}, ttl: loginIPWindow, limit: loginIPLimit},
	}
}

// allowIP records one login attempt from ip. It returns 0 when allowed, or
// how long the caller must wait.
func (l *loginLimiter) allowIP(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	c := l.ips.get(ip, now)
	if c == nil {
		return loginIPWindow
	}
	if c.n >= loginIPLimit {
		return c.t.Add(loginIPWindow).Sub(now)
	}
	c.n++
	return 0
}

// beginUser reserves one password check for username. The attempt is counted
// as a failure up front, so concurrent guesses cannot exceed the limit before
// their results are known; succeeded clears it. It returns 0 when the check
// may proceed, or the remaining lockout.
func (l *loginLimiter) beginUser(username string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	c := l.users.get(userKey(username), now)
	if c == nil {
		return loginLockout
	}
	if c.n >= loginMaxFailures {
		return c.t.Add(loginLockout).Sub(now)
	}
	c.n++
	c.t = now
	return 0
}

// refundUser returns a reservation taken by beginUser whose password was never
// checked (a store fault), so infrastructure errors do not lock users out.
func (l *loginLimiter) refundUser(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e, ok := l.users.m[userKey(username)]; ok && e.n > 0 {
		e.n--
	}
}

// resetUser clears the username's failure counter and any active lockout:
// after a correct password, or on an admin unlock (#138).
func (l *loginLimiter) resetUser(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.users.m, userKey(username))
}

// userKey bounds the memory a single attacker-chosen username can pin.
// Over-long names sharing a prefix share a counter, which only locks sooner.
func userKey(username string) string {
	if len(username) > loginMaxKeyLen {
		return username[:loginMaxKeyLen]
	}
	return username
}

// writeTooManyLogins sends the single 429 shape used for both IP limiting and
// username lockout, so responses never reveal whether a username exists.
func writeTooManyLogins(w http.ResponseWriter, wait time.Duration) {
	secs := int((wait + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeAPIError(w, http.StatusTooManyRequests, "too_many_requests", "too many login attempts, try again later")
}

// loginIPKey returns the rate-limit key for a client address (as returned by
// clientAddr, with or without port): the host for IPv4, its /64 network for
// IPv6 (one host usually owns a whole /64).
func loginIPKey(addr string) string {
	host := addr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
	}
	return host
}

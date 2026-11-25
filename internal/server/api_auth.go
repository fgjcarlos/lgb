package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/fgjcarlos/lgb/internal/auth"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type tokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// sessionUserResponse is the body shape returned by /api/auth/me. The
// browser SPA calls it on mount to populate the auth context from the
// HttpOnly cookie — the client never sees the raw JWT. Fix for #78.
type sessionUserResponse struct {
	User      authUserView `json:"user"`
	ExpiresAt time.Time    `json:"expires_at"`
}

// authUserView is the minimal user shape returned to browser sessions.
// It mirrors the shape used by the JWT payload (id / username / role) so
// the SPA can drop it straight into its auth context without leaking the
// password hash or created_at fields of auth.User.
type authUserView struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// handleLogin authenticates a user and issues a JWT. The token is
// returned in the JSON body (kept for CLI / API tooling) AND set as an
// HttpOnly; SameSite=Strict cookie scoped to /api so browser sessions
// ride on the cookie without exposing the token to page scripts.
//
// Brute-force protection (#74): attempts are rate limited per client IP,
// and loginMaxFailures consecutive failures lock the username for
// loginLockout. Both answer the same 429 for known and unknown usernames.
// Credential failures and lockouts are audited as "login_failed"; store
// faults answer 500 and do not count against the username (#140).
// POST /api/auth/login (public)
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.userStore == nil || s.authTokens == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "unavailable", "auth not configured")
		return
	}

	if wait := s.loginLimiter.allowIP(loginIPKey(s.clientAddr(r))); wait > 0 {
		writeTooManyLogins(w, wait)
		return
	}

	// Bound the body so attacker-chosen usernames cannot bloat limiter state or the audit log.
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "username and password are required")
		return
	}

	if wait := s.loginLimiter.beginUser(req.Username); wait > 0 {
		s.auditLoginFailure(r, req.Username, "locked out")
		writeTooManyLogins(w, wait)
		return
	}

	user, err := s.userStore.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil && !errors.Is(err, auth.ErrUserNotFound) {
		// Only the user lookup can fail this way, before any password check.
		s.loginLimiter.refundUser(req.Username)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not verify credentials")
		return
	}
	if err != nil {
		s.auditLoginFailure(r, req.Username, "invalid credentials")
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}
	s.loginLimiter.resetUser(req.Username)

	token, err := s.authTokens.Issue(user.ID, user.Username, user.Role)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not issue token")
		return
	}

	// Parse the token to extract expiry for the response.
	claims, err := s.authTokens.Validate(token)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not validate token")
		return
	}

	// Set the HttpOnly session cookie. Secure is wired through cfg.Server.TLSEnabled.
	setSessionCookie(w, token, claims.ExpiresAt.Time, resolveSessionCookieConfig(s))

	if s.auditLog != nil {
		_ = s.auditLog.Log(auth.AuditEvent{
			Action:   "login",
			Username: user.Username,
			IP:       s.clientAddr(r),
		})
	}

	writeJSON(w, http.StatusOK, tokenResponse{
		Token:     token,
		ExpiresAt: claims.ExpiresAt.Time,
	})
}

type unlockRequest struct {
	Username string `json:"username"`
}

// handleUnlockLogin clears a username's login lockout (#138). Admin only via
// the route middleware. The username travels in the body because a URL path
// cannot carry every name intact (ServeMux cleans "." and ".." segments).
// It answers 204 whether or not the username exists or is locked, so it
// reveals neither.
func (s *Server) handleUnlockLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var req unlockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "username is required")
		return
	}
	// Audit first and fail closed: an unlock that cannot be recorded does not
	// happen. A nil audit log counts as unrecordable; the server always wires one.
	if s.auditLog == nil || s.auditLog.Log(auth.AuditEvent{
		Action:   "login.unlock",
		Username: actorFromContext(r),
		Detail:   fmt.Sprintf("username=%q", req.Username), // quoted: a crafted name cannot forge key=value pairs
		IP:       s.clientAddr(r),
	}) != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not record audit event")
		return
	}
	s.loginLimiter.resetUser(req.Username)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) auditLoginFailure(r *http.Request, username, detail string) {
	if s.auditLog == nil {
		return
	}
	_ = s.auditLog.Log(auth.AuditEvent{
		Action:   "login_failed",
		Username: userKey(username), // bounded: users sometimes type the password here
		Detail:   detail,
		IP:       s.clientAddr(r),
	})
}

// handleRefresh validates the current Bearer token and issues a fresh one with
// a new expiry. The route is gated by auth.Middleware so claims are in context.
// POST /api/auth/refresh (auth-required)
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if s.authTokens == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "unavailable", "auth not configured")
		return
	}

	// Extract the Bearer token from the Authorization header OR the
	// HttpOnly session cookie — middleware has already validated it, so
	// any of the two transports lets the refresh proceed. (#78)
	rawToken := auth.ExtractToken(r)
	if rawToken == "" {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing authorization token")
		return
	}

	claims, err := s.authTokens.Validate(rawToken)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
		return
	}

	// DB revalidation: ensure the user still exists and pick up current role.
	// Guard: skip if userStore is not wired (partial/test configuration).
	issueUsername := claims.Username
	issueRole := claims.Role
	if s.userStore != nil {
		user, err := s.userStore.GetByID(r.Context(), claims.UserID)
		if err != nil {
			if errors.Is(err, auth.ErrUserNotFound) {
				writeAPIError(w, http.StatusUnauthorized, "unauthorized", "user no longer exists")
				return
			}
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not revalidate user")
			return
		}
		issueUsername = user.Username
		issueRole = user.Role
	}

	newToken, err := s.authTokens.Issue(claims.UserID, issueUsername, issueRole)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not issue token")
		return
	}

	newClaims, err := s.authTokens.Validate(newToken)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not validate token")
		return
	}

	// Refresh the HttpOnly session cookie so the browser keeps its
	// authenticated session alive without ever touching the token in JS. (#78)
	setSessionCookie(w, newToken, newClaims.ExpiresAt.Time, resolveSessionCookieConfig(s))

	writeJSON(w, http.StatusOK, tokenResponse{
		Token:     newToken,
		ExpiresAt: newClaims.ExpiresAt.Time,
	})
}

// handleLogout invalidates the session cookie. The JWT itself is stateless
// (no server-side session store), so the only effect we can have from
// the server side is asking the browser to drop the cookie. A future
// improvement could add a token revocation list — out of scope for #78.
// POST /api/auth/logout (auth-required)
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w, resolveSessionCookieConfig(s))

	claims, _ := auth.ClaimsFromContext(r.Context())
	username := ""
	if claims != nil {
		username = claims.Username
	}
	if s.auditLog != nil {
		_ = s.auditLog.Log(auth.AuditEvent{
			Action:   "logout",
			Username: username,
			IP:       s.clientAddr(r),
		})
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleMe returns the authenticated user's profile from the session
// cookie. The SPA calls this on mount so the auth context can be
// populated without storing the JWT in JS. (#78)
// GET /api/auth/me (auth-required)
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing claims")
		return
	}

	writeJSON(w, http.StatusOK, sessionUserResponse{
		User: authUserView{
			ID:       claims.UserID,
			Username: claims.Username,
			Role:     string(claims.Role),
		},
		ExpiresAt: claims.ExpiresAt.Time,
	})
}

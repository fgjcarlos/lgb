package server

import (
	"encoding/json"
	"net/http"
	"strings"
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

// handleLogin authenticates a user and issues a JWT.
// POST /api/auth/login (public)
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.userStore == nil || s.authTokens == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "unavailable", "auth not configured")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "username and password are required")
		return
	}

	user, err := s.userStore.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}

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

	if s.auditLog != nil {
		_ = s.auditLog.Log(auth.AuditEvent{
			Action:   "login",
			Username: user.Username,
			IP:       r.RemoteAddr,
		})
	}

	writeJSON(w, http.StatusOK, tokenResponse{
		Token:     token,
		ExpiresAt: claims.ExpiresAt.Time,
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

	// Extract the Bearer token from the Authorization header.
	rawToken := ""
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		rawToken = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}
	if rawToken == "" {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing authorization token")
		return
	}

	claims, err := s.authTokens.Validate(rawToken)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
		return
	}

	newToken, err := s.authTokens.Issue(claims.UserID, claims.Username, claims.Role)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not issue token")
		return
	}

	newClaims, err := s.authTokens.Validate(newToken)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not validate token")
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse{
		Token:     newToken,
		ExpiresAt: newClaims.ExpiresAt.Time,
	})
}

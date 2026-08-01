package server

import (
	"net/http"
	"time"
)

// sessionCookieName is the name of the HttpOnly cookie that carries the
// bearer token used by browser sessions. The value is a JWT identical in
// shape to the one returned in the JSON body of /api/auth/login, so server
// handlers and middleware can stay token-agnostic about transport.
// Fix for #78 — the prior approach stored the token in localStorage on the
// client, which is exfiltrable by any injected script (dependency
// compromise, malicious extension, future XSS). HttpOnly + SameSite=Strict
// removes that attack surface.
const sessionCookieName = "lgb_session"

// defaultSessionCookiePath restricts the cookie to the /api tree so the
// SPA's static assets never see it. /api/auth/login and /api/auth/refresh
// both live under this path.
const defaultSessionCookiePath = "/api"

// sessionCookieConfig gathers the knobs the auth handlers need when
// (re)issuing or clearing the session cookie. Secure is wired through
// cfg.Server.TLSEnabled so the dev stack (plain http://localhost:8080)
// keeps working; production deployments behind TLS get Secure=true.
type sessionCookieConfig struct {
	Secure   bool
	SameSite http.SameSite
}

// resolveSessionCookieConfig maps the server config to a cookie policy.
// SameSite=Strict is the conservative choice for an internal gateway UI:
// the SPA and the API share an origin, so Lax buys nothing and Strict
// closes the CSRF surface entirely.
func resolveSessionCookieConfig(s *Server) sessionCookieConfig {
	return sessionCookieConfig{
		Secure:   s.cfg.Server.TLSEnabled,
		SameSite: http.SameSiteStrictMode,
	}
}

// setSessionCookie writes the bearer token to the HttpOnly session cookie.
// The expiry matches the JWT's exp claim — the cookie outlives the tab
// but expires with the token, so there is no separate server-side
// invalidation window for an idle browser session.
func setSessionCookie(w http.ResponseWriter, token string, expires time.Time, cfg sessionCookieConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     defaultSessionCookiePath,
		Expires:  expires,
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: cfg.SameSite,
	})
}

// clearSessionCookie expires the session cookie immediately. Used by
// /api/auth/logout so the browser drops the cookie without waiting for
// the JWT exp.
func clearSessionCookie(w http.ResponseWriter, cfg sessionCookieConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     defaultSessionCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: cfg.SameSite,
	})
}

package server

import "net/http"

// securityHeadersMiddleware sets security-related response headers on every
// HTTP response, regardless of route (API, SPA, health, metrics, WS upgrade).
//
// Headers set (R75-2):
//   - X-Content-Type-Options: nosniff
//   - X-Frame-Options: DENY
//   - Referrer-Policy: strict-origin-when-cross-origin
//   - Content-Security-Policy: (see const below)
//
// Headers deliberately NOT set:
//   - X-XSS-Protection: deprecated; can introduce vulnerabilities in older IE (R75-2c)
//   - Permissions-Policy: out of scope for this OT system
//
// Implementation note: headers are set before next.ServeHTTP is called, so
// they are present even on WebSocket upgrade responses (101 Switching
// Protocols). The coder/websocket library writes 101 through the same
// http.ResponseWriter, which already has these headers in its map.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	const csp = "default-src 'self'; script-src 'self'; style-src 'self'; " +
		"img-src 'self' data:; connect-src 'self' ws: wss:; " +
		"font-src 'self'; object-src 'none'; frame-ancestors 'none'"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", csp)
		next.ServeHTTP(w, r)
	})
}

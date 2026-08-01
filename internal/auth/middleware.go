package auth

import (
	"context"
	"net/http"
	"strings"
)

type contextKey int

const claimsKey contextKey = 1

func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsKey).(*Claims)
	return c, ok
}

// WithClaims returns a copy of ctx that carries the provided claims. It is
// the symmetric counterpart to ClaimsFromContext and exists so tests (and
// any future tooling) can attach claims without going through the full
// Middleware chain. The unexported claimsKey is shared with Middleware so
// the value is observable via ClaimsFromContext downstream. Fix for #78.
func WithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

func Middleware(ts *TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := ExtractToken(r)
			if token == "" {
				http.Error(w, `{"error":"missing authorization token"}`, http.StatusUnauthorized)
				return
			}
			claims, err := ts.Validate(token)
			if err != nil {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireRole(roles ...Role) func(http.Handler) http.Handler {
	allowed := make(map[Role]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFromContext(r.Context())
			if !ok {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			if !allowed[claims.Role] {
				http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r.WithContext(r.Context()))
		})
	}
}

// BearerToken extracts a JWT from the Authorization header.
// It accepts only the "Bearer <token>" form — query-parameter tokens are
// rejected to comply with R71-1 (no credential transport via URL).
// Returns an empty string when the header is absent or malformed.
func BearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return auth[7:]
	}
	return ""
}

// CookieToken extracts a JWT from the session cookie set by
// server.handleLogin. Returns "" when the cookie is absent. The middleware
// accepts either transport so existing tooling (CLI, tests) that uses
// Authorization: Bearer keeps working without change. Fix for #78.
func CookieToken(r *http.Request) string {
	c, err := r.Cookie("lgb_session")
	if err != nil {
		return ""
	}
	return c.Value
}

// ExtractToken returns the bearer token from any supported transport:
// Authorization: Bearer header first, then the lgb_session cookie. The
// cookie path lets browser sessions ride on HttpOnly without changing the
// server-side validate contract. Fix for #78.
func ExtractToken(r *http.Request) string {
	if t := BearerToken(r); t != "" {
		return t
	}
	return CookieToken(r)
}

package config

import "testing"

// TestEnvLookup covers the lookup behaviour that lets the loader honour
// env-var names with extra underscores (e.g. AUTH_JWT_SECRET) alongside
// the canonical names derived from camelCase struct tags (e.g.
// AUTH_JWTSECRET). Fix for #65.
func TestEnvLookup(t *testing.T) {
	cases := []struct {
		suffix string
		want   string // empty means no match expected
	}{
		// Canonical forms — exact match.
		{"AUTH_JWTSECRET", "auth.jwtSecret"},
		{"GATEWAY_LOGLEVEL", "gateway.logLevel"},
		{"SERVER_HTTPADDR", "server.httpAddr"},
		// Spelled-out forms with one extra underscore — match via
		// underscore-removal fallback.
		{"AUTH_JWT_SECRET", "auth.jwtSecret"},
		// Unrelated / unknown suffix — no match.
		{"UNKNOWN_FIELD", ""},
	}
	for _, c := range cases {
		got := envLookup(c.suffix)
		if got != c.want {
			t.Errorf("envLookup(%q) = %q; want %q", c.suffix, got, c.want)
		}
	}
}

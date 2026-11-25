package server

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// Trusted-proxy client IP resolution tests (#137).

func TestResolveClientAddr(t *testing.T) {
	trusted := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("fd00::/8"),
	}
	cases := []struct {
		name      string
		trusted   []netip.Prefix
		remote    string
		xff       []string
		forwarded string
		want      string
	}{
		{"no trusted proxies ignores headers", nil, "10.0.0.1:1234", []string{"203.0.113.9"}, "", "10.0.0.1:1234"},
		{"untrusted peer with spoofed XFF", trusted, "198.51.100.7:1234", []string{"203.0.113.9"}, "", "198.51.100.7:1234"},
		{"untrusted peer with spoofed Forwarded", trusted, "198.51.100.7:1234", nil, "for=203.0.113.9", "198.51.100.7:1234"},
		{"trusted proxy without header", trusted, "10.0.0.1:1234", nil, "", "10.0.0.1:1234"},
		{"trusted proxy single hop", trusted, "10.0.0.1:1234", []string{"203.0.113.9"}, "", "203.0.113.9"},
		{"trusted proxy chained hops", trusted, "10.0.0.1:1234", []string{"6.6.6.6, 203.0.113.9, 10.0.0.2"}, "", "203.0.113.9"},
		{"chained hops across header lines", trusted, "10.0.0.1:1234", []string{"6.6.6.6", "203.0.113.9", "10.0.0.2"}, "", "203.0.113.9"},
		{"all hops trusted returns left-most", trusted, "10.0.0.1:1234", []string{"10.0.0.3, 10.0.0.2"}, "", "10.0.0.3"},
		{"garbage hop falls back to last trusted", trusted, "10.0.0.1:1234", []string{"203.0.113.9, garbage, 10.0.0.2"}, "", "10.0.0.2"},
		{"garbage only falls back to peer", trusted, "10.0.0.1:1234", []string{"garbage"}, "", "10.0.0.1:1234"},
		{"XFF hop with port", trusted, "10.0.0.1:1234", []string{"203.0.113.9:5555"}, "", "203.0.113.9"},
		{"Forwarded single hop", trusted, "10.0.0.1:1234", nil, "for=203.0.113.9;proto=https", "203.0.113.9"},
		{"Forwarded chained IPv6 quoted", trusted, "10.0.0.1:1234", nil, `for=6.6.6.6, For="[2001:db8::1]:4711", for=10.0.0.2`, "2001:db8::1"},
		{"Forwarded unknown falls back", trusted, "10.0.0.1:1234", nil, "for=unknown", "10.0.0.1:1234"},
		{"XFF wins over Forwarded", trusted, "10.0.0.1:1234", []string{"203.0.113.9"}, "for=6.6.6.6", "203.0.113.9"},
		{"trusted IPv6 proxy", trusted, "[fd00::1]:443", []string{"2001:db8::5"}, "", "2001:db8::5"},
		{"zoned hop stops the walk", trusted, "10.0.0.1:1234", []string{"9.9.9.9, [fd00::1%eth0]:80"}, "", "10.0.0.1:1234"},
		{"empty XFF line does not fall through to Forwarded", trusted, "10.0.0.1:1234", []string{""}, "for=6.6.6.6", "10.0.0.1:1234"},
		{"mapped hop normalised", trusted, "10.0.0.1:1234", []string{"9.9.9.9, ::ffff:1.2.3.4"}, "", "1.2.3.4"},
		{"IPv4-mapped peer is trusted", trusted, "[::ffff:10.0.0.1]:443", []string{"203.0.113.9"}, "", "203.0.113.9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remote
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if tc.forwarded != "" {
				r.Header.Set("Forwarded", tc.forwarded)
			}
			if got := resolveClientAddr(r, tc.trusted); got != tc.want {
				t.Errorf("resolveClientAddr = %q, want %q", got, tc.want)
			}
		})
	}
}

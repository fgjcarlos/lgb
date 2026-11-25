package server

import (
	"net/http"
	"net/netip"
	"strings"
)

// Client IP resolution behind reverse proxies (#137).

// parseTrustedProxies converts server.trustedProxies to prefixes. Config
// validation already rejects invalid CIDRs, so bad entries are skipped here.
func parseTrustedProxies(cidrs []string) []netip.Prefix {
	var out []netip.Prefix
	for _, c := range cidrs {
		if p, err := netip.ParsePrefix(c); err == nil {
			out = append(out, p.Masked())
		}
	}
	return out
}

// resolveClientAddr returns the client address used for rate limiting and
// audit events. Unless the TCP peer is a trusted proxy it returns r.RemoteAddr
// unchanged ("host:port"), so forwarding headers from anyone else are ignored. From a
// trusted peer it walks X-Forwarded-For (or Forwarded when XFF is absent)
// right to left and returns the first untrusted hop as a bare IP (no port). An unparseable hop stops
// the walk at the last trusted address, so it is never attacker-chosen; if
// every hop is trusted the left-most one is the client.
func resolveClientAddr(r *http.Request, trusted []netip.Prefix) string {
	peer, ok := parseHop(r.RemoteAddr)
	if !ok || !isTrusted(peer, trusted) {
		return r.RemoteAddr
	}
	hops := forwardedHops(r.Header)
	client := r.RemoteAddr
	for i := len(hops) - 1; i >= 0; i-- {
		ip, ok := parseHop(hops[i])
		if !ok {
			break
		}
		client = ip.String()
		if !isTrusted(ip, trusted) {
			break
		}
	}
	return client
}

func isTrusted(ip netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// forwardedHops lists the client chain, left-most first, from
// X-Forwarded-For or, when that is absent, the for= values of Forwarded
// (RFC 7239). Multiple header lines are concatenated in order.
func forwardedHops(h http.Header) []string {
	var hops []string
	if xff := h.Values("X-Forwarded-For"); len(xff) > 0 {
		for _, line := range xff {
			for _, hop := range strings.Split(line, ",") {
				hops = append(hops, strings.TrimSpace(hop))
			}
		}
		return hops
	}
	for _, line := range h.Values("Forwarded") {
		for _, elem := range strings.Split(line, ",") {
			hop := "" // an element without for= is an unknown hop
			for _, pair := range strings.Split(elem, ";") {
				k, v, _ := strings.Cut(strings.TrimSpace(pair), "=")
				if strings.EqualFold(k, "for") {
					hop = strings.Trim(v, `"`)
				}
			}
			hops = append(hops, hop)
		}
	}
	return hops
}

// parseHop parses "ip", "ip:port", "[ipv6]" or "[ipv6]:port". IPv4-mapped
// IPv6 addresses are unmapped so they match IPv4 CIDRs.
func parseHop(s string) (netip.Addr, bool) {
	var ip netip.Addr
	if ap, err := netip.ParseAddrPort(s); err == nil {
		ip = ap.Addr()
	} else if a, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")); err == nil {
		ip = a
	} else {
		return netip.Addr{}, false
	}
	if ip.Zone() != "" { // zoned addresses never match a CIDR
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

// clientAddr is the request's client address under this server's
// trusted-proxy configuration; see resolveClientAddr for its format.
func (s *Server) clientAddr(r *http.Request) string {
	return resolveClientAddr(r, s.trustedProxies)
}

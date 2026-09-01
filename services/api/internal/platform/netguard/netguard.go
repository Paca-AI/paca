// Package netguard provides SSRF-safe outbound HTTP dialing: it re-resolves
// a destination's DNS at dial time and rejects private/internal IPs,
// closing the DNS-rebinding gap between an upfront URL check and the actual
// connection. Originally built for the plugin fetch/marketplace/installer
// clients (internal/platform/plugin); extracted here so other packages
// (e.g. the automation engine's call_api action) can reuse it without
// importing the plugin package's much larger dependency graph.
package netguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// IsPrivateOrInternalIP reports whether ip is a loopback, link-local,
// private-range, unspecified, or otherwise non-publicly-routable address
// (RFC 1918 and CGNAT IPv4 ranges, IPv6 unique local and multicast
// addresses). It also unwraps IPv6 transition-mechanism literals (6to4,
// NAT64, Teredo, and the deprecated IPv4-compatible format) and recurses on
// any IPv4 address they carry — otherwise a caller can smuggle a private or
// internal IPv4 target past every check above by encoding it as a
// public-looking IPv6 literal.
func IsPrivateOrInternalIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsUnspecified() {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return true
	}

	privateIPv4Ranges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16", // link-local
		"100.64.0.0/10",  // CGNAT shared address space (RFC 6598) — some cloud metadata endpoints live here
	}
	for _, cidr := range privateIPv4Ranges {
		_, ipNet, _ := net.ParseCIDR(cidr)
		if ipNet.Contains(ip) {
			return true
		}
	}

	if ip.To4() == nil { // IPv6
		// Unique local addresses (fc00::/7)
		if len(ip) == 16 && (ip[0]&0xfe) == 0xfc {
			return true
		}
	}

	for _, embedded := range embeddedIPv4s(ip) {
		if IsPrivateOrInternalIP(embedded) {
			return true
		}
	}

	return false
}

// embeddedIPv4s returns the IPv4 addresses carried inside an IPv6
// transition-mechanism literal: 6to4 (2002::/16), NAT64 (the well-known
// 64:ff9b::/96 prefix and the 64:ff9b:1::/48 local-use prefix), Teredo
// (2001::/32), and the deprecated IPv4-compatible format (::a.b.c.d). These
// mechanisms exist to route IPv6 traffic to a real IPv4 destination, so a
// hand-crafted literal that satisfies every check above as "IPv6" can still
// carry an embedded IPv4 address that routes straight to an internal host or
// a cloud metadata endpoint.
//
// Teredo carries two independent, attacker-controlled IPv4 fields — the
// relay/server address at bytes 4-7 (plain) and the client address at bytes
// 12-15 (XORed with 0xff per RFC 4380) — so both are returned; it takes only
// one of them resolving private for the address to be rejected.
func embeddedIPv4s(ip net.IP) []net.IP {
	ip16 := ip.To16()
	if ip16 == nil || ip.To4() != nil {
		return nil
	}

	switch {
	case ip16[0] == 0x20 && ip16[1] == 0x02:
		// 6to4: 2002:WWXX:YYZZ::/16 encodes the IPv4 address as hex octets
		// in bytes 2-5.
		return []net.IP{net.IPv4(ip16[2], ip16[3], ip16[4], ip16[5])}

	case ip16[0] == 0x00 && ip16[1] == 0x64 && ip16[2] == 0xff && ip16[3] == 0x9b:
		// 64:ff9b:1::/48 is reserved for local NAT64 deployments (RFC 8215)
		// and is never a legitimate public destination — block the whole
		// prefix regardless of the trailing bits.
		if ip16[4] == 0x00 && ip16[5] == 0x01 {
			return []net.IP{net.IPv4zero}
		}
		// The well-known NAT64 prefix 64:ff9b::/96 embeds the IPv4 address
		// in the last 32 bits (bytes 12-15).
		for _, b := range ip16[4:12] {
			if b != 0 {
				return nil
			}
		}
		return []net.IP{net.IPv4(ip16[12], ip16[13], ip16[14], ip16[15])}

	case ip16[0] == 0x20 && ip16[1] == 0x01 && ip16[2] == 0x00 && ip16[3] == 0x00:
		// Teredo: 2001:0000:Server:Flags:Port:Client.
		return []net.IP{
			net.IPv4(ip16[4], ip16[5], ip16[6], ip16[7]),
			net.IPv4(ip16[12]^0xff, ip16[13]^0xff, ip16[14]^0xff, ip16[15]^0xff),
		}
	}

	// Deprecated IPv4-compatible format: ::a.b.c.d (top 96 bits zero; not
	// already unwrapped by To4(), which only handles ::ffff:a.b.c.d).
	for _, b := range ip16[0:12] {
		if b != 0 {
			return nil
		}
	}
	return []net.IP{net.IPv4(ip16[12], ip16[13], ip16[14], ip16[15])}
}

// SafeDialContext resolves addr's host, rejects any resolved IP that
// IsPrivateOrInternalIP flags, and dials the checked IP directly (rather
// than re-resolving inside the dialer) so the address actually connected to
// is the same one just validated.
func SafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("safe dial: invalid address %q: %w", addr, err)
	}

	resolver := &net.Resolver{}
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("safe dial: resolve %q: %w", host, err)
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}

	var lastErr error
	for _, ipAddr := range ips {
		if IsPrivateOrInternalIP(ipAddr.IP) {
			lastErr = fmt.Errorf("safe dial: %q resolves to private/internal IP address: %s", host, ipAddr.IP.String())
			continue
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ipAddr.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("safe dial: no addresses found for %q", host)
	}
	return nil, lastErr
}

// NewSafeHTTPTransport returns an *http.Transport whose DialContext is
// SafeDialContext, for callers that want to plug it into their own
// http.Client (e.g. one with a custom Timeout).
func NewSafeHTTPTransport() *http.Transport {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.DialContext = SafeDialContext
	return base
}

// NewSafeHTTPClient is a convenience constructor for the common case of just
// wanting a ready-to-use SSRF-safe client with a fixed timeout.
func NewSafeHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: NewSafeHTTPTransport()}
}

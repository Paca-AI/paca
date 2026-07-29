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

// IsPrivateOrInternalIP reports whether ip is a loopback, link-local, or
// private-range address (RFC 1918 IPv4 ranges, IPv6 unique local addresses).
func IsPrivateOrInternalIP(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	privateIPv4Ranges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16", // link-local
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

	return false
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

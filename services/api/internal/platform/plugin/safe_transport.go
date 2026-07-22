package plugin

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// newSafeHTTPTransport returns an http.Transport whose DialContext re-validates
// the destination against the private/internal IP guard at the moment it
// connects, and dials the checked IP directly.
//
// validateMarketplaceURL performs the same check, but only once, before the
// request is issued. Between that check and the actual connection, a second
// DNS lookup for the same hostname (as happens inside the default dialer, and
// again on every HTTP redirect) could resolve to a different, private
// address — a classic DNS-rebinding bypass. Pinning the transport's dial to
// the IP this function itself resolved and checked closes that window.
func newSafeHTTPTransport() *http.Transport {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.DialContext = safeDialContext
	return base
}

func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
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
		if isPrivateOrInternalIP(ipAddr.IP) {
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

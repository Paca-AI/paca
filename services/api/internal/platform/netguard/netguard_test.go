package netguard

import (
	"context"
	"net"
	"strings"
	"testing"
)

// TestSafeDialContext_RejectsPrivateIPLiteral ensures the dial-time guard
// itself blocks a private address, independent of any upfront URL check —
// this is the boundary that actually prevents DNS-rebinding bypasses, since
// it re-checks at the moment of connecting.
func TestSafeDialContext_RejectsPrivateIPLiteral(t *testing.T) {
	conn, err := SafeDialContext(context.Background(), "tcp", "127.0.0.1:443")
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected error dialing loopback address, got nil")
	}
	if !strings.Contains(err.Error(), "private/internal IP") {
		t.Fatalf("expected private/internal IP error, got: %v", err)
	}
}

// TestSafeDialContext_RejectsHostnameResolvingToLoopback covers the case a
// hostname (not an IP literal) resolves to a private address — "localhost"
// resolves via the OS hosts file with no network dependency, so this is
// deterministic in any test environment.
func TestSafeDialContext_RejectsHostnameResolvingToLoopback(t *testing.T) {
	conn, err := SafeDialContext(context.Background(), "tcp", "localhost:443")
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected error dialing a hostname that resolves to loopback, got nil")
	}
	if !strings.Contains(err.Error(), "private/internal IP") {
		t.Fatalf("expected private/internal IP error, got: %v", err)
	}
}

func TestSafeDialContext_RejectsAddressWithoutPort(t *testing.T) {
	_, err := SafeDialContext(context.Background(), "tcp", "example.com")
	if err == nil {
		t.Fatal("expected error for address missing a port, got nil")
	}
	if !strings.Contains(err.Error(), "invalid address") {
		t.Fatalf("expected invalid address error, got: %v", err)
	}
}

func TestIsPrivateOrInternalIP(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want bool
	}{
		{"loopback v4", "127.0.0.1", true},
		{"loopback v6", "::1", true},
		{"unspecified v4", "0.0.0.0", true},
		{"unspecified v6", "::", true},
		{"rfc1918 10/8", "10.1.2.3", true},
		{"rfc1918 172.16/12", "172.20.0.1", true},
		{"rfc1918 192.168/16", "192.168.1.1", true},
		{"link-local / cloud metadata", "169.254.169.254", true},
		{"cgnat shared space", "100.64.0.1", true},
		{"ipv4 multicast", "224.0.0.1", true},
		{"ipv6 multicast", "ff02::1", true},
		{"ipv6 unique local", "fd00::1", true},
		{"ipv4-mapped private", "::ffff:10.0.0.1", true},
		{"public v4", "8.8.8.8", false},
		{"public v6", "2001:4860:4860::8888", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ip := net.ParseIP(c.ip)
			if ip == nil {
				t.Fatalf("failed to parse test IP %q", c.ip)
			}
			if got := IsPrivateOrInternalIP(ip); got != c.want {
				t.Errorf("IsPrivateOrInternalIP(%q) = %v, want %v", c.ip, got, c.want)
			}
		})
	}
}

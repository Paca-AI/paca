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

		// GHSA-cj3q-c44j-q8p9: IPv6 transition addresses that embed a
		// private/internal IPv4 target must be blocked even though the
		// literal itself isn't in any IPv4 CIDR or the ULA range.
		{"6to4 metadata addr", "2002:a9fe:a9fe::", true},
		{"6to4 loopback", "2002:7f00:1::", true},
		{"6to4 rfc1918", "2002:c0a8:1::", true},
		{"6to4 public (sanity)", "2002:0808:0808::", false},
		{"nat64 well-known metadata addr", "64:ff9b::a9fe:a9fe", true},
		{"nat64 well-known loopback", "64:ff9b::7f00:1", true},
		{"nat64 well-known public (sanity)", "64:ff9b::808:808", false},
		{"nat64 local-use prefix", "64:ff9b:1::1234", true},
		// Teredo carries two independent attacker-controlled IPv4 fields:
		// the unobfuscated relay/server address (bytes 4-7) and the
		// XOR-obfuscated client address (bytes 12-15). Both must be
		// checked — these two cases isolate each field by keeping the
		// other one public, so neither test can pass on the strength of
		// the other field alone.
		{"teredo server field is metadata addr, client field public", "2001:0:a9fe:a9fe:0:0:f7f7:fbfb", true},
		{"teredo client field is metadata addr, server field public", "2001:0:808:808:0:0:5601:5601", true},
		{"teredo both fields public (sanity)", "2001:0:808:808:0:0:f7f7:fbfb", false},
		{"ipv4-compatible metadata addr", "::a9fe:a9fe", true},
		{"ipv4-compatible public (sanity)", "::808:808", false},
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

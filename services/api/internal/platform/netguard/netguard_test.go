package netguard

import (
	"context"
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

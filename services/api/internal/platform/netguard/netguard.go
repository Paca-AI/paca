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
	"sort"
	"strings"
	"time"

	"golang.org/x/net/idna"
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
	return newGuardedDialer(nil).dialContext(ctx, network, addr)
}

// guardedDialer is the implementation behind every dial in this package.
// hosts is nil for all callers except the plugin fetch client (see
// NewSafeHTTPClientWithPrivateHosts), and even there a host gets the exception
// only if the dial's context also allows it (WithAllowedPrivateHosts).
type guardedDialer struct {
	hosts  *PrivateHosts
	lookup func(ctx context.Context, host string) ([]net.IPAddr, error)
	dial   func(ctx context.Context, network, addr string) (net.Conn, error)
}

func newGuardedDialer(hosts *PrivateHosts) guardedDialer {
	resolver := &net.Resolver{}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return guardedDialer{hosts: hosts, lookup: resolver.LookupIPAddr, dial: dialer.DialContext}
}

func (d guardedDialer) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("safe dial: invalid address %q: %w", addr, err)
	}

	ips, err := d.lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("safe dial: resolve %q: %w", host, err)
	}

	hosts := d.hosts
	if allowed, _ := ctx.Value(allowedPrivateHostsKey{}).(*PrivateHosts); !allowed.Contains(host) {
		hosts = nil
	}

	var lastErr error
	for _, ipAddr := range ips {
		if hosts.Blocks(host, ipAddr.IP) {
			lastErr = fmt.Errorf("safe dial: %q resolves to private/internal IP address: %s", host, ipAddr.IP.String())
			continue
		}
		conn, dialErr := d.dial(ctx, network, net.JoinHostPort(ipAddr.IP.String(), port))
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

// NewSafeHTTPClientWithPrivateHosts is NewSafeHTTPClient with the exception
// described on PrivateHosts. The dial stays pinned to the IP it just checked.
// A nil or empty hosts behaves exactly like NewSafeHTTPClient. Only the
// paca.fetch client should use this: the exception is an operator opt-in for
// plugin traffic, not a general relaxation of the guard.
//
// The exception further requires the request's context to name the host via
// WithAllowedPrivateHosts. One client serves every plugin, so this is how a
// dial learns which plugin it is for; a request without it gets no exception.
func NewSafeHTTPClientWithPrivateHosts(timeout time.Duration, hosts *PrivateHosts) *http.Client {
	transport := NewSafeHTTPTransport()
	transport.DialContext = newGuardedDialer(hosts).dialContext
	return &http.Client{Timeout: timeout, Transport: transport}
}

type allowedPrivateHostsKey struct{}

// WithAllowedPrivateHosts returns a copy of ctx in which a dial by the client
// of NewSafeHTTPClientWithPrivateHosts may use the exception for the hosts in
// allowed, if the operator's set lists them too. net/http dials with the
// request's context, redirects included, so the decision follows the request.
func WithAllowedPrivateHosts(ctx context.Context, allowed *PrivateHosts) context.Context {
	return context.WithValue(ctx, allowedPrivateHostsKey{}, allowed)
}

// PrivateHosts is an operator-managed set of exact hostnames that may resolve
// to private network addresses. Only RFC 1918 and unique local (fc00::/7)
// addresses become reachable, and only for these names. Everything else
// IsPrivateOrInternalIP flags stays blocked even for a listed host: loopback,
// link-local, CGNAT (cloud metadata lives there too), unspecified, multicast,
// and IPv6 transition literals, whatever IPv4 address they carry.
//
// A nil *PrivateHosts is valid and is the empty set.
type PrivateHosts struct {
	names map[string]struct{}
}

// NewPrivateHosts builds the set from raw entries and returns those it refused:
// IP literals, wildcards, and anything else that isn't a bare hostname, such as
// a URL or a host:port. Refusing them keeps the exception tied to a name the
// operator chose rather than to an address range. Matching is case-insensitive.
//
// Entries must already be in the ASCII form CanonicalHost produces, so names
// with a leading or trailing dot and non-ASCII names are refused too, rather
// than rewritten: an internationalized name has to be listed in punycode. The
// set then holds exactly the names that are matched, and Names logs them as
// the dialer will see them.
func NewPrivateHosts(entries []string) (*PrivateHosts, []string) {
	names := make(map[string]struct{}, len(entries))
	var rejected []string
	for _, entry := range entries {
		name := strings.ToLower(strings.TrimSpace(entry))
		if name == "" {
			continue
		}
		if net.ParseIP(name) != nil || strings.ContainsAny(name, "*/:[] \t") ||
			strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || !isASCII(name) {
			rejected = append(rejected, entry)
			continue
		}
		names[name] = struct{}{}
	}
	return &PrivateHosts{names: names}, rejected
}

// CanonicalHost returns host in the form net/http dials it, lowercased. Like
// the transport, it passes a non-ASCII name through the IDNA lookup profile, so
// "ｇｉｔｌａｂ.internal" (fullwidth letters) or "git\u00adlab.internal" (soft
// hyphen) both become "gitlab.internal". Any hostname compared against
// PrivateHosts or a plugin manifest must go through it first: a raw comparison
// would see a different name from the one that is dialed. An error means the
// name has no ASCII form, and callers must refuse it.
func CanonicalHost(host string) (string, error) {
	if !isASCII(host) {
		ascii, err := idna.Lookup.ToASCII(host)
		if err != nil {
			return "", fmt.Errorf("netguard: invalid hostname %q: %w", host, err)
		}
		host = ascii
	}
	return strings.ToLower(host), nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// Empty reports whether the set has no hostnames.
func (p *PrivateHosts) Empty() bool {
	return p == nil || len(p.names) == 0
}

// Names returns the hostnames in the set, sorted.
func (p *PrivateHosts) Names() []string {
	if p.Empty() {
		return nil
	}
	names := make([]string, 0, len(p.names))
	for name := range p.names {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Contains reports whether host, after CanonicalHost, is in the set. A host
// with no canonical form is never contained.
func (p *PrivateHosts) Contains(host string) bool {
	if p.Empty() {
		return false
	}
	name, err := CanonicalHost(host)
	if err != nil {
		return false
	}
	_, ok := p.names[name]
	return ok
}

// Blocks reports whether connecting to ip for host must be refused:
// IsPrivateOrInternalIP, minus the exception this set grants. net.IP.IsPrivate
// covers exactly the ranges the exception is for, and it is false for IPv6
// transition literals, so an embedded address never qualifies.
func (p *PrivateHosts) Blocks(host string, ip net.IP) bool {
	if !IsPrivateOrInternalIP(ip) {
		return false
	}
	return !p.Contains(host) || !ip.IsPrivate()
}

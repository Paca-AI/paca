package netguard

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
)

func mustPrivateHosts(t *testing.T, entries ...string) *PrivateHosts {
	t.Helper()
	hosts, rejected := NewPrivateHosts(entries)
	if len(rejected) != 0 {
		t.Fatalf("unexpected rejected entries: %v", rejected)
	}
	return hosts
}

func TestNewPrivateHosts(t *testing.T) {
	hosts, rejected := NewPrivateHosts([]string{
		" GitLab.Internal ", "", "git.corp.example",
		"10.0.0.5", "fd00::1", "[fd00::1]", "*.corp.example", "*",
		"https://gitlab.internal", "gitlab.internal:443", "a b",
		"gitlab.internal.", ".corp.example", "ｇｉｔｌａｂ.internal", "bücher.example", "xn--bcher-kva.example",
	})

	wantNames := []string{"git.corp.example", "gitlab.internal", "xn--bcher-kva.example"}
	if got := hosts.Names(); !reflect.DeepEqual(got, wantNames) {
		t.Errorf("Names() = %v, want %v", got, wantNames)
	}
	wantRejected := []string{"10.0.0.5", "fd00::1", "[fd00::1]", "*.corp.example", "*", "https://gitlab.internal", "gitlab.internal:443", "a b",
		"gitlab.internal.", ".corp.example", "ｇｉｔｌａｂ.internal", "bücher.example"}
	if !reflect.DeepEqual(rejected, wantRejected) {
		t.Errorf("rejected = %v, want %v", rejected, wantRejected)
	}
	if !hosts.Contains("GITLAB.internal") {
		t.Error("Contains should be case-insensitive")
	}
	if hosts.Contains("sub.gitlab.internal") || hosts.Contains("internal") {
		t.Error("Contains should match the exact name only")
	}
}

func TestCanonicalHost(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"gitlab.internal", "gitlab.internal", false},
		{"GitLab.Internal", "gitlab.internal", false},
		{"ｇｉｔｌａｂ.internal", "gitlab.internal", false},
		{"git\u00adlab.internal", "gitlab.internal", false},
		{"bücher.example", "xn--bcher-kva.example", false},
		{"gitlab.internal.", "gitlab.internal.", false},
		{"a\u200d.internal", "", true},
	}
	for _, c := range cases {
		got, err := CanonicalHost(c.in)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("CanonicalHost(%q) = %q, %v; want %q, error %v", c.in, got, err, c.want, c.wantErr)
		}
	}
}

func TestPrivateHostsContains_IDN(t *testing.T) {
	hosts := mustPrivateHosts(t, "gitlab.internal")
	for _, host := range []string{"ｇｉｔｌａｂ.internal", "git\u00adlab.internal", "GITLAB.internal"} {
		if !hosts.Contains(host) {
			t.Errorf("Contains(%q) = false, want true: it dials as gitlab.internal", host)
		}
	}
	for _, host := range []string{"gitlab.internal.", "a\u200d.internal"} {
		if hosts.Contains(host) {
			t.Errorf("Contains(%q) = true, want false", host)
		}
	}
}

func TestPrivateHosts_NilIsEmpty(t *testing.T) {
	var hosts *PrivateHosts
	if !hosts.Empty() || hosts.Contains("gitlab.internal") || hosts.Names() != nil {
		t.Error("a nil set should behave as the empty set")
	}
}

func TestPrivateHostsBlocks(t *testing.T) {
	hosts := mustPrivateHosts(t, "gitlab.internal")
	cases := []struct {
		name string
		host string
		ip   string
		want bool
	}{
		{"listed, 10/8", "gitlab.internal", "10.0.0.5", false},
		{"listed, 172.16/12", "gitlab.internal", "172.20.0.5", false},
		{"listed, 192.168/16", "gitlab.internal", "192.168.1.5", false},
		{"listed, unique local", "gitlab.internal", "fd00::5", false},
		{"listed, ipv4-mapped private", "gitlab.internal", "::ffff:10.0.0.5", false},
		{"listed, public", "gitlab.internal", "8.8.8.8", false},
		{"unlisted, public", "example.com", "8.8.8.8", false},

		{"unlisted, private", "other.internal", "10.0.0.5", true},

		{"listed, loopback", "gitlab.internal", "127.0.0.1", true},
		{"listed, loopback range", "gitlab.internal", "127.1.2.3", true},
		{"listed, loopback v6", "gitlab.internal", "::1", true},
		{"listed, ipv4-mapped loopback", "gitlab.internal", "::ffff:127.0.0.1", true},
		{"listed, link-local", "gitlab.internal", "169.254.0.1", true},
		{"listed, cloud metadata", "gitlab.internal", "169.254.169.254", true},
		{"listed, link-local v6", "gitlab.internal", "fe80::1", true},
		{"listed, ipv4-mapped metadata", "gitlab.internal", "::ffff:169.254.169.254", true},
		{"listed, unspecified", "gitlab.internal", "0.0.0.0", true},
		{"listed, multicast", "gitlab.internal", "224.0.0.1", true},
		{"listed, cgnat", "gitlab.internal", "100.64.0.1", true},

		{"listed, 6to4 loopback", "gitlab.internal", "2002:7f00:1::", true},
		{"listed, 6to4 metadata", "gitlab.internal", "2002:a9fe:a9fe::", true},
		{"listed, 6to4 private", "gitlab.internal", "2002:a00:5::", true},
		{"listed, nat64 metadata", "gitlab.internal", "64:ff9b::a9fe:a9fe", true},
		{"listed, nat64 private", "gitlab.internal", "64:ff9b::a00:5", true},
		{"listed, teredo client loopback", "gitlab.internal", "2001:0:808:808::80ff:fffe", true},
		{"listed, ipv4-compatible loopback", "gitlab.internal", "::127.0.0.1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ip := net.ParseIP(c.ip)
			if ip == nil {
				t.Fatalf("failed to parse test IP %q", c.ip)
			}
			if got := hosts.Blocks(c.host, ip); got != c.want {
				t.Errorf("Blocks(%q, %s) = %v, want %v", c.host, c.ip, got, c.want)
			}
		})
	}
}

// TestPrivateHostsBlocks_EmptySetMatchesGuard pins "empty means unchanged":
// with no hosts configured the decision is exactly IsPrivateOrInternalIP.
func TestPrivateHostsBlocks_EmptySetMatchesGuard(t *testing.T) {
	empty, _ := NewPrivateHosts(nil)
	for _, raw := range []string{"10.0.0.5", "172.20.0.5", "192.168.1.5", "fd00::5", "127.0.0.1", "169.254.169.254", "100.64.0.1", "8.8.8.8", "2001:4860:4860::8888"} {
		ip := net.ParseIP(raw)
		for _, hosts := range []*PrivateHosts{nil, empty} {
			if got, want := hosts.Blocks("gitlab.internal", ip), IsPrivateOrInternalIP(ip); got != want {
				t.Errorf("Blocks(%s) = %v, IsPrivateOrInternalIP = %v", raw, got, want)
			}
		}
	}
}

// fakeConn is what the fake dial function hands back; nothing reads from it.
type fakeConn struct{ net.Conn }

func (fakeConn) Close() error { return nil }

// recordingDialer returns a guardedDialer whose DNS answers come from answers,
// one per lookup (the last repeats), and which records each address dialed.
func recordingDialer(hosts *PrivateHosts, answers ...[]string) (guardedDialer, *[]string) {
	dialed := &[]string{}
	lookups := 0
	return guardedDialer{
		hosts: hosts,
		lookup: func(_ context.Context, _ string) ([]net.IPAddr, error) {
			answer := answers[min(lookups, len(answers)-1)]
			lookups++
			ips := make([]net.IPAddr, 0, len(answer))
			for _, raw := range answer {
				ips = append(ips, net.IPAddr{IP: net.ParseIP(raw)})
			}
			return ips, nil
		},
		dial: func(_ context.Context, _, addr string) (net.Conn, error) {
			*dialed = append(*dialed, addr)
			return fakeConn{}, nil
		},
	}, dialed
}

func TestGuardedDial_ListedHostPinsValidatedPrivateIP(t *testing.T) {
	d, dialed := recordingDialer(mustPrivateHosts(t, "gitlab.internal"), []string{"10.0.0.5"})

	conn, err := d.dialContext(allowCtx(t, "gitlab.internal"), "tcp", "gitlab.internal:443")
	if err != nil {
		t.Fatalf("expected dial to succeed, got: %v", err)
	}
	_ = conn.Close()

	if want := []string{"10.0.0.5:443"}; !reflect.DeepEqual(*dialed, want) {
		t.Errorf("dialed %v, want the checked IP %v rather than the hostname", *dialed, want)
	}
}

func TestGuardedDial_UnlistedHostStaysBlocked(t *testing.T) {
	d, dialed := recordingDialer(mustPrivateHosts(t, "gitlab.internal"), []string{"10.0.0.5"})

	_, err := d.dialContext(allowCtx(t, "gitlab.internal", "other.internal"), "tcp", "other.internal:443")
	if err == nil || !strings.Contains(err.Error(), "private/internal IP") {
		t.Fatalf("expected private/internal IP error, got: %v", err)
	}
	if len(*dialed) != 0 {
		t.Errorf("should not have dialed, got %v", *dialed)
	}
}

func TestGuardedDial_ListedHostSkipsForbiddenAnswers(t *testing.T) {
	d, dialed := recordingDialer(mustPrivateHosts(t, "gitlab.internal"), []string{"127.0.0.1", "169.254.169.254", "10.0.0.5"})

	conn, err := d.dialContext(allowCtx(t, "gitlab.internal"), "tcp", "gitlab.internal:443")
	if err != nil {
		t.Fatalf("expected dial to reach the private answer, got: %v", err)
	}
	_ = conn.Close()

	if want := []string{"10.0.0.5:443"}; !reflect.DeepEqual(*dialed, want) {
		t.Errorf("dialed %v, want only %v", *dialed, want)
	}
}

// TestGuardedDial_RebindingOfListedHost covers DNS changing after an earlier
// answer was accepted: the dial re-resolves and judges what it gets now.
func TestGuardedDial_RebindingOfListedHost(t *testing.T) {
	for _, rebound := range []string{"127.0.0.1", "169.254.169.254", "::1", "2002:7f00:1::"} {
		t.Run(rebound, func(t *testing.T) {
			d, dialed := recordingDialer(mustPrivateHosts(t, "gitlab.internal"), []string{"10.0.0.5"}, []string{rebound})

			conn, err := d.dialContext(allowCtx(t, "gitlab.internal"), "tcp", "gitlab.internal:443")
			if err != nil {
				t.Fatalf("first dial: %v", err)
			}
			_ = conn.Close()

			_, err = d.dialContext(allowCtx(t, "gitlab.internal"), "tcp", "gitlab.internal:443")
			if err == nil || !strings.Contains(err.Error(), "private/internal IP") {
				t.Fatalf("expected the rebound answer to be refused, got: %v", err)
			}
			if want := []string{"10.0.0.5:443"}; !reflect.DeepEqual(*dialed, want) {
				t.Errorf("dialed %v, want only %v", *dialed, want)
			}
		})
	}
}

// allowCtx is the context paca.fetch builds for a plugin whose manifest names
// the given hosts.
func allowCtx(t *testing.T, names ...string) context.Context {
	t.Helper()
	return WithAllowedPrivateHosts(context.Background(), mustPrivateHosts(t, names...))
}

// TestGuardedDial_RequiresPerRequestAllowance covers a "*" plugin whose
// pre-check saw a public answer while the dial resolves to a private one: the
// operator lists the host, but the request does not name it, so it is refused.
func TestGuardedDial_RequiresPerRequestAllowance(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
		want bool
	}{
		{"no allowance in context", context.Background(), false},
		{"wildcard plugin, nothing named", allowCtx(t), false},
		{"other host named", allowCtx(t, "other.internal"), false},
		{"host named", allowCtx(t, "gitlab.internal"), true},
		{"host named, other case", allowCtx(t, "GitLab.Internal"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, dialed := recordingDialer(mustPrivateHosts(t, "gitlab.internal"), []string{"10.0.0.5"})

			conn, err := d.dialContext(c.ctx, "tcp", "gitlab.internal:443")
			if c.want {
				if err != nil {
					t.Fatalf("expected dial to succeed, got: %v", err)
				}
				_ = conn.Close()
				return
			}
			if err == nil || !strings.Contains(err.Error(), "private/internal IP") {
				t.Fatalf("expected private/internal IP error, got: %v", err)
			}
			if len(*dialed) != 0 {
				t.Errorf("should not have dialed, got %v", *dialed)
			}
		})
	}
}

// TestGuardedDial_AllowanceDoesNotWidenOperatorSet: a request can only narrow
// the operator's list, never add to it.
func TestGuardedDial_AllowanceDoesNotWidenOperatorSet(t *testing.T) {
	for _, hosts := range []*PrivateHosts{nil, mustPrivateHosts(t, "gitlab.internal")} {
		d, dialed := recordingDialer(hosts, []string{"10.0.0.5"})
		_, err := d.dialContext(allowCtx(t, "other.internal"), "tcp", "other.internal:443")
		if err == nil || len(*dialed) != 0 {
			t.Fatalf("operator set %v: expected refusal without dialing, got err %v, dialed %v", hosts.Names(), err, *dialed)
		}
	}
}

func TestGuardedDial_NoExceptionByDefault(t *testing.T) {
	d, dialed := recordingDialer(nil, []string{"10.0.0.5"})

	_, err := d.dialContext(context.Background(), "tcp", "gitlab.internal:443")
	if err == nil || !strings.Contains(err.Error(), "private/internal IP") {
		t.Fatalf("expected private/internal IP error, got: %v", err)
	}
	if len(*dialed) != 0 {
		t.Errorf("should not have dialed, got %v", *dialed)
	}
}

func TestGuardedDial_LookupError(t *testing.T) {
	boom := errors.New("boom")
	d := guardedDialer{lookup: func(context.Context, string) ([]net.IPAddr, error) { return nil, boom }}

	if _, err := d.dialContext(context.Background(), "tcp", "gitlab.internal:443"); !errors.Is(err, boom) {
		t.Fatalf("expected lookup error, got: %v", err)
	}
}

// TestSharedTransportsIgnorePrivateHosts is the guarantee for marketplace.go,
// installer.go and every other netguard caller: only the constructor taking a
// set gets the exception.
func TestSharedTransportsIgnorePrivateHosts(t *testing.T) {
	_, err := NewSafeHTTPTransport().DialContext(context.Background(), "tcp", "10.0.0.5:443")
	if err == nil || !strings.Contains(err.Error(), "private/internal IP") {
		t.Fatalf("expected private/internal IP error, got: %v", err)
	}
}

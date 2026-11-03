package plugin

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Paca-AI/api/internal/platform/netguard"
)

// stubFetchDNS makes hostnames resolve from a table for the length of the test.
func stubFetchDNS(t *testing.T, table map[string][]string) {
	t.Helper()
	original := fetchLookupIPAddr
	t.Cleanup(func() { fetchLookupIPAddr = original })
	fetchLookupIPAddr = func(_ context.Context, host string) ([]net.IPAddr, error) {
		answer, ok := table[strings.ToLower(host)]
		if !ok {
			return nil, errors.New("no such host")
		}
		ips := make([]net.IPAddr, 0, len(answer))
		for _, raw := range answer {
			ips = append(ips, net.IPAddr{IP: net.ParseIP(raw)})
		}
		return ips, nil
	}
}

func privateHosts(t *testing.T, entries ...string) *netguard.PrivateHosts {
	t.Helper()
	hosts, rejected := netguard.NewPrivateHosts(entries)
	if len(rejected) != 0 {
		t.Fatalf("unexpected rejected entries: %v", rejected)
	}
	return hosts
}

func TestIsAllowedFetchDomain_PrivateHosts(t *testing.T) {
	stubFetchDNS(t, map[string][]string{
		"gitlab.internal":  {"10.0.0.5"},
		"gitlab6.internal": {"fd00::5"},
		"mixed.internal":   {"10.0.0.5", "127.0.0.1"},
		"loop.internal":    {"127.0.0.1"},
		"meta.internal":    {"169.254.169.254"},
		"embedded.example": {"2002:a9fe:a9fe::"},
		"cgnat.internal":   {"100.100.100.200"},
		"other.internal":   {"192.168.1.5"},
		"gitlab.com":       {"172.65.251.78"},
		// A resolver that answers for the raw IDN spelling with a public
		// address must not let the check pass while the transport dials
		// "gitlab.internal".
		"ｇｉｔｌａｂ.internal":       {"203.0.113.5"},
		"git\u00adlab.internal": {"203.0.113.5"},
	})

	listed := privateHosts(t, "gitlab.internal", "gitlab6.internal", "mixed.internal", "loop.internal", "meta.internal", "embedded.example", "cgnat.internal", "gitlab.com")
	manifest := []string{"gitlab.com", "GitLab.Internal", "gitlab6.internal", "mixed.internal", "loop.internal", "meta.internal", "embedded.example", "cgnat.internal", "other.internal"}

	cases := []struct {
		name     string
		url      string
		manifest []string
		hosts    *netguard.PrivateHosts
		want     bool
	}{
		{"listed, in manifest, private", "https://gitlab.internal/api/v4/user", manifest, listed, true},
		{"listed, port in url", "https://gitlab.internal:8443/api", manifest, listed, true},
		{"listed, url host differs in case", "https://GITLAB.internal/api", manifest, listed, true},
		{"listed, unique local", "https://gitlab6.internal/api", manifest, listed, true},
		{"listed, public address", "https://gitlab.com/api", manifest, listed, true},

		{"not listed, private", "https://other.internal/api", manifest, listed, false},
		{"listed, not in manifest", "https://gitlab.internal/api", []string{"gitlab.com"}, listed, false},
		{"listed, manifest has only the wildcard", "https://gitlab.internal/api", []string{"*"}, listed, false},
		{"listed, empty manifest", "https://gitlab.internal/api", nil, listed, false},
		{"listed, plain http", "http://gitlab.internal/api", manifest, listed, false},
		{"listed, resolves to loopback", "https://loop.internal/api", manifest, listed, false},
		{"listed, resolves to metadata", "https://meta.internal/api", manifest, listed, false},
		{"listed, resolves to embedded metadata", "https://embedded.example/api", manifest, listed, false},
		{"listed, resolves to cgnat", "https://cgnat.internal/api", manifest, listed, false},
		{"listed, one answer is loopback", "https://mixed.internal/api", manifest, listed, false},
		{"ip literal", "https://10.0.0.5/api", []string{"10.0.0.5"}, listed, false},

		{"idn, fullwidth form of listed host, named in manifest", "https://ｇｉｔｌａｂ.internal/api", manifest, listed, true},
		{"idn, manifest entry in fullwidth form", "https://gitlab.internal/api", []string{"ｇｉｔｌａｂ.internal"}, listed, true},
		{"idn, fullwidth form of listed host under wildcard", "https://ｇｉｔｌａｂ.internal/api", []string{"*"}, listed, false},
		{"idn, soft hyphen form of listed host under wildcard", "https://git\u00adlab.internal/api", []string{"*"}, listed, false},
		{"idn, soft hyphen form, not in manifest", "https://git\u00adlab.internal/api", []string{"gitlab.com"}, listed, false},
		{"idn, no ascii form", "https://a\u200d.internal/api", []string{"*"}, listed, false},

		{"empty list, private", "https://gitlab.internal/api", manifest, nil, false},
		{"empty set, private", "https://gitlab.internal/api", manifest, privateHosts(t), false},
		{"empty list, public host", "https://gitlab.com/api", manifest, nil, true},
		{"empty list, wildcard still reaches public", "https://gitlab.com/api", []string{"*"}, nil, true},
		// Resolving public now proves nothing for a listed host: the dial may
		// rebind, or reuse a pooled connection another plugin opened.
		{"wildcard refused for a listed host even when it resolves public", "https://gitlab.com/api", []string{"*"}, listed, false},
		{"wildcard reaches a public host that is not listed", "https://gitlab.com/api", []string{"*"}, privateHosts(t, "gitlab.internal"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isAllowedFetchDomain(context.Background(), c.url, c.manifest, c.hosts); got != c.want {
				t.Errorf("isAllowedFetchDomain(%q) = %v, want %v", c.url, got, c.want)
			}
		})
	}
}

// redirectingTransport answers every request with a redirect to the next
// location in the chain, then 200.
type redirectingTransport struct {
	chain    []string
	requests []string
}

func (rt *redirectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.requests = append(rt.requests, req.URL.String())
	resp := &http.Response{Request: req, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
	if len(rt.requests) > len(rt.chain) {
		resp.StatusCode = http.StatusOK
		return resp, nil
	}
	resp.StatusCode = http.StatusFound
	resp.Header.Set("Location", rt.chain[len(rt.requests)-1])
	return resp, nil
}

func fetchStart(t *testing.T, client *http.Client) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://public.example/start", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	return client.Do(req)
}

func TestPrivateHostRedirectGuard(t *testing.T) {
	listed := privateHosts(t, "gitlab.internal")
	cases := []struct {
		name     string
		location string
		manifest []string
		wantErr  bool
	}{
		{"listed host named in manifest", "https://gitlab.internal/next", []string{"public.example", "gitlab.internal"}, false},
		{"listed host not in manifest", "https://gitlab.internal/next", []string{"public.example"}, true},
		{"listed host under wildcard", "https://gitlab.internal/next", []string{"*"}, true},
		{"listed host, downgrade to http", "http://gitlab.internal/next", []string{"public.example", "gitlab.internal"}, true},
		{"unlisted host keeps existing behaviour", "https://elsewhere.example/next", []string{"public.example"}, false},

		// The transport dials the IDNA form of the Location host, so these
		// spellings of "gitlab.internal" must be judged as that name.
		{"idn fullwidth, https, wildcard", "https://ｇｉｔｌａｂ.internal/next", []string{"*"}, true},
		{"idn fullwidth, http, wildcard", "http://ｇｉｔｌａｂ.internal/next", []string{"*"}, true},
		{"idn soft hyphen, https, wildcard", "https://git\u00adlab.internal/next", []string{"*"}, true},
		{"idn soft hyphen, http, wildcard", "http://git\u00adlab.internal/next", []string{"*"}, true},
		{"idn fullwidth, http, named in manifest", "http://ｇｉｔｌａｂ.internal/next", []string{"public.example", "gitlab.internal"}, true},
		{"idn fullwidth, https, named in manifest", "https://ｇｉｔｌａｂ.internal/next", []string{"public.example", "gitlab.internal"}, false},
		{"idn with no ascii form", "https://a\u200d.internal/next", []string{"*"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			transport := &redirectingTransport{chain: []string{c.location}}
			client := withPrivateHostRedirectGuard(&http.Client{Transport: transport}, c.manifest, listed)

			resp, err := fetchStart(t, client)
			if err == nil {
				_ = resp.Body.Close()
			}
			if (err != nil) != c.wantErr {
				t.Fatalf("Get error = %v, wantErr %v", err, c.wantErr)
			}
			if c.wantErr && len(transport.requests) != 1 {
				t.Errorf("redirect target was requested: %v", transport.requests)
			}
		})
	}
}

func TestPrivateHostRedirectGuard_KeepsDefaultRedirectLimit(t *testing.T) {
	chain := make([]string, 20)
	for i := range chain {
		chain[i] = "https://public.example/next"
	}
	transport := &redirectingTransport{chain: chain}
	client := withPrivateHostRedirectGuard(&http.Client{Transport: transport}, []string{"public.example"}, privateHosts(t, "gitlab.internal"))

	resp, err := fetchStart(t, client)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected the redirect limit to apply")
	}
	if got := len(transport.requests); got != 10 {
		t.Errorf("made %d requests, want 10", got)
	}
}

func TestPrivateHostRedirectGuard_NoHostsLeavesClientUntouched(t *testing.T) {
	client := &http.Client{}
	if got := withPrivateHostRedirectGuard(client, nil, nil); got != client {
		t.Error("nil set should return the same client")
	}
	if got := withPrivateHostRedirectGuard(client, nil, privateHosts(t)); got != client {
		t.Error("empty set should return the same client")
	}
}

// TestPrivateHostRedirectGuard_IDNThroughRealTransport runs the redirect through
// net/http's own transport, which maps the Location host with IDNA before
// dialing. The stub dialer applies the guard's Blocks decision to the name it
// receives, with the listed host resolving to 10.0.0.5, so a guard that judged
// the raw spelling would let a wildcard plugin read the internal server.
func TestPrivateHostRedirectGuard_IDNThroughRealTransport(t *testing.T) {
	internalTLS := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "internal-secret")
	}))
	defer internalTLS.Close()
	internalPlain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "internal-secret")
	}))
	defer internalPlain.Close()

	listed := privateHosts(t, "gitlab.internal")
	for _, location := range []string{
		"https://ｇｉｔｌａｂ.internal/secret",
		"http://ｇｉｔｌａｂ.internal/secret",
		"https://git\u00adlab.internal/secret",
		"http://git\u00adlab.internal/secret",
		"https://gitlab.internal/secret",
		"https://GitLab.Internal/secret",
		"https://gitlab.internal./secret",
	} {
		t.Run(location, func(t *testing.T) {
			public := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, location, http.StatusFound)
			}))
			defer public.Close()

			client := netguard.NewSafeHTTPClientWithPrivateHosts(0, listed)
			transport := client.Transport.(*http.Transport)
			transport.Proxy = nil
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test servers use self-signed certificates
			var dialed []string
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				dialed = append(dialed, host)
				target := public.Listener.Addr().String()
				if host != "public.example" {
					if listed.Blocks(host, net.ParseIP("10.0.0.5")) {
						return nil, errors.New("blocked by stub dialer")
					}
					target = internalTLS.Listener.Addr().String()
					if port == "80" {
						target = internalPlain.Listener.Addr().String()
					}
				}
				return (&net.Dialer{}).DialContext(ctx, network, target)
			}

			resp, err := fetchStart(t, withPrivateHostRedirectGuard(client, []string{"*"}, listed))
			if err != nil {
				return
			}
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			if strings.Contains(string(body), "internal-secret") {
				t.Fatalf("wildcard plugin reached the listed private host via %q (dialed %v)", location, dialed)
			}
		})
	}
}

// TestManifestPrivateHosts pins what paca.fetch tells the dialer per request:
// the hosts the manifest names, in their dialed form, and nothing for "*".
func TestManifestPrivateHosts(t *testing.T) {
	cases := []struct {
		manifest []string
		want     bool
	}{
		{[]string{"*"}, false},
		{[]string{"public.example"}, false},
		{[]string{"10.0.0.5"}, false},
		{[]string{"*", "gitlab.internal"}, true},
		{[]string{" GitLab.Internal "}, true},
		{[]string{"ｇｉｔｌａｂ.internal"}, true},
	}
	for _, c := range cases {
		if got := manifestPrivateHosts(c.manifest).Contains("gitlab.internal"); got != c.want {
			t.Errorf("manifest %q: allows gitlab.internal = %v, want %v", c.manifest, got, c.want)
		}
	}
}

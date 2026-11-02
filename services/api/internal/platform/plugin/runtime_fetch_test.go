package plugin

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
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

		{"empty list, private", "https://gitlab.internal/api", manifest, nil, false},
		{"empty set, private", "https://gitlab.internal/api", manifest, privateHosts(t), false},
		{"empty list, public host", "https://gitlab.com/api", manifest, nil, true},
		{"empty list, wildcard still reaches public", "https://gitlab.com/api", []string{"*"}, nil, true},
		{"wildcard does not change for listed public host", "https://gitlab.com/api", []string{"*"}, listed, true},
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

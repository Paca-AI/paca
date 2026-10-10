package iam

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The web role editor mirrors PlatformRootFor in PLATFORM_ROOTS
// (apps/web/src/lib/policy/codec.ts): a domain missing there would be written
// onto the wrong resource and grant nothing. Fail when the two drift.
func TestPlatformRootForMatchesWebCodec(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "..", "apps", "web", "src", "lib", "policy", "codec.ts"))
	if err != nil {
		t.Skipf("web codec not available: %v", err)
	}
	block := regexp.MustCompile(`(?s)PLATFORM_ROOTS[^{]*\{(.*?)\n\};`).FindSubmatch(src)
	if block == nil {
		t.Fatal("PLATFORM_ROOTS not found in codec.ts")
	}
	entry := regexp.MustCompile(`"?([\w.]+)"?:\s*\[([^\]]*)\]`)
	web := map[string][]string{}
	for _, m := range entry.FindAllSubmatch(block[1], -1) {
		web[string(m[1])] = regexp.MustCompile(`"([^"]*)"`).FindAllString(string(m[2]), -1)
	}
	domains := []string{"users", "roles", "plugins", "settings", "settings.sso", "agents", "projects"}
	for _, d := range domains {
		root := PlatformRootFor(d + ":x")
		if root == "" {
			t.Errorf("%s has no platform root on the server", d)
			continue
		}
		if !strings.Contains(strings.Join(web[d], ","), `"`+root+`"`) {
			t.Errorf("domain %s: server root %q missing from web PLATFORM_ROOTS %v", d, root, web[d])
		}
		delete(web, d)
	}
	for d := range web {
		if PlatformRootFor(d+":x") == "" {
			t.Errorf("web PLATFORM_ROOTS lists %q, which the server has no root for", d)
		}
	}
}

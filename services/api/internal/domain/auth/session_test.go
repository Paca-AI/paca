package auth

import "testing"

func TestParseUserAgent(t *testing.T) {
	cases := []struct{ ua, browser, os string }{
		{"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", "Chrome", "Linux"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126.0 Safari/537.36 Edg/126.0", "Edge", "Windows"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/17.5 Safari/605.1.15", "Safari", "macOS"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 Version/17.5 Mobile/15E148 Safari/604.1", "Safari", "iOS"},
		{"Mozilla/5.0 (Android 14; Mobile; rv:128.0) Gecko/128.0 Firefox/128.0", "Firefox", "Android"},
		{"curl/8.5.0", "curl", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		b, o := ParseUserAgent(c.ua)
		if b != c.browser || o != c.os {
			t.Errorf("%q: got (%q, %q), want (%q, %q)", c.ua, b, o, c.browser, c.os)
		}
	}
}

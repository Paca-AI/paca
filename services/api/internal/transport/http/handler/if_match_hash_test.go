package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

const testHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestIfMatchHash(t *testing.T) {
	ok := map[string]string{
		`"` + testHash + `"`:                  testHash,
		`W/"` + testHash + `"`:                testHash,
		testHash:                              testHash,
		" " + testHash + " ":                  testHash,
		`"` + strings.ToUpper(testHash) + `"`: testHash,
		`*`:                                   "*",
	}
	for header, want := range ok {
		r := httptest.NewRequestWithContext(context.Background(), "PATCH", "/", nil)
		r.Header.Set("If-Match", header)
		got, err := ifMatchHash(r)
		if err != nil || got == nil || *got != want {
			t.Errorf("If-Match %q = %v, %v; want %q", header, got, err, want)
		}
	}
	if got, err := ifMatchHash(httptest.NewRequestWithContext(context.Background(), "PATCH", "/", nil)); got != nil || err != nil {
		t.Errorf("no header must mean no precondition, got %v %v", got, err)
	}
	for _, bad := range []string{
		"7", `"7"`, "abc", `"` + testHash[:63] + `"`, `"` + testHash + `0"`, `"` + testHash[:63] + `g"`,
		`"` + testHash + `", "` + testHash + `"`, `""`, `W/`, `"*"`,
	} {
		r := httptest.NewRequestWithContext(context.Background(), "PATCH", "/", nil)
		r.Header.Set("If-Match", bad)
		if _, err := ifMatchHash(r); err == nil {
			t.Errorf("If-Match %q must be refused, not ignored", bad)
		}
	}
}

func TestEtagMatches(t *testing.T) {
	other := strings.Repeat("f", 64)
	cases := []struct {
		header string
		want   bool
	}{
		{`"` + testHash + `"`, true},
		{`W/"` + testHash + `"`, true},
		{testHash, true},
		{`"` + strings.ToUpper(testHash) + `"`, true},
		{`*`, true},
		{`"` + other + `", "` + testHash + `"`, true},
		{`"` + other + `",W/"` + testHash + `"`, true},
		{`"` + other + `"`, false},
		{``, false},
		{`"` + testHash[:10] + `"`, false},
		{`, ,`, false},
	}
	for _, c := range cases {
		if got := etagMatches(c.header, testHash); got != c.want {
			t.Errorf("etagMatches(%q) = %v, want %v", c.header, got, c.want)
		}
	}
}

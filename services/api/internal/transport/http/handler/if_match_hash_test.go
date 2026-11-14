package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
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

const testMeta = "fedcba9876543210"

func TestIfMatchHash_AcceptsWholeETag(t *testing.T) {
	for _, header := range []string{
		`"` + testHash + "-" + testMeta + `"`,
		`W/"` + testHash + "-" + testMeta + `"`,
		testHash + "-" + strings.ToUpper(testMeta),
	} {
		r := httptest.NewRequestWithContext(context.Background(), "PATCH", "/", nil)
		r.Header.Set("If-Match", header)
		got, err := ifMatchHash(r)
		if err != nil || got == nil || *got != testHash {
			t.Errorf("If-Match %q = %v, %v; want the content hash alone", header, got, err)
		}
	}
	for _, bad := range []string{
		`"` + testHash + `-"`, `"` + testHash + `-abc"`, `"` + testHash + "-" + testMeta + `0"`, `"` + testHash + `-zzzzzzzzzzzzzzzz"`,
	} {
		r := httptest.NewRequestWithContext(context.Background(), "PATCH", "/", nil)
		r.Header.Set("If-Match", bad)
		if _, err := ifMatchHash(r); err == nil {
			t.Errorf("If-Match %q must be refused, not ignored", bad)
		}
	}
}

func TestEtagMatches(t *testing.T) {
	etag := `"` + testHash + "-" + testMeta + `"`
	whole := testHash + "-" + testMeta
	other := strings.Repeat("f", 64)
	otherMeta := strings.Repeat("0", 16)
	cases := []struct {
		header string
		want   bool
	}{
		{`"` + whole + `"`, true},
		{`W/"` + whole + `"`, true},
		{whole, true},
		{`"` + strings.ToUpper(whole) + `"`, true},
		{`*`, true},
		{`"` + other + `", "` + whole + `"`, true},
		{`"` + other + `",W/"` + whole + `"`, true},
		// The content hash alone is not the response: a rename keeps it.
		{`"` + testHash + `"`, false},
		{`"` + testHash + "-" + otherMeta + `"`, false},
		{`"` + other + "-" + testMeta + `"`, false},
		{`"` + other + `"`, false},
		{``, false},
		{`"` + testHash[:10] + `"`, false},
		{`, ,`, false},
	}
	for _, c := range cases {
		if got := etagMatches(c.header, etag); got != c.want {
			t.Errorf("etagMatches(%q) = %v, want %v", c.header, got, c.want)
		}
	}
	// A value that is not a whole document ETag never matches anything.
	if etagMatches(`*`, `"`+testHash+`"`) || etagMatches(`*`, ``) {
		t.Error("etagMatches must not match against a tag that is not a document ETag")
	}
}

func TestDocETag_CoversMetadata(t *testing.T) {
	h := testHash
	folder := uuid.New()
	base := docdom.Document{ID: uuid.New(), ProjectID: uuid.New(), Title: "Doc", ContentHash: &h, UpdatedAt: time.Unix(1700000000, 0).UTC()}
	first := docETag(&base)
	if !strings.HasPrefix(first, `"`+h+`-`) || len(first) != 64+1+metaTagLen+2 {
		t.Fatalf("docETag = %q, want \"<hash>-<%d hex>\"", first, metaTagLen)
	}
	if again := docETag(&base); again != first {
		t.Fatalf("docETag is not stable: %q then %q", first, again)
	}
	renamed, moved, reordered, touched := base, base, base, base
	renamed.Title = "Doc 2"
	moved.FolderID = &folder
	reordered.Position = 3
	touched.UpdatedAt = base.UpdatedAt.Add(time.Second)
	for name, d := range map[string]docdom.Document{"rename": renamed, "move": moved, "reorder": reordered, "updated_at": touched} {
		got := docETag(&d)
		if got == first {
			t.Errorf("%s: ETag did not change", name)
		}
		if !strings.HasPrefix(got, `"`+h+`-`) {
			t.Errorf("%s: content hash part changed: %q", name, got)
		}
	}
}

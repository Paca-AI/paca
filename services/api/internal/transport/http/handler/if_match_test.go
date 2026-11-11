package handler

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestIfMatchVersion(t *testing.T) {
	for header, want := range map[string]int64{`7`: 7, `"7"`: 7, `W/"7"`: 7, ` 12 `: 12, `0`: 0} {
		r := httptest.NewRequestWithContext(context.Background(), "PATCH", "/", nil)
		r.Header.Set("If-Match", header)
		got, err := ifMatchVersion(r)
		if err != nil || got == nil || *got != want {
			t.Errorf("If-Match %q = %v, %v; want %d", header, got, err, want)
		}
	}
	if got, err := ifMatchVersion(httptest.NewRequestWithContext(context.Background(), "PATCH", "/", nil)); got != nil || err != nil {
		t.Errorf("no header must mean no precondition, got %v %v", got, err)
	}
	for _, bad := range []string{"abc", `"*"`, "-1", "1.5"} {
		r := httptest.NewRequestWithContext(context.Background(), "PATCH", "/", nil)
		r.Header.Set("If-Match", bad)
		if _, err := ifMatchVersion(r); err == nil {
			t.Errorf("If-Match %q must be refused, not ignored", bad)
		}
	}
}

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFixedWindowLimiter(t *testing.T) {
	l := &fixedWindowLimiter{limit: 2, window: time.Minute, counts: map[string]*windowCount{}}
	now := time.Now()
	for i := 0; i < 2; i++ {
		if ok, _ := l.allow("a", now); !ok {
			t.Fatalf("request %d rejected", i+1)
		}
	}
	if ok, retry := l.allow("a", now); ok || retry <= 0 {
		t.Fatalf("third request allowed=%v retry=%v, want rejected with retry", ok, retry)
	}
	if ok, _ := l.allow("b", now); !ok {
		t.Fatal("other client rejected")
	}
	if ok, _ := l.allow("a", now.Add(time.Minute)); !ok {
		t.Fatal("request after window rejected")
	}
}

func TestRateLimitResponds429(t *testing.T) {
	h := RateLimit(1, time.Minute, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	codes := []int{}
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
		codes = append(codes, rec.Code)
	}
	if codes[0] != http.StatusNoContent || codes[1] != http.StatusTooManyRequests {
		t.Fatalf("codes = %v, want [204 429]", codes)
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.5:1234"
	if got := ClientIP(r); got != "10.0.0.5" {
		t.Fatalf("no XFF: got %q", got)
	}
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")
	if got := ClientIP(r); got != "203.0.113.9" {
		t.Fatalf("XFF: got %q, want rightmost entry", got)
	}
}

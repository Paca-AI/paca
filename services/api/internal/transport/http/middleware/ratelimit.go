package middleware

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Paca-AI/api/internal/apierr"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// RateLimit allows each client at most limit requests per window and answers
// the rest with 429. Counts are kept in memory, per API instance: this is an
// abuse brake for unauthenticated endpoints that do work on every call (e.g.
// SSO login, which writes a sign-in attempt to Redis), not a precise quota.
// A rejected request gets a JSON 429, or onLimited when it is non-nil (for
// browser navigations, where a JSON body would be a dead end).
func RateLimit(limit int, window time.Duration, onLimited http.Handler) func(http.Handler) http.Handler {
	l := &fixedWindowLimiter{limit: limit, window: window, counts: map[string]*windowCount{}}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, retryAfter := l.allow(ClientIP(r), time.Now())
			if !ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
				if onLimited != nil {
					onLimited.ServeHTTP(w, r)
					return
				}
				presenter.Error(w, r, apierr.New(apierr.CodeTooManyRequests, "too many requests"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP returns the address of the client behind the reverse proxy: the
// rightmost X-Forwarded-For entry — the one the proxy nearest to the API
// appended from its own connection, so a client cannot choose it — else the
// connection's remote address.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

type windowCount struct {
	start time.Time
	n     int
}

type fixedWindowLimiter struct {
	limit  int
	window time.Duration

	mu        sync.Mutex
	counts    map[string]*windowCount
	lastSweep time.Time
}

func (l *fixedWindowLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Drop expired windows now and then so the map cannot grow without
	// bound under a spray of distinct clients.
	if now.Sub(l.lastSweep) >= l.window {
		for k, c := range l.counts {
			if now.Sub(c.start) >= l.window {
				delete(l.counts, k)
			}
		}
		l.lastSweep = now
	}
	c := l.counts[key]
	if c == nil || now.Sub(c.start) >= l.window {
		l.counts[key] = &windowCount{start: now, n: 1}
		return true, 0
	}
	if c.n >= l.limit {
		return false, c.start.Add(l.window).Sub(now)
	}
	c.n++
	return true, 0
}

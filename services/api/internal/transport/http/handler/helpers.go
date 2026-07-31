package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Paca-AI/api/internal/apierr"
)

// defaultQuery returns the URL query parameter named key, or fallback if missing/empty.
func defaultQuery(r *http.Request, key, fallback string) string {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	return v
}

// parsePageSize reads the page_size query parameter, capped at maxSize. An
// absent param silently falls back to defaultSize — only an explicitly
// supplied, invalid value is rejected. This intentionally differs from
// silently substituting a default: a caller driving pagination off the
// page_size it requested (e.g. computing the next offset as N * page_size)
// would otherwise get a smaller page back with no signal, skipping or
// duplicating rows against its own math.
func parsePageSize(r *http.Request, defaultSize, maxSize int) (int, error) {
	raw := r.URL.Query().Get("page_size")
	if raw == "" {
		return defaultSize, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > maxSize {
		return 0, apierr.New(apierr.CodeBadRequest, fmt.Sprintf("page_size must be an integer between 1 and %d", maxSize))
	}
	return n, nil
}

// parsePage reads the page query parameter (1-indexed). An absent param
// silently falls back to 1 — only an explicitly supplied, invalid value
// (non-numeric or less than 1) is rejected. See parsePageSize for why.
func parsePage(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("page")
	if raw == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, apierr.New(apierr.CodeBadRequest, "page must be a positive integer")
	}
	return n, nil
}

// parseOffsetLimit reads the offset and limit query parameters. Absent
// params silently fall back to their defaults (0 and 50); only an explicitly
// supplied, invalid value is rejected — for the same reason parsePageSize
// rejects one: a caller advancing offset by the limit it requested (e.g.
// offset += limit) would otherwise get a different limit back with no
// signal, skipping or duplicating rows against its own math.
func parseOffsetLimit(r *http.Request) (offset, limit int, err error) {
	rawOffset := r.URL.Query().Get("offset")
	if rawOffset == "" {
		offset = 0
	} else if offset, err = strconv.Atoi(rawOffset); err != nil || offset < 0 {
		return 0, 0, apierr.New(apierr.CodeBadRequest, "offset must be a non-negative integer")
	}

	rawLimit := r.URL.Query().Get("limit")
	if rawLimit == "" {
		limit = 50
	} else if limit, err = strconv.Atoi(rawLimit); err != nil || limit < 1 || limit > 200 {
		return 0, 0, apierr.New(apierr.CodeBadRequest, "limit must be an integer between 1 and 200")
	}

	return offset, limit, nil
}

// splitCommaList splits a comma-separated query param into its trimmed,
// non-empty parts (e.g. "running, paused" -> ["running", "paused"]).
func splitCommaList(raw string) []string {
	var out []string
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

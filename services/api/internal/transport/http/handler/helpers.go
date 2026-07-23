package handler

import (
	"fmt"
	"net/http"
	"strconv"

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

// parseOffsetLimit reads offset and limit query parameters with sensible defaults.
func parseOffsetLimit(r *http.Request) (offset, limit int) {
	offset, _ = strconv.Atoi(defaultQuery(r, "offset", "0"))
	limit, _ = strconv.Atoi(defaultQuery(r, "limit", "50"))
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	return offset, limit
}

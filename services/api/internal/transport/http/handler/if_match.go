package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Paca-AI/api/internal/apierr"
)

// ifMatchVersion reads the row version a write names in its If-Match header
// (a bare number, optionally quoted or weak: 7, "7", W/"7"). nil when the
// header is absent, so a write without it behaves as it always did. A header
// that is not a version is a bad request, not silently ignored: the caller
// asked for a precondition and would otherwise believe it held.
func ifMatchVersion(r *http.Request) (*int64, error) {
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" {
		return nil, nil
	}
	raw = strings.TrimPrefix(raw, "W/")
	raw = strings.Trim(raw, `"`)
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return nil, apierr.New(apierr.CodeBadRequest, "If-Match must be a row version, e.g. \"7\"")
	}
	return &v, nil
}

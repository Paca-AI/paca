package handler

import (
	"net/http"
	"strings"

	"github.com/Paca-AI/api/internal/apierr"
)

// ifMatchHash reads the document content hash a write names in its If-Match
// header: "<hex>", W/"<hex>" or bare <hex> (64 hex characters, returned
// lowercase), or * (any version, as long as the document exists). nil when the
// header is absent, so a write without it behaves as it always did. Anything
// else is a bad request, not silently ignored: the caller asked for a
// precondition and would otherwise believe it held. Documents use a content
// hash, not the row version ifMatchVersion reads for tasks and sprints.
func ifMatchHash(r *http.Request) (*string, error) {
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" {
		return nil, nil
	}
	if raw == "*" {
		star := "*"
		return &star, nil
	}
	h, ok := parseEntityTag(raw)
	if !ok {
		return nil, apierr.New(apierr.CodeBadRequest, `If-Match must be a document content hash, e.g. "<64 hex characters>", or *`)
	}
	return &h, nil
}

// etagMatches reports whether an If-None-Match header value names hash: a
// comma-separated list of entity tags (weak ones compare like strong ones) or
// *. A malformed tag in the list matches nothing.
func etagMatches(headerValue, hash string) bool {
	for _, part := range strings.Split(headerValue, ",") {
		part = strings.TrimSpace(part)
		if part == "*" {
			return true
		}
		if h, ok := parseEntityTag(part); ok && h == hash {
			return true
		}
	}
	return false
}

// docETag is the ETag header value for a content hash.
func docETag(hash string) string { return `"` + hash + `"` }

// parseEntityTag returns the lowercase content hash an entity tag carries,
// accepting "<hex>", W/"<hex>" and bare <hex>.
func parseEntityTag(tag string) (string, bool) {
	tag = strings.TrimPrefix(tag, "W/")
	if len(tag) >= 2 && tag[0] == '"' && tag[len(tag)-1] == '"' {
		tag = tag[1 : len(tag)-1]
	}
	return normalizeContentHash(tag)
}

// normalizeContentHash returns s lowercased when it is 64 hex characters.
func normalizeContentHash(s string) (string, bool) {
	if len(s) != 64 {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return "", false
		}
	}
	return strings.ToLower(s), true
}

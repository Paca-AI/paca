package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Paca-AI/api/internal/apierr"
	docdom "github.com/Paca-AI/api/internal/domain/doc"
	"github.com/Paca-AI/api/internal/transport/http/dto"
)

// Documents carry two related tokens.
//
// The content hash (content_hash in the body) identifies the content alone.
// It is the If-Match precondition on writes: a rename or a move is not a
// content change, so it causes no conflict.
//
// The ETag identifies the whole response: "<content hash>-<meta tag>", where
// the meta tag covers every other field of the document response (title,
// folder, position, timestamps, ...). If-None-Match compares the whole ETag,
// so a 304 never hides a rename or a move. If-Match accepts either form and
// reads only the content hash from it.
//
// A proxy that compresses the response appends the content coding to a strong
// ETag: the bundled Caddy gateway (encode gzip) turns "<hash>-<meta>" into
// "<hash>-<meta>-gzip". Clients send that tag back unchanged, so both headers
// accept one such suffix after the meta tag and ignore it: the suffixed and
// the unsuffixed tag name the same document version.

// metaTagLen is the length, in hex characters, of an ETag's meta tag.
const metaTagLen = 16

// etagCodingSuffixes are the content codings a compressing proxy may append to
// a document ETag ("<hash>-<meta>-<coding>"), lowercase.
var etagCodingSuffixes = map[string]bool{"gzip": true, "br": true, "zstd": true, "deflate": true}

// ifMatchHash reads the document content hash a write names in its If-Match
// header: a content hash or a whole document ETag, quoted, weak (W/"...") or
// bare, or * (any version, as long as the document exists). A whole ETag may
// end in a content-coding suffix (-gzip, -br, -zstd, -deflate), which a proxy
// that compresses the response, such as the bundled Caddy gateway, appends;
// it is ignored. Only the content hash is returned (lowercase); an ETag's meta
// tag is ignored, because the precondition is about content. nil when the
// header is absent, so a write without it behaves as it always did. Anything
// else is a bad request, not
// silently ignored: the caller asked for a precondition and would otherwise
// believe it held. Documents use a content hash, not the row version
// ifMatchVersion reads for tasks and sprints.
func ifMatchHash(r *http.Request) (*string, error) {
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" {
		return nil, nil
	}
	if raw == "*" {
		star := "*"
		return &star, nil
	}
	h, _, ok := parseEntityTag(raw)
	if !ok {
		return nil, apierr.New(apierr.CodeBadRequest, `If-Match must be a document content hash or ETag, e.g. "<64 hex characters>", or *`)
	}
	return &h, nil
}

// etagMatches reports whether an If-None-Match header value names etag, a
// value docETag returned: a comma-separated list of entity tags (weak ones
// compare like strong ones) or *. Only a whole ETag matches. A bare content
// hash does not, because the response also carries fields the content hash
// does not cover. A content-coding suffix (see parseEntityTag) is ignored on
// both sides, so the tag a compressing proxy returned matches the document it
// came from. A malformed tag in the list matches nothing.
func etagMatches(headerValue, etag string) bool {
	_, want, ok := parseEntityTag(etag)
	if !ok || !strings.Contains(want, "-") {
		return false
	}
	for _, part := range strings.Split(headerValue, ",") {
		part = strings.TrimSpace(part)
		if part == "*" {
			return true
		}
		if _, got, ok := parseEntityTag(part); ok && got == want {
			return true
		}
	}
	return false
}

// docETag is the ETag header value for a document response: the content hash,
// then a tag over the rest of the response, so it changes whenever anything
// the client would receive changes.
func docETag(d *docdom.Document) string {
	return `"` + d.Hash() + "-" + docMetaTag(d) + `"`
}

// docMetaTag fingerprints every field of the document response except the
// content and its hash. It is built from the response DTO, so a field added to
// the response is covered without a change here.
func docMetaTag(d *docdom.Document) string {
	resp := dto.DocumentFromEntity(d)
	resp.Content = nil
	resp.ContentHash = ""
	// The same instant must tag the same way whatever zone or precision the
	// value was read with (a fresh time.Now versus a row read back).
	resp.CreatedAt = resp.CreatedAt.UTC().Truncate(time.Microsecond)
	resp.UpdatedAt = resp.UpdatedAt.UTC().Truncate(time.Microsecond)
	raw, err := json.Marshal(resp)
	if err != nil {
		// Not reachable for this struct; an empty tag still yields a valid ETag
		// that simply never matches a real one.
		return strings.Repeat("0", metaTagLen)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:metaTagLen]
}

// parseEntityTag reads an entity tag that is a content hash or a document
// ETag, accepting "<tag>", W/"<tag>" and a bare <tag>. It returns the content
// hash and the whole tag, both lowercase.
//
// A document ETag may carry one content-coding suffix after its meta tag,
// "<hash>-<meta>-<coding>" with coding one of etagCodingSuffixes (any case):
// a proxy that compresses the response, such as the bundled Caddy gateway,
// appends it, and the client sends it back as it got it. The suffix is
// dropped, so whole is "<hash>-<meta>" either way. It is only valid after a
// meta tag; an empty, unknown or repeated suffix makes the tag invalid.
func parseEntityTag(tag string) (contentHash, whole string, ok bool) {
	tag = strings.TrimPrefix(tag, "W/")
	if len(tag) >= 2 && tag[0] == '"' && tag[len(tag)-1] == '"' {
		tag = tag[1 : len(tag)-1]
	}
	hashPart, metaPart, hasMeta := strings.Cut(tag, "-")
	contentHash, ok = normalizeContentHash(hashPart)
	if !ok {
		return "", "", false
	}
	if !hasMeta {
		return contentHash, contentHash, true
	}
	if meta, coding, hasCoding := strings.Cut(metaPart, "-"); hasCoding {
		if !etagCodingSuffixes[strings.ToLower(coding)] {
			return "", "", false
		}
		metaPart = meta
	}
	if len(metaPart) != metaTagLen || !isHex(metaPart) {
		return "", "", false
	}
	return contentHash, contentHash + "-" + strings.ToLower(metaPart), true
}

// normalizeContentHash returns s lowercased when it is 64 hex characters.
func normalizeContentHash(s string) (string, bool) {
	if len(s) != 64 || !isHex(s) {
		return "", false
	}
	return strings.ToLower(s), true
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

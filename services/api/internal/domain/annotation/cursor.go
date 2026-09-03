package annotationdom

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// AnnotationCursor holds the stable ordering fields for keyset-based
// pagination over SearchInProject, ordered by created_at DESC, id DESC
// (newest first, matching Repository.ListForPortForward's own convention)
// — mirrors docdom.DocumentCursor exactly, just keyed on CreatedAt instead
// of Title and sorted the opposite direction.
type AnnotationCursor struct {
	CreatedAt time.Time `json:"ca"`
	ID        string    `json:"id"`
}

// EncodeAnnotationCursor builds an opaque base64 cursor from the last
// annotation on a page.
func EncodeAnnotationCursor(a *PageAnnotation) string {
	cur := AnnotationCursor{CreatedAt: a.CreatedAt, ID: a.ID.String()}
	b, _ := json.Marshal(cur)
	return base64.URLEncoding.EncodeToString(b)
}

// DecodeAnnotationCursor parses a cursor token produced by
// EncodeAnnotationCursor. A malformed token returns ok=false rather than an
// error — the repository treats that as "no cursor" and restarts from the
// first page instead of hard-failing the request, the same permissive-decode
// convention docdom.DecodeDocumentCursor uses.
func DecodeAnnotationCursor(s string) (cur AnnotationCursor, ok bool) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return AnnotationCursor{}, false
	}
	if err := json.Unmarshal(b, &cur); err != nil {
		return AnnotationCursor{}, false
	}
	return cur, true
}

package docdom

import (
	"encoding/base64"
	"encoding/json"
)

// DocumentCursor holds the stable ordering fields for keyset-based
// pagination over a searchable/paginated document listing (see
// Repository.ListDocuments), ordered by title ASC, id ASC.
type DocumentCursor struct {
	Title string `json:"t"`
	ID    string `json:"id"`
}

// EncodeDocumentCursor builds an opaque base64 cursor from the last document
// on a page.
func EncodeDocumentCursor(d *Document) string {
	cur := DocumentCursor{Title: d.Title, ID: d.ID.String()}
	b, _ := json.Marshal(cur)
	return base64.URLEncoding.EncodeToString(b)
}

// DecodeDocumentCursor parses a cursor token produced by
// EncodeDocumentCursor. A malformed token (never produced by this codebase's
// own frontend, which only ever round-trips an opaque cursor it was handed)
// returns ok=false rather than an error — the repository treats that as "no
// cursor" and restarts from the first page instead of hard-failing the
// request, the same permissive-decode convention used elsewhere for opaque
// pagination tokens.
func DecodeDocumentCursor(s string) (cur DocumentCursor, ok bool) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return DocumentCursor{}, false
	}
	if err := json.Unmarshal(b, &cur); err != nil {
		return DocumentCursor{}, false
	}
	return cur, true
}

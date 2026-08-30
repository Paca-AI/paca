package automationdom

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// AutomationCursor holds the stable ordering fields for keyset-based
// pagination over a searchable/paginated automation listing (see
// Repository.ListAutomations), ordered by created_at DESC, id DESC.
type AutomationCursor struct {
	CreatedAt time.Time `json:"ca"`
	ID        string    `json:"id"`
}

// EncodeAutomationCursor builds an opaque base64 cursor from the last
// automation on a page.
func EncodeAutomationCursor(a *Automation) string {
	cur := AutomationCursor{CreatedAt: a.CreatedAt.UTC(), ID: a.ID.String()}
	b, _ := json.Marshal(cur)
	return base64.URLEncoding.EncodeToString(b)
}

// DecodeAutomationCursor parses a cursor token produced by
// EncodeAutomationCursor. A malformed token returns ok=false rather than an
// error — see docdom.DecodeDocumentCursor's doc comment for why this is
// permissive rather than a hard failure.
func DecodeAutomationCursor(s string) (cur AutomationCursor, ok bool) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return AutomationCursor{}, false
	}
	if err := json.Unmarshal(b, &cur); err != nil {
		return AutomationCursor{}, false
	}
	cur.CreatedAt = cur.CreatedAt.UTC()
	return cur, true
}

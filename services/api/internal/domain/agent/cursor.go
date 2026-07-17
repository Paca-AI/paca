package agentdom

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// ConversationCursor holds the stable ordering fields for keyset-based
// pagination over the conversations list, which is always ordered by
// created_at DESC, id DESC.
type ConversationCursor struct {
	CreatedAt time.Time `json:"ca"`
	ID        string    `json:"id"`
}

// EncodeConversationCursor builds an opaque base64 cursor from the last
// conversation on a page.
func EncodeConversationCursor(c *AgentConversation) string {
	cur := ConversationCursor{CreatedAt: c.CreatedAt.UTC(), ID: c.ID.String()}
	b, _ := json.Marshal(cur)
	return base64.URLEncoding.EncodeToString(b)
}

// DecodeConversationCursor parses a cursor token produced by EncodeConversationCursor.
func DecodeConversationCursor(s string) (*ConversationCursor, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode cursor base64: %w", err)
	}
	var c ConversationCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("decode cursor json: %w", err)
	}
	c.CreatedAt = c.CreatedAt.UTC()
	return &c, nil
}

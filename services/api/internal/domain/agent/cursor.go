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

// ActivityFeedCursor holds the stable ordering fields for keyset-based
// pagination over an agent's activity feed, which is always ordered by
// created_at DESC, id DESC.
type ActivityFeedCursor struct {
	CreatedAt time.Time `json:"ca"`
	ID        string    `json:"id"`
}

// EncodeActivityFeedCursor builds an opaque base64 cursor from the last
// activity item on a page.
func EncodeActivityFeedCursor(item *ActivityFeedItem) string {
	cur := ActivityFeedCursor{CreatedAt: item.CreatedAt.UTC(), ID: item.ID.String()}
	b, _ := json.Marshal(cur)
	return base64.URLEncoding.EncodeToString(b)
}

// DecodeActivityFeedCursor parses a cursor token produced by EncodeActivityFeedCursor.
func DecodeActivityFeedCursor(s string) (*ActivityFeedCursor, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode cursor base64: %w", err)
	}
	var c ActivityFeedCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("decode cursor json: %w", err)
	}
	c.CreatedAt = c.CreatedAt.UTC()
	return &c, nil
}

package userdom

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Cursor identifies the last user on a page of the name-sorted user list.
// Only the ID is stored: the repository re-derives the sort keys from that
// row, so the cursor stays valid even if the keys were computed differently
// in Go and SQL, and stays opaque to clients.
type Cursor struct {
	ID string `json:"id"`
}

// EncodeCursor builds an opaque base64 cursor from the last user on a page.
func EncodeCursor(u *User) string {
	b, _ := json.Marshal(Cursor{ID: u.ID.String()})
	return base64.URLEncoding.EncodeToString(b)
}

// DecodeCursor parses a token produced by EncodeCursor. Any failure wraps
// ErrInvalidCursor.
func DecodeCursor(s string) (*Cursor, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: base64: %v", ErrInvalidCursor, err)
	}
	var c Cursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%w: json: %v", ErrInvalidCursor, err)
	}
	if _, err := uuid.Parse(c.ID); err != nil {
		return nil, fmt.Errorf("%w: id: %v", ErrInvalidCursor, err)
	}
	return &c, nil
}

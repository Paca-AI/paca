package userdom_test

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/google/uuid"

	userdom "github.com/Paca-AI/api/internal/domain/user"
)

func TestCursorRoundTrip(t *testing.T) {
	u := &userdom.User{ID: uuid.New()}
	c, err := userdom.DecodeCursor(userdom.EncodeCursor(u))
	if err != nil || c.ID != u.ID.String() {
		t.Fatalf("got %+v, %v", c, err)
	}
}

func TestDecodeCursorInvalid(t *testing.T) {
	for _, in := range []string{"!!!", base64.URLEncoding.EncodeToString([]byte("nope")), base64.URLEncoding.EncodeToString([]byte(`{"id":"x"}`))} {
		if _, err := userdom.DecodeCursor(in); !errors.Is(err, userdom.ErrInvalidCursor) {
			t.Errorf("%q: err = %v, want ErrInvalidCursor", in, err)
		}
	}
}

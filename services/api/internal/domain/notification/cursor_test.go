package notificationdom

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNotificationCursor_RoundTrip(t *testing.T) {
	id := uuid.New()
	createdAt := time.Date(2026, 3, 15, 10, 30, 0, 123456000, time.UTC)
	n := &Notification{ID: id, CreatedAt: createdAt}

	token := EncodeNotificationCursor(n)
	if token == "" {
		t.Fatal("expected non-empty cursor token")
	}

	cur, err := DecodeNotificationCursor(token)
	if err != nil {
		t.Fatalf("decode: unexpected error: %v", err)
	}
	if cur.ID != id.String() {
		t.Errorf("expected ID %q, got %q", id.String(), cur.ID)
	}
	if !cur.CreatedAt.Equal(createdAt) {
		t.Errorf("expected CreatedAt %v, got %v", createdAt, cur.CreatedAt)
	}
}

func TestEncodeNotificationCursor_NormalizesToUTC(t *testing.T) {
	loc := time.FixedZone("UTC+7", 7*60*60)
	localTime := time.Date(2026, 3, 15, 17, 30, 0, 0, loc)
	n := &Notification{ID: uuid.New(), CreatedAt: localTime}

	token := EncodeNotificationCursor(n)
	cur, err := DecodeNotificationCursor(token)
	if err != nil {
		t.Fatalf("decode: unexpected error: %v", err)
	}

	if cur.CreatedAt.Location() != time.UTC {
		t.Errorf("expected cursor CreatedAt in UTC, got location %v", cur.CreatedAt.Location())
	}
	if !cur.CreatedAt.Equal(localTime) {
		t.Errorf("expected same instant as %v, got %v", localTime, cur.CreatedAt)
	}
}

func TestDecodeNotificationCursor_InvalidBase64(t *testing.T) {
	_, err := DecodeNotificationCursor("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64 cursor, got nil")
	}
}

func TestDecodeNotificationCursor_InvalidJSON(t *testing.T) {
	// Valid URL-safe base64, but the decoded bytes are not valid JSON.
	_, err := DecodeNotificationCursor("bm90LWpzb24tcGF5bG9hZA==")
	if err == nil {
		t.Fatal("expected error for cursor whose payload isn't valid JSON, got nil")
	}
}

func TestDecodeNotificationCursor_EmptyString(t *testing.T) {
	// Empty input decodes to zero base64 bytes, which is not valid JSON — the
	// handler only calls Decode when the cursor query param is non-empty, but
	// the function itself must still fail closed rather than panic or return
	// a misleadingly "valid" zero cursor.
	_, err := DecodeNotificationCursor("")
	if err == nil {
		t.Fatal("expected error for empty cursor string, got nil")
	}
}

func TestDecodeNotificationCursor_NormalizesResultToUTC(t *testing.T) {
	// A cursor whose embedded timestamp carries a non-UTC offset should still
	// come back normalized to UTC, regardless of how it was produced.
	cur, err := DecodeNotificationCursor("eyJjYSI6ICIyMDI2LTAzLTE1VDE3OjMwOjAwKzA3OjAwIiwgImlkIjogImFiYyJ9")
	if err != nil {
		t.Fatalf("decode: unexpected error: %v", err)
	}
	if cur.CreatedAt.Location() != time.UTC {
		t.Errorf("expected UTC location, got %v", cur.CreatedAt.Location())
	}
}

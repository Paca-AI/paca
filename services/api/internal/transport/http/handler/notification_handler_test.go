package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
)

type mockNotificationSvc struct {
	mu sync.RWMutex

	listNotifications func(ctx context.Context, userID uuid.UUID, limit int, cursorAfter *string) ([]*notificationdom.Notification, bool, error)
	unreadCount       func(ctx context.Context, userID uuid.UUID) (int64, error)
	markAsRead        func(ctx context.Context, id, userID uuid.UUID) error
	markAllAsRead     func(ctx context.Context, userID uuid.UUID) error
}

func (m *mockNotificationSvc) NotifyAssigned(_ context.Context, _ notificationdom.NotifyAssignedInput) error {
	return nil
}

func (m *mockNotificationSvc) NotifyMentioned(_ context.Context, _ notificationdom.NotifyMentionedInput) error {
	return nil
}

func (m *mockNotificationSvc) ListNotifications(ctx context.Context, userID uuid.UUID, limit int, cursorAfter *string) ([]*notificationdom.Notification, bool, error) {
	if m.listNotifications != nil {
		return m.listNotifications(ctx, userID, limit, cursorAfter)
	}
	return nil, false, nil
}

func (m *mockNotificationSvc) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	if m.unreadCount != nil {
		return m.unreadCount(ctx, userID)
	}
	return 0, nil
}

func (m *mockNotificationSvc) MarkAsRead(ctx context.Context, id, userID uuid.UUID) error {
	if m.markAsRead != nil {
		return m.markAsRead(ctx, id, userID)
	}
	return nil
}

func (m *mockNotificationSvc) MarkAllAsRead(ctx context.Context, userID uuid.UUID) error {
	if m.markAllAsRead != nil {
		return m.markAllAsRead(ctx, userID)
	}
	return nil
}

var _ notificationdom.Service = (*mockNotificationSvc)(nil)

func buildNotificationRouter(svc *mockNotificationSvc) chi.Router {
	h := handler.NewNotificationHandler(svc)
	r := chi.NewRouter()
	r.Route("/users", func(r chi.Router) {
		r.Get("/me/notifications", h.List)
		r.Patch("/me/notifications/{notificationId}/read", h.MarkAsRead)
		r.Post("/me/notifications/read-all", h.MarkAllAsRead)
	})
	return r
}

func doNotifRequest(r chi.Router, method, path string, body any) *httptest.ResponseRecorder {
	var buf *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doNotifRequestWithActor(r chi.Router, method, path string, body any, actorID uuid.UUID) *httptest.ResponseRecorder {
	var buf *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequestWithContext(middleware.WithActorID(context.Background(), actorID), method, path, buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeNotificationEnvelope(t *testing.T, body []byte) (bool, string, json.RawMessage) {
	t.Helper()
	var env struct {
		Success   bool            `json:"success"`
		ErrorCode string          `json:"error_code"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return env.Success, env.ErrorCode, env.Data
}

func TestNotificationHandler_List_Unauthenticated(t *testing.T) {
	svc := &mockNotificationSvc{}
	r := buildNotificationRouter(svc)

	w := doNotifRequest(r, http.MethodGet, "/users/me/notifications", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
	ok, code, _ := decodeNotificationEnvelope(t, w.Body.Bytes())
	if ok {
		t.Fatal("expected success=false")
	}
	if code != "AUTH_UNAUTHENTICATED" {
		t.Fatalf("expected error code AUTH_UNAUTHENTICATED, got %s", code)
	}
}

func TestNotificationHandler_List_Success(t *testing.T) {
	userID := uuid.New()
	now := time.Now().Truncate(time.Millisecond)
	taskID := uuid.New()
	notifs := []*notificationdom.Notification{
		{
			ID:              uuid.New(),
			RecipientUserID: userID,
			Type:            notificationdom.NotificationTypeAssigned,
			ActorFullName:   "Jane Doe",
			ActorUsername:   "janedoe",
			TaskID:          &taskID,
			TaskTitle:       "Do the thing",
			TaskNumber:      7,
			ProjectID:       uuid.New(),
			ProjectName:     "Paca",
			CreatedAt:       now,
		},
	}

	svc := &mockNotificationSvc{}
	svc.listNotifications = func(_ context.Context, gotUserID uuid.UUID, limit int, cursorAfter *string) ([]*notificationdom.Notification, bool, error) {
		if gotUserID != userID {
			t.Fatalf("expected user %s, got %s", userID, gotUserID)
		}
		if limit != 20 {
			t.Fatalf("expected default page_size 20, got %d", limit)
		}
		if cursorAfter != nil {
			t.Fatalf("expected nil cursor on first page, got %v", *cursorAfter)
		}
		return notifs, false, nil
	}
	svc.unreadCount = func(_ context.Context, _ uuid.UUID) (int64, error) {
		return 3, nil
	}

	r := buildNotificationRouter(svc)
	w := doNotifRequestWithActor(r, http.MethodGet, "/users/me/notifications", nil, userID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	ok, _, data := decodeNotificationEnvelope(t, w.Body.Bytes())
	if !ok {
		t.Fatal("expected success=true")
	}

	var resp struct {
		Items []struct {
			ID            string `json:"id"`
			Type          string `json:"type"`
			ActorFullName string `json:"actor_full_name"`
			ActorUsername string `json:"actor_username"`
			TaskTitle     string `json:"task_title"`
			ProjectName   string `json:"project_name"`
		} `json:"items"`
		PageSize    int     `json:"page_size"`
		NextCursor  *string `json:"next_cursor"`
		UnreadCount int64   `json:"unread_count"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].Type != "assigned" {
		t.Fatalf("expected type assigned, got %s", resp.Items[0].Type)
	}
	if resp.Items[0].ActorFullName != "Jane Doe" {
		t.Fatalf("expected actor Jane Doe, got %s", resp.Items[0].ActorFullName)
	}
	if resp.UnreadCount != 3 {
		t.Fatalf("expected unread_count 3, got %d", resp.UnreadCount)
	}
	if resp.PageSize != 20 {
		t.Fatalf("expected page_size 20, got %d", resp.PageSize)
	}
	if resp.NextCursor != nil {
		t.Fatalf("expected nil next_cursor when hasMore=false, got %v", *resp.NextCursor)
	}
}

func TestNotificationHandler_List_Empty(t *testing.T) {
	userID := uuid.New()
	svc := &mockNotificationSvc{}
	svc.listNotifications = func(_ context.Context, _ uuid.UUID, _ int, _ *string) ([]*notificationdom.Notification, bool, error) {
		return nil, false, nil
	}
	svc.unreadCount = func(_ context.Context, _ uuid.UUID) (int64, error) {
		return 0, nil
	}

	r := buildNotificationRouter(svc)
	w := doNotifRequestWithActor(r, http.MethodGet, "/users/me/notifications", nil, userID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	_, _, data := decodeNotificationEnvelope(t, w.Body.Bytes())
	var resp struct {
		Items       []any `json:"items"`
		UnreadCount int64 `json:"unread_count"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(resp.Items))
	}
}

func TestNotificationHandler_List_CursorAndPageSizeForwarded(t *testing.T) {
	userID := uuid.New()
	nextItem := &notificationdom.Notification{
		ID:            uuid.New(),
		CreatedAt:     time.Now(),
		ActorFullName: "Next Page Seed",
	}

	svc := &mockNotificationSvc{}
	var gotLimit int
	var gotCursor *string
	svc.listNotifications = func(_ context.Context, _ uuid.UUID, limit int, cursorAfter *string) ([]*notificationdom.Notification, bool, error) {
		gotLimit = limit
		gotCursor = cursorAfter
		return []*notificationdom.Notification{nextItem}, true, nil
	}
	svc.unreadCount = func(_ context.Context, _ uuid.UUID) (int64, error) {
		return 0, nil
	}

	r := buildNotificationRouter(svc)
	w := doNotifRequestWithActor(r, http.MethodGet, "/users/me/notifications?page_size=5&cursor=abc123", nil, userID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotLimit != 5 {
		t.Fatalf("expected page_size 5 forwarded as limit, got %d", gotLimit)
	}
	if gotCursor == nil || *gotCursor != "abc123" {
		t.Fatalf("expected cursor %q forwarded, got %v", "abc123", gotCursor)
	}

	_, _, data := decodeNotificationEnvelope(t, w.Body.Bytes())
	var resp struct {
		PageSize   int     `json:"page_size"`
		NextCursor *string `json:"next_cursor"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if resp.PageSize != 5 {
		t.Fatalf("expected response page_size 5, got %d", resp.PageSize)
	}
	if resp.NextCursor == nil {
		t.Fatal("expected non-nil next_cursor when hasMore=true")
	}
	wantCursor := notificationdom.EncodeNotificationCursor(nextItem)
	if *resp.NextCursor != wantCursor {
		t.Fatalf("expected next_cursor derived from the last item on the page, got %q want %q", *resp.NextCursor, wantCursor)
	}
}

func TestNotificationHandler_List_InvalidPageSize(t *testing.T) {
	userID := uuid.New()
	svc := &mockNotificationSvc{}

	r := buildNotificationRouter(svc)
	w := doNotifRequestWithActor(r, http.MethodGet, "/users/me/notifications?page_size=0", nil, userID)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestNotificationHandler_List_InvalidCursor(t *testing.T) {
	userID := uuid.New()
	svc := &mockNotificationSvc{}
	svc.listNotifications = func(_ context.Context, _ uuid.UUID, _ int, _ *string) ([]*notificationdom.Notification, bool, error) {
		return nil, false, notificationdom.ErrInvalidCursor
	}

	r := buildNotificationRouter(svc)
	w := doNotifRequestWithActor(r, http.MethodGet, "/users/me/notifications?cursor=garbage", nil, userID)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	ok, code, _ := decodeNotificationEnvelope(t, w.Body.Bytes())
	if ok {
		t.Fatal("expected success=false")
	}
	if code != "NOTIFICATION_INVALID_CURSOR" {
		t.Fatalf("expected error code NOTIFICATION_INVALID_CURSOR, got %s", code)
	}
}

func TestNotificationHandler_List_ServiceError(t *testing.T) {
	userID := uuid.New()
	svc := &mockNotificationSvc{}
	svc.listNotifications = func(_ context.Context, _ uuid.UUID, _ int, _ *string) ([]*notificationdom.Notification, bool, error) {
		return nil, false, errors.New("boom")
	}

	r := buildNotificationRouter(svc)
	w := doNotifRequestWithActor(r, http.MethodGet, "/users/me/notifications", nil, userID)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestNotificationHandler_List_UnreadCountError(t *testing.T) {
	userID := uuid.New()
	svc := &mockNotificationSvc{}
	svc.listNotifications = func(_ context.Context, _ uuid.UUID, _ int, _ *string) ([]*notificationdom.Notification, bool, error) {
		return nil, false, nil
	}
	svc.unreadCount = func(_ context.Context, _ uuid.UUID) (int64, error) {
		return 0, errors.New("unread count failure")
	}

	r := buildNotificationRouter(svc)
	w := doNotifRequestWithActor(r, http.MethodGet, "/users/me/notifications", nil, userID)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestNotificationHandler_MarkAsRead_Unauthenticated(t *testing.T) {
	svc := &mockNotificationSvc{}
	r := buildNotificationRouter(svc)

	w := doNotifRequest(r, http.MethodPatch, fmt.Sprintf("/users/me/notifications/%s/read", uuid.New()), nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
	ok, code, _ := decodeNotificationEnvelope(t, w.Body.Bytes())
	if ok {
		t.Fatal("expected success=false")
	}
	if code != "AUTH_UNAUTHENTICATED" {
		t.Fatalf("expected error code AUTH_UNAUTHENTICATED, got %s", code)
	}
}

func TestNotificationHandler_MarkAsRead_InvalidUUID(t *testing.T) {
	svc := &mockNotificationSvc{}
	r := buildNotificationRouter(svc)
	userID := uuid.New()

	w := doNotifRequestWithActor(r, http.MethodPatch, "/users/me/notifications/not-a-uuid/read", nil, userID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	ok, code, _ := decodeNotificationEnvelope(t, w.Body.Bytes())
	if ok {
		t.Fatal("expected success=false")
	}
	if code != "BAD_REQUEST" {
		t.Fatalf("expected error code BAD_REQUEST, got %s", code)
	}
}

func TestNotificationHandler_MarkAsRead_Success(t *testing.T) {
	userID := uuid.New()
	notifID := uuid.New()

	var capturedID, capturedUserID uuid.UUID
	svc := &mockNotificationSvc{}
	svc.markAsRead = func(_ context.Context, id, uid uuid.UUID) error {
		capturedID = id
		capturedUserID = uid
		return nil
	}

	r := buildNotificationRouter(svc)
	w := doNotifRequestWithActor(r, http.MethodPatch, fmt.Sprintf("/users/me/notifications/%s/read", notifID), nil, userID)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if capturedID != notifID {
		t.Fatalf("expected notification id %s, got %s", notifID, capturedID)
	}
	if capturedUserID != userID {
		t.Fatalf("expected user id %s, got %s", userID, capturedUserID)
	}
}

func TestNotificationHandler_MarkAsRead_NotFoundError(t *testing.T) {
	userID := uuid.New()
	notifID := uuid.New()

	svc := &mockNotificationSvc{}
	svc.markAsRead = func(_ context.Context, _, _ uuid.UUID) error {
		return notificationdom.ErrNotificationNotFound
	}

	r := buildNotificationRouter(svc)
	w := doNotifRequestWithActor(r, http.MethodPatch, fmt.Sprintf("/users/me/notifications/%s/read", notifID), nil, userID)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	ok, code, _ := decodeNotificationEnvelope(t, w.Body.Bytes())
	if ok {
		t.Fatal("expected success=false")
	}
	if code != "NOTIFICATION_NOT_FOUND" {
		t.Fatalf("expected error code NOTIFICATION_NOT_FOUND, got %s", code)
	}
}

func TestNotificationHandler_MarkAllAsRead_Unauthenticated(t *testing.T) {
	svc := &mockNotificationSvc{}
	r := buildNotificationRouter(svc)

	w := doNotifRequest(r, http.MethodPost, "/users/me/notifications/read-all", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
	ok, code, _ := decodeNotificationEnvelope(t, w.Body.Bytes())
	if ok {
		t.Fatal("expected success=false")
	}
	if code != "AUTH_UNAUTHENTICATED" {
		t.Fatalf("expected error code AUTH_UNAUTHENTICATED, got %s", code)
	}
}

func TestNotificationHandler_MarkAllAsRead_Success(t *testing.T) {
	userID := uuid.New()

	var capturedUserID uuid.UUID
	svc := &mockNotificationSvc{}
	svc.markAllAsRead = func(_ context.Context, uid uuid.UUID) error {
		capturedUserID = uid
		return nil
	}

	r := buildNotificationRouter(svc)
	w := doNotifRequestWithActor(r, http.MethodPost, "/users/me/notifications/read-all", nil, userID)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if capturedUserID != userID {
		t.Fatalf("expected user id %s, got %s", userID, capturedUserID)
	}
}

func TestNotificationHandler_MarkAllAsRead_ServiceError(t *testing.T) {
	userID := uuid.New()

	svc := &mockNotificationSvc{}
	svc.markAllAsRead = func(_ context.Context, _ uuid.UUID) error {
		return errors.New("db down")
	}

	r := buildNotificationRouter(svc)
	w := doNotifRequestWithActor(r, http.MethodPost, "/users/me/notifications/read-all", nil, userID)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

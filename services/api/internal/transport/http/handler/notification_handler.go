package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

const (
	defaultNotificationPageSize = 20
	maxNotificationPageSize     = 50
)

// NotificationHandler handles notification endpoints.
type NotificationHandler struct {
	svc       notificationdom.Service
	avatarSvc attachmentdom.AvatarService
}

// NewNotificationHandler returns a NotificationHandler wired to svc.
func NewNotificationHandler(svc notificationdom.Service, opts ...NotificationHandlerOption) *NotificationHandler {
	h := &NotificationHandler{svc: svc}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// NotificationHandlerOption is a functional option for NotificationHandler.
type NotificationHandlerOption func(*NotificationHandler)

// WithNotificationAvatarService configures avatar URL resolution for
// notification responses (the actor's avatar shown in the notification list).
func WithNotificationAvatarService(svc attachmentdom.AvatarService) NotificationHandlerOption {
	return func(h *NotificationHandler) {
		h.avatarSvc = svc
	}
}

// toNotificationResponse maps n to a NotificationResponse and, if an
// AvatarService is configured, resolves the actor's avatar keys into
// presigned display URLs.
func (h *NotificationHandler) toNotificationResponse(ctx context.Context, n *notificationdom.Notification) dto.NotificationResponse {
	resp := dto.NotificationFromEntity(n)
	if h.avatarSvc != nil {
		resp.ActorAvatarURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, n.ActorAvatarKey)
		resp.ActorAvatarThumbURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, n.ActorAvatarThumbKey)
	}
	return resp
}

// List handles GET /users/me/notifications.
// Returns a keyset-paginated page of the authenticated user's notifications,
// newest first, together with the current unread count (which always
// reflects the full unread total, not just this page).
//
// Supported query params (both optional):
//   - page_size=<1-50>    defaults to 20
//   - cursor=<opaque>     from the previous page's next_cursor; omit for the first page
func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.ActorIDFromContext(r.Context())
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	pageSize, err := parsePageSize(r, defaultNotificationPageSize, maxNotificationPageSize)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var cursor *string
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor = &raw
	}

	notifications, hasMore, err := h.svc.ListNotifications(r.Context(), userID, pageSize, cursor)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	unreadCount, err := h.svc.UnreadCount(r.Context(), userID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	items := make([]dto.NotificationResponse, 0, len(notifications))
	for _, n := range notifications {
		items = append(items, h.toNotificationResponse(r.Context(), n))
	}

	var nextCursor *string
	if hasMore && len(notifications) > 0 {
		s := notificationdom.EncodeNotificationCursor(notifications[len(notifications)-1])
		nextCursor = &s
	}

	presenter.OK(w, r, dto.NotificationListResponse{
		Items:       items,
		PageSize:    pageSize,
		NextCursor:  nextCursor,
		UnreadCount: unreadCount,
	})
}

// MarkAsRead handles PATCH /users/me/notifications/:notificationId/read.
func (h *NotificationHandler) MarkAsRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.ActorIDFromContext(r.Context())
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	notificationID, err := uuid.Parse(chi.URLParam(r, "notificationId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid notification id"))
		return
	}

	if err := h.svc.MarkAsRead(r.Context(), notificationID, userID); err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.NoContent(w)
}

// MarkAllAsRead handles POST /users/me/notifications/read-all.
func (h *NotificationHandler) MarkAllAsRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.ActorIDFromContext(r.Context())
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	if err := h.svc.MarkAllAsRead(r.Context(), userID); err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.NoContent(w)
}

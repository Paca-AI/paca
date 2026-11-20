package handler

import (
	"context"
	"net/http"
	"strings"

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
//   - type=<a,b>          keep only these types (assigned, mentioned, question, automation)
//   - project_id=<uuid>   keep only one project's notifications
//   - actor=agent         keep only notifications caused by an agent
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
	cursor := parseCursorParam(r)

	filter, err := parseNotificationFilter(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	notifications, hasMore, err := h.svc.ListNotifications(r.Context(), userID, pageSize, cursor, filter)
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

// parseNotificationFilter reads the list endpoint's optional filters.
func parseNotificationFilter(r *http.Request) (notificationdom.ListFilter, error) {
	var f notificationdom.ListFilter
	q := r.URL.Query()
	if raw := q.Get("type"); raw != "" {
		parts := strings.Split(raw, ",")
		if len(parts) > 4 {
			return f, apierr.New(apierr.CodeBadRequest, "too many notification types")
		}
		for _, t := range parts {
			switch nt := notificationdom.NotificationType(strings.TrimSpace(t)); nt {
			case notificationdom.NotificationTypeAssigned, notificationdom.NotificationTypeMentioned,
				notificationdom.NotificationTypeQuestion, notificationdom.NotificationTypeAutomation:
				f.Types = append(f.Types, nt)
			default:
				return f, apierr.New(apierr.CodeBadRequest, "invalid notification type")
			}
		}
	}
	if raw := q.Get("project_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return f, apierr.New(apierr.CodeBadRequest, "invalid project id")
		}
		f.ProjectID = &id
	}
	switch q.Get("actor") {
	case "":
	case "agent":
		f.AgentActor = true
	default:
		return f, apierr.New(apierr.CodeBadRequest, "invalid actor filter")
	}
	return f, nil
}

// UnreadMentions handles GET /users/me/notifications/unread-mentions: the
// caller's unread mentions per task, for the badges on task cards and sprint
// headers. Optional project_id narrows it to one project.
func (h *NotificationHandler) UnreadMentions(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.ActorIDFromContext(r.Context())
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}
	var projectID *uuid.UUID
	if raw := r.URL.Query().Get("project_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid project id"))
			return
		}
		projectID = &id
	}
	rows, err := h.svc.UnreadMentionsByTask(r.Context(), userID, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	items := make([]dto.UnreadMentionResponse, 0, len(rows))
	for _, m := range rows {
		items = append(items, dto.UnreadMentionResponse{ProjectID: m.ProjectID, TaskID: m.TaskID, SprintID: m.SprintID, Count: m.Count})
	}
	presenter.OK(w, r, items)
}

// OpenTask handles PUT /projects/:projectId/tasks/:taskId/read: the caller
// opened the task. It records the view, reads the caller's mentions on it and
// answers with the previous view time for the "unread since" divider. The
// router gates it on read access to the task.
func (h *NotificationHandler) OpenTask(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.ActorIDFromContext(r.Context())
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}
	projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid project id"))
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "taskId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid task id"))
		return
	}
	// The task must belong to the project in the URL: the router gate names the
	// task by those two ids but does not check they match.
	prev, err := h.svc.OpenTask(r.Context(), userID, projectID, taskID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.TaskReadResponse{PreviousSeenAt: prev})
}

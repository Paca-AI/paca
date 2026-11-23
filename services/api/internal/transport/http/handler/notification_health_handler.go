package handler

import (
	"net/http"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// NotificationHealthHandler serves the notification pipeline's health to
// administrators. The router gates it; the handler only reports.
type NotificationHealthHandler struct {
	svc notificationdom.HealthService
}

// NewNotificationHealthHandler returns a handler wired to svc.
func NewNotificationHealthHandler(svc notificationdom.HealthService) *NotificationHealthHandler {
	return &NotificationHealthHandler{svc: svc}
}

// Get handles GET /admin/notifications/health.
func (h *NotificationHealthHandler) Get(w http.ResponseWriter, r *http.Request) {
	health, err := h.svc.Health(r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.NotificationHealthFromEntity(health))
}

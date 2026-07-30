package dto

import (
	"time"

	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
)

// NotificationResponse is the API representation of a notification.
type NotificationResponse struct {
	ID            uuid.UUID  `json:"id"`
	Type          string     `json:"type"`
	ActorFullName string     `json:"actor_full_name"`
	ActorUsername string     `json:"actor_username"`
	TaskID        *uuid.UUID `json:"task_id"`
	TaskTitle     string     `json:"task_title"`
	TaskNumber    int        `json:"task_number"`
	ProjectID     uuid.UUID  `json:"project_id"`
	ProjectName   string     `json:"project_name"`
	ReadAt        *time.Time `json:"read_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

// NotificationFromEntity converts a domain Notification to a response DTO.
func NotificationFromEntity(n *notificationdom.Notification) NotificationResponse {
	return NotificationResponse{
		ID:            n.ID,
		Type:          string(n.Type),
		ActorFullName: n.ActorFullName,
		ActorUsername: n.ActorUsername,
		TaskID:        n.TaskID,
		TaskTitle:     n.TaskTitle,
		TaskNumber:    n.TaskNumber,
		ProjectID:     n.ProjectID,
		ProjectName:   n.ProjectName,
		ReadAt:        n.ReadAt,
		CreatedAt:     n.CreatedAt,
	}
}

// NotificationListResponse wraps a slice of notifications.
type NotificationListResponse struct {
	Items       []NotificationResponse `json:"items"`
	UnreadCount int64                  `json:"unread_count"`
}

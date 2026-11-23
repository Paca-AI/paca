package dto

import (
	"time"

	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
)

// NotificationResponse is the API representation of a notification.
type NotificationResponse struct {
	ID                  uuid.UUID `json:"id"`
	Type                string    `json:"type"`
	ActorFullName       string    `json:"actor_full_name"`
	ActorUsername       string    `json:"actor_username"`
	ActorAvatarURL      *string   `json:"actor_avatar_url,omitempty"`
	ActorAvatarThumbURL *string   `json:"actor_avatar_thumb_url,omitempty"`
	// ActorMemberType/ActorAgentType/ActorAgentLLMProvider/ActorAgentACPProvider
	// let the frontend pick a default provider-logo avatar for agent actors
	// with no custom avatar uploaded — mirrors ProjectMemberResponse's
	// equivalent fields.
	ActorMemberType       string     `json:"actor_member_type,omitempty"`
	ActorAgentType        string     `json:"actor_agent_type,omitempty"`
	ActorAgentLLMProvider string     `json:"actor_agent_llm_provider,omitempty"`
	ActorAgentACPProvider *string    `json:"actor_agent_acp_provider,omitempty"`
	ActorAgentCLIProvider *string    `json:"actor_agent_cli_provider,omitempty"`
	TaskID                *uuid.UUID `json:"task_id"`
	TaskTitle             string     `json:"task_title"`
	TaskNumber            int        `json:"task_number"`
	// ProjectID is nil for a question asked in a global-chat conversation.
	ProjectID         *uuid.UUID `json:"project_id"`
	ProjectName       string     `json:"project_name"`
	DocumentID        *uuid.UUID `json:"document_id"`
	DocumentTitle     string     `json:"document_title"`
	ConversationID    *uuid.UUID `json:"conversation_id"`
	ConversationTitle string     `json:"conversation_title"`
	// Message is the text of an automation notification; empty otherwise.
	Message   string     `json:"message"`
	ReadAt    *time.Time `json:"read_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// NotificationFromEntity converts a domain Notification to a response DTO.
func NotificationFromEntity(n *notificationdom.Notification) NotificationResponse {
	var projectID *uuid.UUID
	if n.ProjectID != uuid.Nil {
		id := n.ProjectID
		projectID = &id
	}
	return NotificationResponse{
		ID:                    n.ID,
		Type:                  string(n.Type),
		ActorFullName:         n.ActorFullName,
		ActorUsername:         n.ActorUsername,
		ActorMemberType:       n.ActorMemberType,
		ActorAgentType:        n.ActorAgentType,
		ActorAgentLLMProvider: n.ActorAgentLLMProvider,
		ActorAgentACPProvider: n.ActorAgentACPProvider,
		ActorAgentCLIProvider: n.ActorAgentCLIProvider,
		TaskID:                n.TaskID,
		TaskTitle:             n.TaskTitle,
		TaskNumber:            n.TaskNumber,
		ProjectID:             projectID,
		ProjectName:           n.ProjectName,
		DocumentID:            n.DocumentID,
		DocumentTitle:         n.DocumentTitle,
		ConversationID:        n.ConversationID,
		ConversationTitle:     n.ConversationTitle,
		Message:               n.Message,
		ReadAt:                n.ReadAt,
		CreatedAt:             n.CreatedAt,
	}
}

// NotificationListResponse wraps a keyset-paginated page of notifications.
// NextCursor is nil when this is the last page.
type NotificationListResponse struct {
	Items       []NotificationResponse `json:"items"`
	PageSize    int                    `json:"page_size"`
	NextCursor  *string                `json:"next_cursor"`
	UnreadCount int64                  `json:"unread_count"`
}

// PushPreference is whether push notifications are enabled for one type.
type PushPreference struct {
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
}

// PushSettingsResponse is the caller's Web Push state.
type PushSettingsResponse struct {
	// PublicKey is the VAPID public key a browser subscribes against.
	PublicKey string `json:"public_key"`
	// SubscriptionCount is how many of the caller's browsers are subscribed.
	SubscriptionCount int              `json:"subscription_count"`
	Preferences       []PushPreference `json:"preferences"`
	DoNotDisturb      DoNotDisturb     `json:"do_not_disturb"`
}

// DoNotDisturb is whether the caller's pushes are silenced, and until when.
type DoNotDisturb struct {
	Active bool `json:"active"`
	// Until is when it ends; null when it is off or has no end.
	Until *time.Time `json:"until"`
	// Indefinite is true when it lasts until the caller turns it off.
	Indefinite bool `json:"indefinite"`
}

// DoNotDisturbRequest turns Do Not Disturb on until a time (or until turned
// off), or off.
type DoNotDisturbRequest struct {
	// Until is when it should end; null turns it off (unless Indefinite).
	Until      *time.Time `json:"until"`
	Indefinite bool       `json:"indefinite"`
}

// PushSubscribeRequest is a browser PushSubscription.toJSON().
type PushSubscribeRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// PushUnsubscribeRequest names the subscription to remove.
type PushUnsubscribeRequest struct {
	Endpoint string `json:"endpoint"`
}

// NotificationHealthResponse is the notification pipeline's health.
type NotificationHealthResponse struct {
	// Status is "ok", "degraded" or "failing".
	Status    string                  `json:"status"`
	Reasons   []string                `json:"reasons"`
	CheckedAt time.Time               `json:"checked_at"`
	Channels  []DeliveryChannelHealth `json:"channels"`
	Streams   []StreamHealthResponse  `json:"streams"`
}

// DeliveryChannelHealth is the delivery outbox for one channel.
type DeliveryChannelHealth struct {
	Channel        string     `json:"channel"`
	Pending        int64      `json:"pending"`
	Retrying       int64      `json:"retrying"`
	Overdue        int64      `json:"overdue"`
	OldestOverdue  *time.Time `json:"oldest_overdue"`
	Sent           int64      `json:"sent"`
	Failed         int64      `json:"failed"`
	SentLastHour   int64      `json:"sent_last_hour"`
	FailedLastHour int64      `json:"failed_last_hour"`
	// Latency is creation-to-delivery in seconds over the last hour; null when
	// nothing was sent.
	LatencyP50Seconds *float64        `json:"latency_p50_seconds"`
	LatencyP95Seconds *float64        `json:"latency_p95_seconds"`
	TopErrors         []ErrorCountDTO `json:"top_errors"`
}

// ErrorCountDTO is a failure reason and how often it happened.
type ErrorCountDTO struct {
	Error string `json:"error"`
	Count int64  `json:"count"`
}

// StreamHealthResponse is one consumer group's state.
type StreamHealthResponse struct {
	Stream                string `json:"stream"`
	Group                 string `json:"group"`
	Pending               int64  `json:"pending"`
	OldestPendingIdleSecs int64  `json:"oldest_pending_idle_seconds"`
	DeadLastHour          int64  `json:"dead_last_hour"`
	DeadTotal             int64  `json:"dead_total"`
}

// NotificationHealthFromEntity maps h to its API representation.
func NotificationHealthFromEntity(h *notificationdom.Health) NotificationHealthResponse {
	out := NotificationHealthResponse{
		Status: string(h.Status), Reasons: append([]string{}, h.Reasons...), CheckedAt: h.CheckedAt,
		Channels: make([]DeliveryChannelHealth, 0, len(h.Channels)), Streams: make([]StreamHealthResponse, 0, len(h.Streams)),
	}
	for _, c := range h.Channels {
		d := DeliveryChannelHealth{
			Channel: string(c.Channel), Pending: c.Pending, Retrying: c.Retrying, Overdue: c.Overdue, OldestOverdue: c.OldestOverdue,
			Sent: c.Sent, Failed: c.Failed, SentLastHour: c.SentLastHour, FailedLastHour: c.FailedLastHour,
			LatencyP50Seconds: c.LatencyP50, LatencyP95Seconds: c.LatencyP95, TopErrors: []ErrorCountDTO{},
		}
		for _, e := range c.TopErrors {
			d.TopErrors = append(d.TopErrors, ErrorCountDTO{Error: e.Error, Count: e.Count})
		}
		out.Channels = append(out.Channels, d)
	}
	for _, s := range h.Streams {
		out.Streams = append(out.Streams, StreamHealthResponse{
			Stream: s.Stream, Group: s.Group, Pending: s.Pending, OldestPendingIdleSecs: int64(s.OldestPendingIdle.Seconds()),
			DeadLastHour: s.DeadLastHour, DeadTotal: s.DeadTotal,
		})
	}
	return out
}

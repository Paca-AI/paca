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
	Message string `json:"message"`
	// CommentID, MentionSource and Excerpt describe a mention: the comment it
	// points at (nil for a description or document body), where it was made
	// ("comment", "description", "document", "doc_comment") and the stored
	// start of the text.
	CommentID     *uuid.UUID `json:"comment_id"`
	MentionSource string     `json:"mention_source"`
	Excerpt       string     `json:"excerpt"`
	// TaskStatusName/Color and SprintID/SprintName are where the task is now
	// (joined at read time). SprintID is nil for a task in the backlog.
	TaskStatusName  string     `json:"task_status_name"`
	TaskStatusColor string     `json:"task_status_color"`
	SprintID        *uuid.UUID `json:"sprint_id"`
	SprintName      string     `json:"sprint_name"`
	ReadAt          *time.Time `json:"read_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

// UnreadMentionResponse is the count of one task's unread mentions.
type UnreadMentionResponse struct {
	ProjectID uuid.UUID  `json:"project_id"`
	TaskID    uuid.UUID  `json:"task_id"`
	SprintID  *uuid.UUID `json:"sprint_id"`
	Count     int64      `json:"count"`
}

// TaskReadResponse says when the caller last viewed a task before now.
type TaskReadResponse struct {
	// PreviousSeenAt is nil the first time the caller opens the task.
	PreviousSeenAt *time.Time `json:"previous_seen_at"`
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
		CommentID:             n.CommentID,
		MentionSource:         string(n.MentionSource),
		Excerpt:               n.Excerpt,
		TaskStatusName:        n.TaskStatusName,
		TaskStatusColor:       n.TaskStatusColor,
		SprintID:              n.SprintID,
		SprintName:            n.SprintName,
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

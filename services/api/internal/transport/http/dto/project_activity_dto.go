package dto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	activitydom "github.com/Paca-AI/api/internal/domain/activity"
)

// ProjectActivityResponse is one entry of the project-wide activity log.
type ProjectActivityResponse struct {
	ID         uuid.UUID  `json:"id"`
	EntityType string     `json:"entity_type"`
	EntityID   *uuid.UUID `json:"entity_id,omitempty"`
	// EntityTitle is the entity's current title; empty when EntityDeleted.
	EntityTitle   string     `json:"entity_title"`
	EntityDeleted bool       `json:"entity_deleted"`
	ActorID       *uuid.UUID `json:"actor_id,omitempty"`
	ActorName     string     `json:"actor_name"`
	ActorUsername string     `json:"actor_username"`
	// ActorAvatarURL/ActorAvatarThumbURL are presigned GET URLs, populated by
	// the handler (not this mapper) — nil when the actor has no avatar.
	ActorAvatarURL      *string         `json:"actor_avatar_url,omitempty"`
	ActorAvatarThumbURL *string         `json:"actor_avatar_thumb_url,omitempty"`
	Origin              string          `json:"origin"`
	ActivityType        string          `json:"activity_type"`
	Content             json.RawMessage `json:"content"`
	CreatedAt           time.Time       `json:"created_at"`
}

// ProjectActivityFromEntity maps a domain Activity to its response DTO.
func ProjectActivityFromEntity(a *activitydom.Activity) ProjectActivityResponse {
	content := a.Content
	if len(content) == 0 {
		content = json.RawMessage("{}")
	}
	return ProjectActivityResponse{
		ID:            a.ID,
		EntityType:    a.EntityType,
		EntityID:      a.EntityID,
		EntityTitle:   a.EntityTitle,
		EntityDeleted: a.EntityDeleted,
		ActorID:       a.ActorID,
		ActorName:     a.ActorName,
		ActorUsername: a.ActorUsername,
		Origin:        a.Origin,
		ActivityType:  a.ActivityType,
		Content:       content,
		CreatedAt:     a.CreatedAt,
	}
}

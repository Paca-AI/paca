package dto

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// --- Optional JSON field types ----------------------------------------------
// These types distinguish three states in a JSON PATCH body:
//   - field absent  → Set=false (do not overwrite the stored value)
//   - field = null  → Set=true, Value=nil  (explicitly clear the stored value)
//   - field = value → Set=true, Value=non-nil (set the stored value)

// OptionalUUID is a JSON-decodable field for nullable UUID columns.
type OptionalUUID struct {
	Set   bool
	Value *uuid.UUID
}

// UnmarshalJSON implements json.Unmarshaler. It marks the field as Set and
// decodes the value, treating JSON null as a nil pointer.
func (o *OptionalUUID) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var id uuid.UUID
	if err := json.Unmarshal(data, &id); err != nil {
		return fmt.Errorf("invalid uuid value: %w", err)
	}
	o.Value = &id
	return nil
}

// Ptr returns a **uuid.UUID suitable for UpdateTaskInput:
// nil when the field was absent, &nil when explicitly null, &&id when set.
func (o OptionalUUID) Ptr() **uuid.UUID {
	if !o.Set {
		return nil
	}
	return &o.Value
}

// OptionalUUIDSlice is a JSON-decodable field for a settable list of UUIDs
// (e.g. assignee_ids). Unlike OptionalUUID it has no null/absent distinction
// beyond "was this field present in the body" — JSON null is treated the same
// as an empty array (both clear the stored set).
type OptionalUUIDSlice struct {
	Set   bool
	Value []uuid.UUID
}

// UnmarshalJSON implements json.Unmarshaler. It marks the field as Set and
// decodes the value, treating JSON null as an empty (non-nil) slice.
func (o *OptionalUUIDSlice) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = []uuid.UUID{}
		return nil
	}
	var ids []uuid.UUID
	if err := json.Unmarshal(data, &ids); err != nil {
		return fmt.Errorf("invalid uuid array value: %w", err)
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	o.Value = ids
	return nil
}

// Ptr returns a *[]uuid.UUID suitable for UpdateTaskInput.AssigneeIDs: nil
// when the field was absent (don't overwrite), non-nil when it was set
// (replace in full, possibly with an empty slice to clear).
func (o OptionalUUIDSlice) Ptr() *[]uuid.UUID {
	if !o.Set {
		return nil
	}
	return &o.Value
}

// OptionalString is a JSON-decodable field for nullable string columns.
type OptionalString struct {
	Set   bool
	Value *string
}

// UnmarshalJSON implements json.Unmarshaler. It marks the field as Set and
// decodes the value, treating JSON null as a nil pointer.
func (o *OptionalString) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("invalid string value: %w", err)
	}
	o.Value = &s
	return nil
}

// Ptr returns a **string for use in patch inputs: nil when absent, &nil when
// explicitly null, or a pointer-to-pointer when set to a value.
func (o OptionalString) Ptr() **string {
	if !o.Set {
		return nil
	}
	return &o.Value
}

// OptionalTime is a JSON-decodable field for nullable time columns.
type OptionalTime struct {
	Set   bool
	Value *time.Time
}

// UnmarshalJSON implements json.Unmarshaler. It marks the field as Set and
// decodes the value, treating JSON null as a nil pointer.
func (o *OptionalTime) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var t time.Time
	if err := json.Unmarshal(data, &t); err != nil {
		return fmt.Errorf("invalid time value: %w", err)
	}
	o.Value = &t
	return nil
}

// Ptr returns a **time.Time for use in patch inputs: nil when absent, &nil
// when explicitly null, or a pointer-to-pointer when set to a value.
func (o OptionalTime) Ptr() **time.Time {
	if !o.Set {
		return nil
	}
	return &o.Value
}

// OptionalJSON is a JSON-decodable field for nullable JSONB columns.
// It distinguishes between a field that was absent in the request body
// (Set=false) and one that was explicitly set to null or a JSON value.
type OptionalJSON struct {
	Set   bool
	Value json.RawMessage
}

// UnmarshalJSON implements json.Unmarshaler. It marks the field as Set and
// stores the raw JSON bytes, treating JSON null as a nil RawMessage.
func (o *OptionalJSON) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	o.Value = json.RawMessage(data)
	return nil
}

// Ptr returns a *json.RawMessage for use in patch inputs: nil when absent,
// &nil when explicitly null, or a pointer to the raw bytes when set.
func (o OptionalJSON) Ptr() *json.RawMessage {
	if !o.Set {
		return nil
	}
	return &o.Value
}

// OptionalInt is a JSON-decodable field for nullable integer columns.
type OptionalInt struct {
	Set   bool
	Value *int
}

// UnmarshalJSON implements json.Unmarshaler. It marks the field as Set and
// decodes the value, treating JSON null as a nil pointer.
func (o *OptionalInt) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("invalid int value: %w", err)
	}
	o.Value = &n
	return nil
}

// Ptr returns a **int for use in patch inputs: nil when absent, &nil when
// explicitly null, or a pointer-to-pointer when set to a value.
func (o OptionalInt) Ptr() **int {
	if !o.Set {
		return nil
	}
	return &o.Value
}

// --- Task Type DTOs ---------------------------------------------------------

// CreateTaskTypeRequest is the body for POST /projects/:projectId/task-types.
type CreateTaskTypeRequest struct {
	Name        string  `json:"name" binding:"required"`
	Icon        *string `json:"icon"`
	Color       *string `json:"color"`
	Description *string `json:"description"`
}

// UpdateTaskTypeRequest is the body for PATCH /projects/:projectId/task-types/:typeId.
// Icon, Color, and Description use the Optional* wrapper types so an absent
// field leaves the stored value unchanged, distinct from an explicit JSON null.
type UpdateTaskTypeRequest struct {
	Name        string         `json:"name"`
	Icon        OptionalString `json:"icon"`
	Color       OptionalString `json:"color"`
	Description OptionalString `json:"description"`
}

// TaskTypeResponse is the public representation of a task type.
type TaskTypeResponse struct {
	ID          uuid.UUID `json:"id"`
	ProjectID   uuid.UUID `json:"project_id"`
	Name        string    `json:"name"`
	Icon        *string   `json:"icon,omitempty"`
	Color       *string   `json:"color,omitempty"`
	Description *string   `json:"description,omitempty"`
	IsDefault   bool      `json:"is_default"`
	IsSystem    bool      `json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TaskTypeFromEntity maps a domain TaskType to a TaskTypeResponse DTO.
func TaskTypeFromEntity(t *taskdom.TaskType) TaskTypeResponse {
	return TaskTypeResponse{
		ID:          t.ID,
		ProjectID:   t.ProjectID,
		Name:        t.Name,
		Icon:        t.Icon,
		Color:       t.Color,
		Description: t.Description,
		IsDefault:   t.IsDefault,
		IsSystem:    t.IsSystem,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

// --- Task Status DTOs -------------------------------------------------------

// CreateTaskStatusRequest is the body for POST /projects/:projectId/task-statuses.
type CreateTaskStatusRequest struct {
	Name     string                 `json:"name" binding:"required"`
	Color    *string                `json:"color"`
	Position int                    `json:"position"`
	Category taskdom.StatusCategory `json:"category" binding:"required"`
}

// UpdateTaskStatusRequest is the body for PATCH /projects/:projectId/task-statuses/:statusId.
type UpdateTaskStatusRequest struct {
	Name     string                  `json:"name"`
	Color    *string                 `json:"color"`
	Position *int                    `json:"position"`
	Category *taskdom.StatusCategory `json:"category"`
}

// TaskStatusResponse is the public representation of a task status.
type TaskStatusResponse struct {
	ID        uuid.UUID              `json:"id"`
	ProjectID uuid.UUID              `json:"project_id"`
	Name      string                 `json:"name"`
	Color     *string                `json:"color,omitempty"`
	Position  int                    `json:"position"`
	Category  taskdom.StatusCategory `json:"category"`
	IsDefault bool                   `json:"is_default"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// ReorderTaskStatusesRequest is the body for PUT /projects/:projectId/task-statuses/positions.
type ReorderTaskStatusesRequest struct {
	StatusIDs []uuid.UUID `json:"status_ids" binding:"required,min=1"`
}

// TaskStatusFromEntity maps a domain TaskStatus to a TaskStatusResponse DTO.
func TaskStatusFromEntity(s *taskdom.TaskStatus) TaskStatusResponse {
	return TaskStatusResponse{
		ID:        s.ID,
		ProjectID: s.ProjectID,
		Name:      s.Name,
		Color:     s.Color,
		Position:  s.Position,
		Category:  s.Category,
		IsDefault: s.IsDefault,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

// --- Task DTOs --------------------------------------------------------------

// CreateTaskRequest is the body for POST /projects/:projectId/tasks.
type CreateTaskRequest struct {
	Title        string           `json:"title"`
	TaskTypeID   *uuid.UUID       `json:"task_type_id"`
	StatusID     *uuid.UUID       `json:"status_id"`
	SprintID     *uuid.UUID       `json:"sprint_id"`
	ParentTaskID *uuid.UUID       `json:"parent_task_id"`
	Description  *json.RawMessage `json:"description"`
	Importance   int              `json:"importance"`
	StoryPoints  *int             `json:"story_points"`
	AssigneeIDs  []uuid.UUID      `json:"assignee_ids"`
	ReporterID   *uuid.UUID       `json:"reporter_id"`
	CustomFields map[string]any   `json:"custom_fields"`
	StartDate    *time.Time       `json:"start_date"`
	DueDate      *time.Time       `json:"due_date"`
	Tags         []string         `json:"tags"`
}

// NormalizedDescription returns the description as a json.RawMessage suitable
// for CreateTaskInput. Both a missing field (nil pointer) and an explicit JSON
// null value are normalized to nil (SQL NULL).
func (r CreateTaskRequest) NormalizedDescription() json.RawMessage {
	if r.Description == nil || string(*r.Description) == "null" {
		return nil
	}
	return *r.Description
}

// UpdateTaskRequest is the body for PATCH /projects/:projectId/tasks/:taskId.
// Only fields present in the JSON body are applied; absent fields are left unchanged.
// For Tags and CustomFields, a nil pointer means absent (don't update); a non-nil
// pointer (even to an empty slice/map) means the field was explicitly set.
type UpdateTaskRequest struct {
	Title        string            `json:"title"`
	TaskTypeID   OptionalUUID      `json:"task_type_id"`
	StatusID     OptionalUUID      `json:"status_id"`
	SprintID     OptionalUUID      `json:"sprint_id"`
	ParentTaskID OptionalUUID      `json:"parent_task_id"`
	Description  OptionalJSON      `json:"description"`
	Importance   *int              `json:"importance"`
	StoryPoints  OptionalInt       `json:"story_points"`
	AssigneeIDs  OptionalUUIDSlice `json:"assignee_ids"`
	ReporterID   OptionalUUID      `json:"reporter_id"`
	CustomFields *map[string]any   `json:"custom_fields"`
	StartDate    OptionalTime      `json:"start_date"`
	DueDate      OptionalTime      `json:"due_date"`
	Tags         *[]string         `json:"tags"`
}

// TaskResponse is the public representation of a task.
// ViewPosition and ViewGroupKey are only populated when the caller supplies a
// valid view_id query parameter on list endpoints; they reflect the task's
// manual position within that view.
type TaskResponse struct {
	ID           uuid.UUID       `json:"id"`
	ProjectID    uuid.UUID       `json:"project_id"`
	TaskNumber   int64           `json:"task_number"`
	Title        string          `json:"title"`
	TaskTypeID   *uuid.UUID      `json:"task_type_id,omitempty"`
	StatusID     *uuid.UUID      `json:"status_id,omitempty"`
	SprintID     *uuid.UUID      `json:"sprint_id,omitempty"`
	ParentTaskID *uuid.UUID      `json:"parent_task_id,omitempty"`
	Description  json.RawMessage `json:"description,omitempty"`
	Importance   int             `json:"importance"`
	StoryPoints  *int            `json:"story_points,omitempty"`
	AssigneeIDs  []uuid.UUID     `json:"assignee_ids"`
	ReporterID   *uuid.UUID      `json:"reporter_id,omitempty"`
	CustomFields map[string]any  `json:"custom_fields"`
	StartDate    *time.Time      `json:"start_date,omitempty"`
	DueDate      *time.Time      `json:"due_date,omitempty"`
	Tags         []string        `json:"tags"`
	ViewPosition *float64        `json:"view_position,omitempty"`
	ViewGroupKey *string         `json:"view_group_key,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// TaskFromEntity maps a domain Task to a TaskResponse DTO.
func TaskFromEntity(t *taskdom.Task) TaskResponse {
	cf := t.CustomFields
	if cf == nil {
		cf = map[string]any{}
	}
	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}
	assigneeIDs := t.AssigneeIDs
	if assigneeIDs == nil {
		assigneeIDs = []uuid.UUID{}
	}
	return TaskResponse{
		ID:           t.ID,
		ProjectID:    t.ProjectID,
		TaskNumber:   t.TaskNumber,
		Title:        t.Title,
		TaskTypeID:   t.TaskTypeID,
		StatusID:     t.StatusID,
		SprintID:     t.SprintID,
		ParentTaskID: t.ParentTaskID,
		Description:  t.Description,
		Importance:   t.Importance,
		StoryPoints:  t.StoryPoints,
		AssigneeIDs:  assigneeIDs,
		ReporterID:   t.ReporterID,
		CustomFields: cf,
		StartDate:    t.StartDate,
		DueDate:      t.DueDate,
		Tags:         tags,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}
}

// --- Custom Field Definition DTOs ------------------------------------------

// CustomFieldOptionDTO is one selectable value of a select / multi_select
// custom field, with an optional display color. Mirrors
// taskdom.CustomFieldOption.
type CustomFieldOptionDTO struct {
	Value string  `json:"value"`
	Color *string `json:"color,omitempty"`
}

// CreateCustomFieldDefinitionRequest is the body for
// POST /projects/:projectId/custom-fields.
type CreateCustomFieldDefinitionRequest struct {
	FieldKey    string                 `json:"field_key" binding:"required"`
	DisplayName string                 `json:"display_name" binding:"required"`
	FieldType   taskdom.FieldType      `json:"field_type" binding:"required"`
	Options     []CustomFieldOptionDTO `json:"options"`
	IsRequired  bool                   `json:"is_required"`
}

// UpdateCustomFieldDefinitionRequest is the body for
// PATCH /projects/:projectId/custom-fields/:fieldId.
type UpdateCustomFieldDefinitionRequest struct {
	DisplayName string                 `json:"display_name"`
	FieldType   *taskdom.FieldType     `json:"field_type"`
	Options     []CustomFieldOptionDTO `json:"options"`
	IsRequired  *bool                  `json:"is_required"`
}

// CustomFieldDefinitionResponse is the public representation of a custom
// field definition.
type CustomFieldDefinitionResponse struct {
	ID          uuid.UUID              `json:"id"`
	ProjectID   uuid.UUID              `json:"project_id"`
	FieldKey    string                 `json:"field_key"`
	DisplayName string                 `json:"display_name"`
	FieldType   taskdom.FieldType      `json:"field_type"`
	Options     []CustomFieldOptionDTO `json:"options"`
	IsRequired  bool                   `json:"is_required"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// customFieldOptionsToDomain maps request DTOs to domain options.
func customFieldOptionsToDomain(opts []CustomFieldOptionDTO) []taskdom.CustomFieldOption {
	if opts == nil {
		return nil
	}
	out := make([]taskdom.CustomFieldOption, len(opts))
	for i, o := range opts {
		out[i] = taskdom.CustomFieldOption{Value: o.Value, Color: o.Color}
	}
	return out
}

// CustomFieldOptionsToDomain maps the create request body's options to their
// domain representation.
func (r CreateCustomFieldDefinitionRequest) CustomFieldOptionsToDomain() []taskdom.CustomFieldOption {
	return customFieldOptionsToDomain(r.Options)
}

// CustomFieldOptionsToDomain maps the update request body's options to their
// domain representation.
func (r UpdateCustomFieldDefinitionRequest) CustomFieldOptionsToDomain() []taskdom.CustomFieldOption {
	return customFieldOptionsToDomain(r.Options)
}

// CustomFieldDefinitionFromEntity maps a domain CustomFieldDefinition to a DTO.
func CustomFieldDefinitionFromEntity(f *taskdom.CustomFieldDefinition) CustomFieldDefinitionResponse {
	opts := make([]CustomFieldOptionDTO, len(f.Options))
	for i, o := range f.Options {
		opts[i] = CustomFieldOptionDTO{Value: o.Value, Color: o.Color}
	}
	return CustomFieldDefinitionResponse{
		ID:          f.ID,
		ProjectID:   f.ProjectID,
		FieldKey:    f.FieldKey,
		DisplayName: f.DisplayName,
		FieldType:   f.FieldType,
		Options:     opts,
		IsRequired:  f.IsRequired,
		CreatedAt:   f.CreatedAt,
		UpdatedAt:   f.UpdatedAt,
	}
}

// --- Activity / Comment DTOs -----------------------------------------------

// ActivityResponse is the public representation of a task activity entry.
type ActivityResponse struct {
	ID            uuid.UUID  `json:"id"`
	TaskID        uuid.UUID  `json:"task_id"`
	ActorID       *uuid.UUID `json:"actor_id,omitempty"`
	ActorName     string     `json:"actor_name"`
	ActorUsername string     `json:"actor_username"`
	// ActorAvatarURL/ActorAvatarThumbURL are presigned GET URLs, populated by
	// the handler (not this mapper) via attachmentdom.AvatarService — nil
	// when the actor has no avatar.
	ActorAvatarURL      *string              `json:"actor_avatar_url,omitempty"`
	ActorAvatarThumbURL *string              `json:"actor_avatar_thumb_url,omitempty"`
	ActivityType        taskdom.ActivityType `json:"activity_type"`
	Content             json.RawMessage      `json:"content"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
}

// ActivityFromEntity maps a domain Activity to an ActivityResponse DTO.
func ActivityFromEntity(a *taskdom.Activity) ActivityResponse {
	content := a.Content
	if len(content) == 0 {
		content = json.RawMessage("{}")
	}
	return ActivityResponse{
		ID:            a.ID,
		TaskID:        a.TaskID,
		ActorID:       a.ActorID,
		ActorName:     a.ActorName,
		ActorUsername: a.ActorUsername,
		ActivityType:  a.ActivityType,
		Content:       content,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
	}
}

// AddCommentRequest is the body for
// POST /projects/:projectId/tasks/:taskId/activities/comments.
type AddCommentRequest struct {
	Content json.RawMessage `json:"content" binding:"required"`
}

// UpdateCommentRequest is the body for
// PATCH /projects/:projectId/tasks/:taskId/activities/comments/:commentId.
type UpdateCommentRequest struct {
	Content json.RawMessage `json:"content" binding:"required"`
}

// --- Task Link DTOs ---------------------------------------------------------

// CreateTaskLinkRequest is the body for
// POST /projects/:projectId/tasks/:taskId/links.
type CreateTaskLinkRequest struct {
	TargetTaskID uuid.UUID        `json:"target_task_id"`
	LinkType     taskdom.LinkType `json:"link_type"`
}

// LinkedTaskSummaryResponse is the embedded task info returned alongside a link.
type LinkedTaskSummaryResponse struct {
	ID         uuid.UUID  `json:"id"`
	TaskNumber int64      `json:"task_number"`
	Title      string     `json:"title"`
	StatusID   *uuid.UUID `json:"status_id,omitempty"`
	TaskTypeID *uuid.UUID `json:"task_type_id,omitempty"`
}

// TaskLinkResponse is the public representation of a task link.
// DisplayLinkType is the relationship label from the queried task's perspective
// (e.g. "is_blocked_by" when the queried task is the target of a "blocks" link).
type TaskLinkResponse struct {
	ID              uuid.UUID                 `json:"id"`
	SourceTaskID    uuid.UUID                 `json:"source_task_id"`
	TargetTaskID    uuid.UUID                 `json:"target_task_id"`
	LinkType        taskdom.LinkType          `json:"link_type"`
	DisplayLinkType string                    `json:"display_link_type"`
	LinkedTask      LinkedTaskSummaryResponse `json:"linked_task"`
	CreatedBy       *uuid.UUID                `json:"created_by,omitempty"`
	CreatedAt       time.Time                 `json:"created_at"`
}

// TaskLinkFromEntity maps a domain TaskLink to a TaskLinkResponse DTO.
func TaskLinkFromEntity(l *taskdom.TaskLink) TaskLinkResponse {
	resp := TaskLinkResponse{
		ID:              l.ID,
		SourceTaskID:    l.SourceTaskID,
		TargetTaskID:    l.TargetTaskID,
		LinkType:        l.LinkType,
		DisplayLinkType: l.DisplayLinkType,
		CreatedBy:       l.CreatedBy,
		CreatedAt:       l.CreatedAt,
	}
	if l.LinkedTask != nil {
		resp.LinkedTask = LinkedTaskSummaryResponse{
			ID:         l.LinkedTask.ID,
			TaskNumber: l.LinkedTask.TaskNumber,
			Title:      l.LinkedTask.Title,
			StatusID:   l.LinkedTask.StatusID,
			TaskTypeID: l.LinkedTask.TaskTypeID,
		}
	}
	return resp
}

package dto

import (
	"time"

	"github.com/google/uuid"

	annotationdom "github.com/Paca-AI/api/internal/domain/annotation"
)

// =========================================================================
// Page Annotation DTOs
// =========================================================================

// PageAnnotationResponse is the public view of a page annotation — a
// comment pinned to one element of one page, created via the Paca browser
// extension.
type PageAnnotationResponse struct {
	ID                uuid.UUID                     `json:"id"`
	ProjectID         uuid.UUID                     `json:"project_id"`
	EnvironmentID     uuid.UUID                     `json:"environment_id"`
	PortForwardID     uuid.UUID                     `json:"port_forward_id"`
	PagePath          string                        `json:"page_path"`
	ElementSelector   string                        `json:"element_selector"`
	SelectorFallbacks []string                      `json:"element_selector_fallbacks"`
	BoundingBox       annotationdom.BoundingBox     `json:"bounding_box"`
	ElementSnapshot   annotationdom.ElementSnapshot `json:"element_snapshot"`
	ConsoleErrors     []annotationdom.ConsoleEntry  `json:"console_errors"`
	FailedRequests    []annotationdom.FailedRequest `json:"failed_requests"`
	ScreenshotFileID  *uuid.UUID                    `json:"screenshot_file_id"`
	Body              string                        `json:"body"`
	Status            string                        `json:"status"`
	TaskID            *uuid.UUID                    `json:"task_id"`
	CreatedBy         uuid.UUID                     `json:"created_by"`
	CreatedByName     string                        `json:"created_by_name"`
	CreatedByUsername string                        `json:"created_by_username"`
	// CreatedByAvatarURL/CreatedByAvatarThumbURL are presigned display URLs,
	// resolved from the entity's own AvatarKey/AvatarThumbKey — filled in by
	// the handler (see AnnotationHandler.toAnnotationResponse), not by
	// PageAnnotationFromEntity itself, which stays pure/IO-free. Absent
	// (omitted, not null) when the user has no avatar set.
	CreatedByAvatarURL      *string                     `json:"created_by_avatar_url,omitempty"`
	CreatedByAvatarThumbURL *string                     `json:"created_by_avatar_thumb_url,omitempty"`
	ResolvedBy              *uuid.UUID                  `json:"resolved_by"`
	ResolvedAt              *time.Time                  `json:"resolved_at"`
	CreatedAt               time.Time                   `json:"created_at"`
	UpdatedAt               time.Time                   `json:"updated_at"`
	Comments                []AnnotationCommentResponse `json:"comments"`
}

// AnnotationCommentResponse is one reply in an annotation's thread.
type AnnotationCommentResponse struct {
	ID                      uuid.UUID `json:"id"`
	Body                    string    `json:"body"`
	CreatedBy               uuid.UUID `json:"created_by"`
	CreatedByName           string    `json:"created_by_name"`
	CreatedByUsername       string    `json:"created_by_username"`
	CreatedByAvatarURL      *string   `json:"created_by_avatar_url,omitempty"`
	CreatedByAvatarThumbURL *string   `json:"created_by_avatar_thumb_url,omitempty"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

// CreateAnnotationRequest is the body for POST
// .../port-forwards/:portForwardId/annotations. PortForwardID isn't a field
// here — it's the owning resource in the URL, not client-supplied payload.
type CreateAnnotationRequest struct {
	PagePath          string                        `json:"page_path" binding:"required"`
	ElementSelector   string                        `json:"element_selector" binding:"required"`
	SelectorFallbacks []string                      `json:"element_selector_fallbacks"`
	BoundingBox       annotationdom.BoundingBox     `json:"bounding_box"`
	ElementSnapshot   annotationdom.ElementSnapshot `json:"element_snapshot"`
	ConsoleErrors     []annotationdom.ConsoleEntry  `json:"console_errors"`
	FailedRequests    []annotationdom.FailedRequest `json:"failed_requests"`
	ScreenshotFileID  *uuid.UUID                    `json:"screenshot_file_id"`
	Body              string                        `json:"body" binding:"required"`
}

// AddAnnotationCommentRequest is the body for POST
// .../annotations/:annotationId/comments.
type AddAnnotationCommentRequest struct {
	Body string `json:"body" binding:"required"`
}

// CreateTaskFromAnnotationRequest is the body for POST
// .../annotations/:annotationId/create-task.
type CreateTaskFromAnnotationRequest struct {
	TaskTypeID  *uuid.UUID  `json:"task_type_id"`
	StatusID    *uuid.UUID  `json:"status_id"`
	AssigneeIDs []uuid.UUID `json:"assignee_ids"`
}

// InitiateScreenshotUploadRequest is the body for POST
// .../annotations/upload-url.
type InitiateScreenshotUploadRequest struct {
	FileName    string `json:"file_name" binding:"required"`
	ContentType string `json:"content_type" binding:"required"`
	FileSize    int64  `json:"file_size" binding:"required"`
}

// ScreenshotUploadSessionResponse is the body returned for POST
// .../annotations/upload-url.
type ScreenshotUploadSessionResponse struct {
	FileID    uuid.UUID `json:"file_id"`
	UploadURL string    `json:"upload_url"`
}

// CompleteScreenshotUploadRequest is the body for POST
// .../annotations/:annotationId/complete-upload.
type CompleteScreenshotUploadRequest struct {
	FileID uuid.UUID `json:"file_id" binding:"required"`
}

// ScreenshotURLResponse is the body returned for GET
// .../annotations/:annotationId/screenshot-url.
type ScreenshotURLResponse struct {
	URL string `json:"url"`
}

// ResolvePortForwardResponse is the body returned for GET
// /port-forwards/resolve.
type ResolvePortForwardResponse struct {
	ProjectID     uuid.UUID `json:"project_id"`
	EnvironmentID uuid.UUID `json:"environment_id"`
	PortForwardID uuid.UUID `json:"port_forward_id"`
	Label         string    `json:"label"`
}

// PageAnnotationFromEntity maps a PageAnnotation entity to its DTO. Pure —
// avatar URLs are left nil here; see AnnotationHandler.toAnnotationResponse
// for the presign step this needs a live request context for.
func PageAnnotationFromEntity(a *annotationdom.PageAnnotation) PageAnnotationResponse {
	comments := make([]AnnotationCommentResponse, 0, len(a.Comments))
	for _, c := range a.Comments {
		comments = append(comments, AnnotationCommentFromEntity(c))
	}
	return PageAnnotationResponse{
		ID:                a.ID,
		ProjectID:         a.ProjectID,
		EnvironmentID:     a.EnvironmentID,
		PortForwardID:     a.PortForwardID,
		PagePath:          a.PagePath,
		ElementSelector:   a.ElementSelector,
		SelectorFallbacks: a.SelectorFallbacks,
		BoundingBox:       a.BoundingBox,
		ElementSnapshot:   a.ElementSnapshot,
		ConsoleErrors:     a.ConsoleErrors,
		FailedRequests:    a.FailedRequests,
		ScreenshotFileID:  a.ScreenshotFileID,
		Body:              a.Body,
		Status:            a.Status,
		TaskID:            a.TaskID,
		CreatedBy:         a.CreatedBy,
		CreatedByName:     a.CreatedByAuthor.Name,
		CreatedByUsername: a.CreatedByAuthor.Username,
		ResolvedBy:        a.ResolvedBy,
		ResolvedAt:        a.ResolvedAt,
		CreatedAt:         a.CreatedAt,
		UpdatedAt:         a.UpdatedAt,
		Comments:          comments,
	}
}

// AnnotationCommentFromEntity maps an AnnotationComment entity to its DTO —
// same pure/no-avatar-URL contract as PageAnnotationFromEntity.
func AnnotationCommentFromEntity(c *annotationdom.AnnotationComment) AnnotationCommentResponse {
	return AnnotationCommentResponse{
		ID:                c.ID,
		Body:              c.Body,
		CreatedBy:         c.CreatedBy,
		CreatedByName:     c.CreatedByAuthor.Name,
		CreatedByUsername: c.CreatedByAuthor.Username,
		CreatedAt:         c.CreatedAt,
		UpdatedAt:         c.UpdatedAt,
	}
}

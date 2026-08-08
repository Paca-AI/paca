package dto

import "github.com/google/uuid"

// CompleteAvatarUploadRequest is the body for POST .../avatar/complete-upload.
// Unlike CompleteUploadRequest (task/doc attachments), avatars are always
// single-part (capped well under the multipart threshold), so there is no
// upload_id/parts to carry.
type CompleteAvatarUploadRequest struct {
	FileID uuid.UUID `json:"file_id" binding:"required"`
}

package dto

import (
	"time"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	"github.com/Paca-AI/api/internal/platform/storage"
)

// --- Requests ---------------------------------------------------------------

// InitiateUploadRequest is the body for POST .../attachments/initiate-upload.
type InitiateUploadRequest struct {
	FileName    string `json:"file_name"    binding:"required"`
	ContentType string `json:"content_type" binding:"required"`
	FileSize    int64  `json:"file_size"    binding:"required,min=1"`
}

// CompleteUploadRequest is the body for POST .../attachments/complete-upload.
type CompleteUploadRequest struct {
	FileID uuid.UUID `json:"file_id"   binding:"required"`
	// UploadID and Parts are required only for multipart uploads.
	UploadID *string                `json:"upload_id"`
	Parts    []CompletedPartRequest `json:"parts"`
}

// CompletedPartRequest represents one completed multipart upload part.
type CompletedPartRequest struct {
	PartNumber int    `json:"part_number" binding:"required,min=1"`
	ETag       string `json:"etag"        binding:"required"`
}

// --- Responses --------------------------------------------------------------

// PresignedPartResponse represents one presigned part URL.
type PresignedPartResponse struct {
	PartNumber int    `json:"part_number"`
	UploadURL  string `json:"upload_url"`
}

// MultipartUploadResponse carries the session details for a multipart upload.
type MultipartUploadResponse struct {
	UploadID string                  `json:"upload_id"`
	Parts    []PresignedPartResponse `json:"parts"`
}

// UploadSessionResponse is returned by POST .../initiate-upload.
type UploadSessionResponse struct {
	FileID      uuid.UUID                `json:"file_id"`
	IsMultipart bool                     `json:"is_multipart"`
	UploadURL   string                   `json:"upload_url,omitempty"`
	Multipart   *MultipartUploadResponse `json:"multipart,omitempty"`
}

// FileResponse carries file metadata in attachment responses.
type FileResponse struct {
	ID          uuid.UUID `json:"id"`
	FileName    string    `json:"file_name"`
	ContentType string    `json:"content_type"`
	FileSize    int64     `json:"file_size"`
	CreatedAt   time.Time `json:"created_at"`
}

// TaskAttachmentResponse is the list/create response for a task attachment.
type TaskAttachmentResponse struct {
	ID        uuid.UUID     `json:"id"`
	TaskID    uuid.UUID     `json:"task_id"`
	FileID    uuid.UUID     `json:"file_id"`
	CreatedBy *uuid.UUID    `json:"created_by,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	File      *FileResponse `json:"file,omitempty"`
}

// DownloadURLResponse wraps a short-lived presigned download URL.
type DownloadURLResponse struct {
	URL string `json:"url"`
}

// --- Converters -------------------------------------------------------------

// UploadSessionFromDomain maps an attachmentdom.UploadSession to a response DTO.
func UploadSessionFromDomain(s *attachmentdom.UploadSession) UploadSessionResponse {
	resp := UploadSessionResponse{
		FileID:      s.FileID,
		IsMultipart: s.IsMultipart,
		UploadURL:   s.UploadURL,
	}
	if s.Multipart != nil {
		resp.Multipart = multipartFromDomain(s.Multipart)
	}
	return resp
}

func multipartFromDomain(mu *storage.MultipartUpload) *MultipartUploadResponse {
	parts := make([]PresignedPartResponse, 0, len(mu.Parts))
	for _, p := range mu.Parts {
		parts = append(parts, PresignedPartResponse{
			PartNumber: p.PartNumber,
			UploadURL:  p.UploadURL,
		})
	}
	return &MultipartUploadResponse{UploadID: mu.UploadID, Parts: parts}
}

// TaskAttachmentFromEntity maps an attachmentdom.TaskAttachment to a response DTO.
func TaskAttachmentFromEntity(a *attachmentdom.TaskAttachment) TaskAttachmentResponse {
	resp := TaskAttachmentResponse{
		ID:        a.ID,
		TaskID:    a.TaskID,
		FileID:    a.FileID,
		CreatedBy: a.CreatedBy,
		CreatedAt: a.CreatedAt,
	}
	if a.File != nil {
		f := fileFromEntity(a.File)
		resp.File = &f
	}
	return resp
}

// FileFromEntity maps an attachmentdom.File to a FileResponse DTO.
func FileFromEntity(f *attachmentdom.File) FileResponse {
	return fileFromEntity(f)
}

func fileFromEntity(f *attachmentdom.File) FileResponse {
	return FileResponse{
		ID:          f.ID,
		FileName:    f.FileName,
		ContentType: f.ContentType,
		FileSize:    f.FileSize,
		CreatedAt:   f.CreatedAt,
	}
}

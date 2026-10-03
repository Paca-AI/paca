package dto

import (
	"time"

	"github.com/google/uuid"

	exportdom "github.com/Paca-AI/api/internal/domain/export"
)

// ProjectExportResponse is one project export and, once completed, the
// metadata of its file. The file itself is fetched through the separate
// download endpoint, which mints a short-lived URL.
type ProjectExportResponse struct {
	ID           uuid.UUID  `json:"id"`
	ProjectID    uuid.UUID  `json:"project_id"`
	RequestedBy  *uuid.UUID `json:"requested_by,omitempty"`
	Kind         string     `json:"kind"`
	Status       string     `json:"status"`
	FileName     *string    `json:"file_name,omitempty"`
	FileSize     *int64     `json:"file_size,omitempty"`
	RowCount     *int       `json:"row_count,omitempty"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	// Expired is true once the file is past its retention window.
	Expired bool `json:"expired"`
}

// ProjectExportFromEntity maps a domain export to its response DTO. The
// storage key is deliberately not exposed.
func ProjectExportFromEntity(e *exportdom.ProjectExport, now time.Time) ProjectExportResponse {
	return ProjectExportResponse{
		ID:           e.ID,
		ProjectID:    e.ProjectID,
		RequestedBy:  e.RequestedBy,
		Kind:         string(e.Kind),
		Status:       string(e.Status),
		FileName:     e.FileName,
		FileSize:     e.FileSize,
		RowCount:     e.RowCount,
		ErrorMessage: e.ErrorMessage,
		CreatedAt:    e.CreatedAt,
		CompletedAt:  e.CompletedAt,
		ExpiresAt:    e.ExpiresAt,
		Expired:      e.Expired(now),
	}
}

// ProjectExportDownloadResponse carries a presigned download URL.
type ProjectExportDownloadResponse struct {
	URL string `json:"url"`
	// ExpiresInSeconds is how long URL stays valid.
	ExpiresInSeconds int `json:"expires_in_seconds"`
}

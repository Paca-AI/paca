// Package exportdom defines the project export aggregate: a request to dump
// part of a project's data into a downloadable file, produced asynchronously.
package exportdom

import (
	"time"

	"github.com/google/uuid"
)

// Kind identifies what an export contains.
type Kind string

// Kind values.
const (
	// KindProjectArchive exports the project as one zip: tasks, their comments
	// and activities as CSV files, and the documentation as Markdown files.
	KindProjectArchive Kind = "project_archive"
)

// Status is the lifecycle stage of an export.
type Status string

// Status values. An export moves pending -> processing -> completed|failed
// and never leaves a terminal state.
const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
)

// Active reports whether the export is still queued or running.
func (s Status) Active() bool {
	return s == StatusPending || s == StatusProcessing
}

// RetentionPeriod is how long a completed export's file stays downloadable
// before it is removed from object storage.
const RetentionPeriod = 7 * 24 * time.Hour

// StaleAfter is how long an export may sit in an active state before it is
// treated as abandoned (the worker crashed mid-run, or the stream message was
// lost) so it no longer blocks a new request for the project.
const StaleAfter = 30 * time.Minute

// ProjectExport is one export request and, once finished, its stored file.
type ProjectExport struct {
	ID          uuid.UUID
	ProjectID   uuid.UUID
	RequestedBy *uuid.UUID
	Kind        Kind
	Status      Status
	// FileKey is the object-storage key of the generated file; set only once
	// the export is completed.
	FileKey      *string
	FileName     *string
	FileSize     *int64
	RowCount     *int
	ErrorMessage *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CompletedAt  *time.Time
	ExpiresAt    *time.Time
}

// Expired reports whether the export's file is past its retention window.
func (e *ProjectExport) Expired(now time.Time) bool {
	return e.ExpiresAt != nil && !now.Before(*e.ExpiresAt)
}

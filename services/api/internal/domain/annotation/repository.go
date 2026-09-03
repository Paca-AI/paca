package annotationdom

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Repository is the storage contract for the PageAnnotation aggregate.
type Repository interface {
	// ListForPage returns every non-deleted annotation for portForwardID
	// whose PagePath equals pagePath, oldest first, each with Comments
	// populated — the extension's own hot path, called on every preview
	// page load.
	ListForPage(ctx context.Context, portForwardID uuid.UUID, pagePath string) ([]*PageAnnotation, error)
	// ListForPortForward returns every non-deleted annotation for
	// portForwardID across all pages it serves, newest first — backs the
	// web app's port-forward-scoped Comments tab.
	ListForPortForward(ctx context.Context, portForwardID uuid.UUID) ([]*PageAnnotation, error)
	// FindVisibleInProject returns a single annotation by ID, but only if
	// it belongs to projectID — mirrors
	// environmentdom.Repository.FindVisibleEnvironmentInProject. Returns
	// ErrAnnotationNotFound otherwise.
	FindVisibleInProject(ctx context.Context, projectID, annotationID uuid.UUID) (*PageAnnotation, error)
	// SearchInProject returns every annotation visible in projectID matching
	// filter, newest first, cursor-paginated when filter.Limit is set —
	// backs the project-wide search used by BlockNote mentions, the agent
	// conversation's attach-context picker, and the MCP list_annotations
	// tool, none of which are scoped to one specific port forward the way
	// ListForPage/ListForPortForward are. hasMore is only meaningful when
	// filter.Limit is set.
	SearchInProject(ctx context.Context, projectID uuid.UUID, filter SearchFilter) (results []*PageAnnotation, hasMore bool, err error)
	Create(ctx context.Context, a *PageAnnotation) error
	// SetScreenshotFileID attaches (or replaces) an already-uploaded
	// screenshot on an existing annotation — used by
	// Service.CompleteScreenshotUpload for the "attach a screenshot after
	// the comment already exists" path; Service.Create sets it inline for
	// the common "screenshot uploaded before the comment was submitted"
	// path instead.
	SetScreenshotFileID(ctx context.Context, id, fileID uuid.UUID) error
	SetStatus(ctx context.Context, id uuid.UUID, status string, resolvedBy *uuid.UUID, resolvedAt *time.Time) error
	SetTaskID(ctx context.Context, id, taskID uuid.UUID) error
	AddComment(ctx context.Context, c *AnnotationComment) error

	// CreatePendingScreenshotFile inserts a pending row into the shared
	// files table (the same table attachmentdom.File rows live in — see
	// that type for the full column set) under a
	// "annotations/{fileID}/{fileName}" storage key, before an annotation
	// exists to reference it. Returns the presigned-upload-ready file ID.
	CreatePendingScreenshotFile(ctx context.Context, fileID, uploadedBy uuid.UUID, storageKey, bucket, fileName, contentType string, fileSize int64) error
	// MarkScreenshotFileUploaded flips a pending screenshot file's
	// upload_status to "uploaded" — called from Create once the caller has
	// actually PUT the bytes to the presigned URL InitiateScreenshotUpload
	// returned. A no-op (not an error) if fileID doesn't exist or isn't
	// pending, since a caller that never attached a screenshot passes no
	// fileID at all — this only ever runs when one was supplied.
	MarkScreenshotFileUploaded(ctx context.Context, fileID uuid.UUID) error

	// ResolvePortForward looks up which project/environment/port-forward
	// currently owns hostPort, scoped to projects userID is a member of —
	// environment_port_forwards.host_port has a whole-table unique index,
	// so at most one row can match at any given moment. Returns
	// ErrPortForwardNotFound if none does (including "it exists, but not
	// in a project this user can see" — deliberately indistinguishable
	// from the general not-found case, so this endpoint never confirms or
	// denies the existence of a port forward in a project the caller isn't
	// a member of).
	ResolvePortForward(ctx context.Context, userID uuid.UUID, hostPort int) (*PortForwardMatch, error)
}

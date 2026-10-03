package exportdom

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Service is the API-facing project export contract. Running an export is the
// worker's job (see service/export.Service.Execute) and deliberately not part
// of this interface.
type Service interface {
	// RequestExport queues an export of the project (tasks, task comments and
	// activities, and documentation) as a zip.
	// Returns an apierr.CodeProjectExportInProgress error when one is already
	// queued or running.
	RequestExport(ctx context.Context, projectID, requestedBy uuid.UUID) (*ProjectExport, error)
	// List returns the project's recent exports, newest first.
	List(ctx context.Context, projectID uuid.UUID) ([]*ProjectExport, error)
	// Get returns one export of the project.
	Get(ctx context.Context, projectID, exportID uuid.UUID) (*ProjectExport, error)
	// DownloadURL returns a short-lived presigned URL for a completed,
	// unexpired export's file along with how long it stays valid.
	DownloadURL(ctx context.Context, projectID, exportID uuid.UUID) (string, time.Duration, error)
}

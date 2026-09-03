package annotationdom

import (
	"context"

	"github.com/google/uuid"
)

// Service is the combined PageAnnotation service contract. Concrete task-
// creation/screenshot-storage mechanics are deliberately kept out of this
// interface (see CreateTaskFromAnnotationInput/InitiateScreenshotUploadInput
// below) — the concrete implementation in internal/service/annotation
// depends on taskdom/attachmentdom/storage directly, the same way
// environmentsvc.Service depends on secret.Encryptor without that leaking
// into environmentdom.Service's own interface.
type Service interface {
	// ListForPage returns every annotation (open and resolved alike) for
	// the given page — callers filter resolved ones client-side, so a
	// "show resolved" toggle never needs a second round trip.
	ListForPage(ctx context.Context, projectID, environmentID, portForwardID uuid.UUID, pagePath string) ([]*PageAnnotation, error)
	// ListForPortForward returns every annotation across every page this
	// port forward serves — backs the web app's Comments tab on the port
	// forward detail page.
	ListForPortForward(ctx context.Context, projectID, environmentID, portForwardID uuid.UUID) ([]*PageAnnotation, error)
	Create(ctx context.Context, projectID, environmentID, portForwardID uuid.UUID, in CreateAnnotationInput) (*PageAnnotation, error)
	// Get returns a single annotation by ID, scoped only to projectID —
	// backs the web app's comment detail page. Deliberately takes no
	// environment/port-forward ID, mirroring how Resolve/Reopen/AddComment/
	// CreateTaskFromAnnotation/CompleteScreenshotUpload/GetScreenshotURL
	// below already scope by project+annotation alone.
	Get(ctx context.Context, projectID, annotationID uuid.UUID) (*PageAnnotation, error)
	// SearchInProject returns every annotation visible in projectID matching
	// filter — see Repository.SearchInProject's own doc comment for why this
	// exists alongside ListForPage/ListForPortForward.
	SearchInProject(ctx context.Context, projectID uuid.UUID, filter SearchFilter) ([]*PageAnnotation, bool, error)
	Resolve(ctx context.Context, projectID, annotationID, resolvedBy uuid.UUID) (*PageAnnotation, error)
	Reopen(ctx context.Context, projectID, annotationID uuid.UUID) (*PageAnnotation, error)
	AddComment(ctx context.Context, projectID, annotationID, createdBy uuid.UUID, body string) (*AnnotationComment, error)
	// CreateTaskFromAnnotation creates a real task pre-filled from the
	// annotation's comment + captured context, links the annotation's
	// already-uploaded screenshot into the new task's attachments without
	// re-uploading it, and sets PageAnnotation.TaskID. Returns
	// ErrAnnotationAlreadyHasTask if one already exists.
	CreateTaskFromAnnotation(ctx context.Context, projectID, annotationID uuid.UUID, in CreateTaskFromAnnotationInput) (*PageAnnotation, error)

	// InitiateScreenshotUpload returns a presigned PUT URL for an
	// annotation's screenshot, called before Create (the file must exist
	// before an annotation row can reference it).
	InitiateScreenshotUpload(ctx context.Context, projectID, environmentID, portForwardID uuid.UUID, in InitiateScreenshotUploadInput) (*ScreenshotUploadSession, error)
	// CompleteScreenshotUpload marks the presigned upload as finished and
	// links the resulting file to annotationID.
	CompleteScreenshotUpload(ctx context.Context, projectID, annotationID, fileID uuid.UUID) (*PageAnnotation, error)

	// ResolvePortForward turns a raw host port into the project/
	// environment/port-forward it currently belongs to, scoped to
	// projects userID is a member of — see Repository.ResolvePortForward.
	ResolvePortForward(ctx context.Context, userID uuid.UUID, hostPort int) (*PortForwardMatch, error)

	// GetScreenshotURL returns a short-lived presigned GET URL for
	// annotationID's screenshot. Returns ErrAnnotationScreenshotNotUploaded
	// if the annotation has no screenshot attached.
	GetScreenshotURL(ctx context.Context, projectID, annotationID uuid.UUID) (string, error)
}

// CreateAnnotationInput carries fields required to create a page
// annotation. All context fields (BoundingBox/ElementSnapshot/
// ConsoleErrors/FailedRequests) are captured client-side by the extension
// at comment time — the server stores them opaquely, sanitization of
// OuterHTMLExcerpt already having happened before it ever reaches here
// (see ElementSnapshot's doc comment).
type CreateAnnotationInput struct {
	PagePath          string
	ElementSelector   string
	SelectorFallbacks []string
	BoundingBox       BoundingBox
	ElementSnapshot   ElementSnapshot
	ConsoleErrors     []ConsoleEntry
	FailedRequests    []FailedRequest
	ScreenshotFileID  *uuid.UUID
	Body              string
	CreatedBy         uuid.UUID
}

// SearchFilter narrows a SearchInProject call — every field is optional
// (nil/empty means "don't filter on this"), mirroring the pointer-optional
// style task/doc list filters already use in this codebase. Limit being nil
// means "return everything, no pagination" (used internally where a full
// unpaginated project scan is fine); a non-nil Limit switches on
// cursor-based pagination and hasMore reporting.
type SearchFilter struct {
	EnvironmentID *uuid.UUID
	PortForwardID *uuid.UUID
	Status        *string
	Search        *string
	Cursor        *string
	Limit         *int
}

// CreateTaskFromAnnotationInput carries the fields a task-creation form
// must supply that an annotation alone doesn't imply — mirroring what the
// normal task-creation flow already requires a user to pick (status/type),
// since task.Service.CreateTask has no default-resolution for either.
type CreateTaskFromAnnotationInput struct {
	TaskTypeID  *uuid.UUID
	StatusID    *uuid.UUID
	AssigneeIDs []uuid.UUID
	ReporterID  uuid.UUID
}

// InitiateScreenshotUploadInput carries the client-supplied file metadata
// for starting a screenshot upload — mirrors
// attachmentdom.InitiateUploadInput, scoped to an environment (not yet an
// annotation, which doesn't exist until Create) instead of a task.
type InitiateScreenshotUploadInput struct {
	FileName    string
	ContentType string
	FileSize    int64
	UploadedBy  uuid.UUID
}

// ScreenshotUploadSession is returned by InitiateScreenshotUpload and
// carries everything the extension needs to upload the screenshot
// directly to the object store. Screenshots captured via
// chrome.tabs.captureVisibleTab are always well under the single-part
// threshold, so unlike attachmentdom.UploadSession this has no multipart
// variant.
type ScreenshotUploadSession struct {
	FileID    uuid.UUID `json:"file_id"`
	UploadURL string    `json:"upload_url"`
}

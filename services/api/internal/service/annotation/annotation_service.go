// Package annotationsvc implements the PageAnnotation application service —
// the use-case layer behind the Paca browser extension's on-page comments
// (apps/extension). Orchestrates across the annotation, environment, task,
// and attachment domains the same way environmentsvc.Service orchestrates
// across environment/agent-runner: this package is where those
// cross-domain dependencies live, deliberately kept out of
// annotationdom.Service's own interface.
package annotationsvc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	annotationdom "github.com/Paca-AI/api/internal/domain/annotation"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	environmentdom "github.com/Paca-AI/api/internal/domain/environment"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// presignedUploadTTL bounds how long a screenshot upload URL stays valid —
// mirrors attachmentsvc's own presign TTL: long enough for a browser to
// finish a same-request PUT, short enough that a leaked URL is useless
// shortly after.
const presignedUploadTTL = 15 * time.Minute

// maxTaskTitleLength caps the title derived from a comment body — Task
// titles are meant to be scannable on a board, not a full paragraph.
const maxTaskTitleLength = 120

// ObjectStore is the minimal presigned-URL surface this service needs from
// the object store — *storage.S3Client satisfies this directly.
type ObjectStore interface {
	PresignPutObject(ctx context.Context, bucket, key, contentType string, ttl time.Duration) (string, error)
	PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, contentDisposition string) (string, error)
}

// TaskCreator is the minimal task.Service surface CreateTaskFromAnnotation
// needs — *tasksvc.Service (or its cached wrapper) satisfies this
// directly.
type TaskCreator interface {
	CreateTask(ctx context.Context, in taskdom.CreateTaskInput) (*taskdom.Task, error)
}

// TaskAttachmentLinker is the minimal attachment-repository surface needed
// to link an already-uploaded screenshot file into a newly created task's
// own attachments, without re-uploading it.
type TaskAttachmentLinker interface {
	CreateTaskAttachment(ctx context.Context, a *attachmentdom.TaskAttachment) error
}

// FileFinder is the minimal attachment-repository surface needed to look
// up an annotation's own screenshot file (stored in the same shared files
// table attachmentdom.File rows live in) for GetScreenshotURL — reusing
// attachmentRepo rather than duplicating a second file-lookup path.
type FileFinder interface {
	FindFileByID(ctx context.Context, id uuid.UUID) (*attachmentdom.File, error)
}

// screenshotURLTTL bounds how long a screenshot's presigned GET URL stays
// valid — long enough for a browser to load the image, short enough that
// a leaked URL (e.g. cached in a proxy log) is useless shortly after.
const screenshotURLTTL = 15 * time.Minute

// annotationScreenshotPrefix is the storage-key prefix every screenshot
// uploaded via InitiateScreenshotUpload gets (see that method) — screenshots
// live in the same shared files table as task attachments, docs, and
// avatars, each under their own prefix.
const annotationScreenshotPrefix = "annotations/"

// isAnnotationScreenshotFile reports whether f's storage key marks it as one
// of an annotation's own screenshots, as opposed to a task attachment, doc,
// or avatar — each of which lives under its own prefix in the same shared
// files table. Unlike uploader identity (see verifyAnnotationScreenshotFile
// below), this is an invariant of the file itself: still true no matter
// which project member with annotations.write ends up being the one to
// attach a given upload to an annotation.
func isAnnotationScreenshotFile(f *attachmentdom.File) bool {
	return strings.HasPrefix(f.StorageKey, annotationScreenshotPrefix)
}

// verifyAnnotationScreenshotFile confirms f is actually a screenshot
// uploadedBy themselves uploaded through InitiateScreenshotUpload — guards
// Create and CompleteScreenshotUpload against a caller passing an arbitrary
// files.id as screenshot_file_id (a task attachment, doc, avatar, or a
// screenshot someone else uploaded that they merely know the ID of) and
// having it silently accepted. The storage key alone can't disambiguate by
// annotation (InitiateScreenshotUpload runs before the annotation exists,
// so it can't be encoded in the key the way docs/{docId}/... is), so
// uploader identity is the check instead — mirrors avatar_service.go's own
// prefix+owner check at CompleteAvatarUpload time.
//
// Deliberately not reused as-is by GetScreenshotURL: annotations.write lets
// any project member attach a screenshot to any annotation (not just their
// own — see the "Resolve is a separate permission tier" reasoning in
// router.go), so a legitimately-attached screenshot's uploader routinely
// differs from the annotation's own CreatedBy. GetScreenshotURL instead
// reuses just isAnnotationScreenshotFile, the one part of this check that's
// still true regardless of who did the attaching.
func verifyAnnotationScreenshotFile(f *attachmentdom.File, uploadedBy uuid.UUID) error {
	if !isAnnotationScreenshotFile(f) {
		return annotationdom.ErrAnnotationScreenshotMismatch
	}
	if f.UploadedBy == nil || *f.UploadedBy != uploadedBy {
		return annotationdom.ErrAnnotationScreenshotMismatch
	}
	return nil
}

// Service is the concrete PageAnnotation service.
type Service struct {
	repo      annotationdom.Repository
	envSvc    environmentdom.Service
	tasks     TaskCreator
	attach    TaskAttachmentLinker
	files     FileFinder
	store     ObjectStore
	bucket    string
	publicURL string
}

// New returns a configured annotation Service. envSvc is used only to
// verify an environmentID belongs to a projectID before any read/write —
// the same ownership check environmentsvc.Service.GetEnvironment already
// performs, reused here rather than duplicated. files/store are typically
// the same *postgres.AttachmentRepository and *storage.S3Client instances
// already wired for the attachment domain, not new ones.
func New(repo annotationdom.Repository, envSvc environmentdom.Service, tasks TaskCreator, attach TaskAttachmentLinker, files FileFinder, store ObjectStore, bucket string) *Service {
	return &Service{repo: repo, envSvc: envSvc, tasks: tasks, attach: attach, files: files, store: store, bucket: bucket}
}

// WithPublicURL attaches the workspace's externally reachable base URL,
// used to build the comment link CreateTaskFromAnnotation writes into the
// new task's description (see annotationURL/buildTaskDescription). Mirrors
// notificationsvc.Svc.WithEventPublishing's own publicURL wiring.
func (s *Service) WithPublicURL(publicURL string) *Service {
	s.publicURL = publicURL
	return s
}

// annotationURL builds a's own canonical comment-detail-page link — the
// exact URL apps/web's Copy button (comment-detail-view.tsx) and the
// extension's pin popover already copy, and that apps/web's BlockNote load
// path (lib/annotation-link.ts's matchAnnotationLink) already knows how to
// recognize.
func (s *Service) annotationURL(a *annotationdom.PageAnnotation) string {
	return fmt.Sprintf("%s/projects/%s/environments/%s/port-forwards/%s/comments/%s",
		strings.TrimRight(s.publicURL, "/"), a.ProjectID, a.EnvironmentID, a.PortForwardID, a.ID)
}

// checkPortForward verifies portForwardID belongs to environmentID which
// belongs to projectID — the ownership gate for every annotation
// list/create operation, since a comment now belongs to a specific port
// forward, not the environment as a whole.
func (s *Service) checkPortForward(ctx context.Context, projectID, environmentID, portForwardID uuid.UUID) error {
	_, err := s.envSvc.GetPortForward(ctx, projectID, environmentID, portForwardID)
	return err
}

// ListForPage returns every annotation on pagePath within portForwardID.
func (s *Service) ListForPage(ctx context.Context, projectID, environmentID, portForwardID uuid.UUID, pagePath string) ([]*annotationdom.PageAnnotation, error) {
	if err := s.checkPortForward(ctx, projectID, environmentID, portForwardID); err != nil {
		return nil, err
	}
	return s.repo.ListForPage(ctx, portForwardID, pagePath)
}

// ListForPortForward returns every annotation across every page portForwardID
// serves.
func (s *Service) ListForPortForward(ctx context.Context, projectID, environmentID, portForwardID uuid.UUID) ([]*annotationdom.PageAnnotation, error) {
	if err := s.checkPortForward(ctx, projectID, environmentID, portForwardID); err != nil {
		return nil, err
	}
	return s.repo.ListForPortForward(ctx, portForwardID)
}

// Get returns annotationID if it belongs to projectID.
func (s *Service) Get(ctx context.Context, projectID, annotationID uuid.UUID) (*annotationdom.PageAnnotation, error) {
	return s.repo.FindVisibleInProject(ctx, projectID, annotationID)
}

// SearchInProject returns annotations across the whole project matching
// filter, project-wide (not scoped to one port forward) — see
// annotationdom.SearchFilter.
func (s *Service) SearchInProject(ctx context.Context, projectID uuid.UUID, filter annotationdom.SearchFilter) ([]*annotationdom.PageAnnotation, bool, error) {
	return s.repo.SearchInProject(ctx, projectID, filter)
}

// Create adds a new page annotation.
func (s *Service) Create(ctx context.Context, projectID, environmentID, portForwardID uuid.UUID, in annotationdom.CreateAnnotationInput) (*annotationdom.PageAnnotation, error) {
	if err := s.checkPortForward(ctx, projectID, environmentID, portForwardID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Body) == "" {
		return nil, annotationdom.ErrAnnotationBodyEmpty
	}

	if in.ScreenshotFileID != nil {
		// Reject outright (not best-effort) if this isn't a screenshot
		// in.CreatedBy themselves uploaded via InitiateScreenshotUpload — an
		// arbitrary files.id here (a task attachment, doc, avatar, or
		// another user's screenshot) would otherwise get silently attached
		// and later handed back as a presigned URL by GetScreenshotURL. See
		// verifyAnnotationScreenshotFile's own doc comment.
		f, err := s.files.FindFileByID(ctx, *in.ScreenshotFileID)
		if err != nil {
			return nil, fmt.Errorf("annotation svc: find screenshot file: %w", err)
		}
		if err := verifyAnnotationScreenshotFile(f, in.CreatedBy); err != nil {
			return nil, err
		}
		// Best-effort from here — a screenshot that fails to mark "uploaded"
		// (e.g. the client's PUT never actually landed) shouldn't block the
		// comment itself from being saved; the pin just ends up without a
		// visible screenshot, same as if none had been attached.
		_ = s.repo.MarkScreenshotFileUploaded(ctx, *in.ScreenshotFileID)
	}

	now := time.Now()
	a := &annotationdom.PageAnnotation{
		ID:                uuid.New(),
		ProjectID:         projectID,
		EnvironmentID:     environmentID,
		PortForwardID:     portForwardID,
		PagePath:          in.PagePath,
		ElementSelector:   in.ElementSelector,
		SelectorFallbacks: in.SelectorFallbacks,
		BoundingBox:       in.BoundingBox,
		ElementSnapshot:   in.ElementSnapshot,
		ConsoleErrors:     in.ConsoleErrors,
		FailedRequests:    in.FailedRequests,
		ScreenshotFileID:  in.ScreenshotFileID,
		Body:              in.Body,
		Status:            annotationdom.StatusOpen,
		CreatedBy:         in.CreatedBy,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if a.SelectorFallbacks == nil {
		a.SelectorFallbacks = []string{}
	}
	if a.ConsoleErrors == nil {
		a.ConsoleErrors = []annotationdom.ConsoleEntry{}
	}
	if a.FailedRequests == nil {
		a.FailedRequests = []annotationdom.FailedRequest{}
	}

	if err := s.repo.Create(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

// Resolve marks an annotation resolved.
func (s *Service) Resolve(ctx context.Context, projectID, annotationID, resolvedBy uuid.UUID) (*annotationdom.PageAnnotation, error) {
	if _, err := s.repo.FindVisibleInProject(ctx, projectID, annotationID); err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.repo.SetStatus(ctx, annotationID, annotationdom.StatusResolved, &resolvedBy, &now); err != nil {
		return nil, err
	}
	return s.repo.FindVisibleInProject(ctx, projectID, annotationID)
}

// Reopen moves a resolved annotation back to open.
func (s *Service) Reopen(ctx context.Context, projectID, annotationID uuid.UUID) (*annotationdom.PageAnnotation, error) {
	if _, err := s.repo.FindVisibleInProject(ctx, projectID, annotationID); err != nil {
		return nil, err
	}
	if err := s.repo.SetStatus(ctx, annotationID, annotationdom.StatusOpen, nil, nil); err != nil {
		return nil, err
	}
	return s.repo.FindVisibleInProject(ctx, projectID, annotationID)
}

// AddComment appends a reply to an annotation's thread.
func (s *Service) AddComment(ctx context.Context, projectID, annotationID, createdBy uuid.UUID, body string) (*annotationdom.AnnotationComment, error) {
	if strings.TrimSpace(body) == "" {
		return nil, annotationdom.ErrCommentBodyEmpty
	}
	if _, err := s.repo.FindVisibleInProject(ctx, projectID, annotationID); err != nil {
		return nil, err
	}
	now := time.Now()
	c := &annotationdom.AnnotationComment{
		ID:           uuid.New(),
		AnnotationID: annotationID,
		Body:         body,
		CreatedBy:    createdBy,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repo.AddComment(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// CreateTaskFromAnnotation creates a task pre-filled from an annotation's
// canonical link (see buildTaskDescription) and links the two together —
// errors if the annotation already has one.
func (s *Service) CreateTaskFromAnnotation(ctx context.Context, projectID, annotationID uuid.UUID, in annotationdom.CreateTaskFromAnnotationInput) (*annotationdom.PageAnnotation, error) {
	a, err := s.repo.FindVisibleInProject(ctx, projectID, annotationID)
	if err != nil {
		return nil, err
	}
	if a.TaskID != nil {
		return nil, annotationdom.ErrAnnotationAlreadyHasTask
	}

	// Claim before calling out to task creation — see
	// annotationdom.Repository.ClaimTaskCreation's doc comment. This is
	// what makes it safe for a client to retry this call after a timeout:
	// without it, a retry landing after CreateTask below already succeeded
	// but before SetTaskID recorded that fact would re-enter this method
	// with a.TaskID still nil and create a second task.
	claimed, err := s.repo.ClaimTaskCreation(ctx, annotationID)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, annotationdom.ErrAnnotationTaskCreationInProgress
	}

	task, err := s.tasks.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID:    projectID,
		TaskTypeID:   in.TaskTypeID,
		StatusID:     in.StatusID,
		Title:        taskTitleFromBody(a.Body),
		Description:  buildTaskDescription(s.annotationURL(a)),
		Importance:   0,
		AssigneeIDs:  in.AssigneeIDs,
		ReporterID:   &in.ReporterID,
		CustomFields: map[string]any{},
		Tags:         []string{},
	})
	if err != nil {
		return nil, fmt.Errorf("annotation svc: create task: %w", err)
	}

	if a.ScreenshotFileID != nil {
		if err := s.attach.CreateTaskAttachment(ctx, &attachmentdom.TaskAttachment{
			ID:        uuid.New(),
			TaskID:    task.ID,
			FileID:    *a.ScreenshotFileID,
			CreatedBy: &in.ReporterID,
			CreatedAt: time.Now(),
		}); err != nil {
			// The task already exists at this point — a failed screenshot
			// link shouldn't roll that back or fail the whole action; the
			// task is just missing the screenshot a user can still attach
			// manually.
			_ = err
		}
	}

	// The task now exists — a handful of in-process retries on the last,
	// otherwise-transient step (a single UPDATE on a row we just read)
	// meaningfully shrinks the window in which a client-side timeout here
	// would leave task_id unset (the claim above, not this, is what
	// actually prevents a duplicate task on such a retry).
	if err := s.setTaskIDWithRetry(ctx, annotationID, task.ID); err != nil {
		return nil, fmt.Errorf("annotation svc: link created task %s to annotation: %w", task.ID, err)
	}
	return s.repo.FindVisibleInProject(ctx, projectID, annotationID)
}

// setTaskIDCreationLinkRetries and setTaskIDRetryDelay bound the
// in-process retry described in CreateTaskFromAnnotation above.
const (
	setTaskIDCreationLinkRetries = 3
	setTaskIDRetryDelay          = 200 * time.Millisecond
)

func (s *Service) setTaskIDWithRetry(ctx context.Context, annotationID, taskID uuid.UUID) error {
	var err error
	for attempt := 0; attempt < setTaskIDCreationLinkRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(setTaskIDRetryDelay):
			}
		}
		if err = s.repo.SetTaskID(ctx, annotationID, taskID); err == nil {
			return nil
		}
	}
	return err
}

// InitiateScreenshotUpload creates a pending file record and returns a
// presigned URL the client can PUT the screenshot bytes to directly.
func (s *Service) InitiateScreenshotUpload(ctx context.Context, projectID, environmentID, portForwardID uuid.UUID, in annotationdom.InitiateScreenshotUploadInput) (*annotationdom.ScreenshotUploadSession, error) {
	if err := s.checkPortForward(ctx, projectID, environmentID, portForwardID); err != nil {
		return nil, err
	}
	fileID := uuid.New()
	storageKey := annotationScreenshotPrefix + fileID.String() + "/" + sanitizeFileName(in.FileName)

	if err := s.repo.CreatePendingScreenshotFile(ctx, fileID, in.UploadedBy, storageKey, s.bucket, in.FileName, in.ContentType, in.FileSize); err != nil {
		return nil, err
	}
	uploadURL, err := s.store.PresignPutObject(ctx, s.bucket, storageKey, in.ContentType, presignedUploadTTL)
	if err != nil {
		return nil, fmt.Errorf("annotation svc: presign screenshot upload: %w", err)
	}
	return &annotationdom.ScreenshotUploadSession{FileID: fileID, UploadURL: uploadURL}, nil
}

// CompleteScreenshotUpload attaches an already-uploaded screenshot to an
// existing annotation — for the "add/replace a screenshot after the
// comment was already submitted" path. The common path (screenshot
// captured and uploaded in the same flow as the comment itself) instead
// passes ScreenshotFileID directly to Create, which finalizes it inline.
// completedBy must match the file's own uploader (see
// verifyAnnotationScreenshotFile) — rejects a caller passing someone else's
// (or an unrelated) files.id here.
func (s *Service) CompleteScreenshotUpload(ctx context.Context, projectID, annotationID, fileID, completedBy uuid.UUID) (*annotationdom.PageAnnotation, error) {
	if _, err := s.repo.FindVisibleInProject(ctx, projectID, annotationID); err != nil {
		return nil, err
	}
	f, err := s.files.FindFileByID(ctx, fileID)
	if err != nil {
		return nil, fmt.Errorf("annotation svc: find screenshot file: %w", err)
	}
	if err := verifyAnnotationScreenshotFile(f, completedBy); err != nil {
		return nil, err
	}
	if err := s.repo.MarkScreenshotFileUploaded(ctx, fileID); err != nil {
		return nil, err
	}
	if err := s.repo.SetScreenshotFileID(ctx, annotationID, fileID); err != nil {
		return nil, err
	}
	return s.repo.FindVisibleInProject(ctx, projectID, annotationID)
}

// ResolvePortForward looks up which project/environment/port-forward
// currently owns hostPort, scoped to projects userID is a member of.
func (s *Service) ResolvePortForward(ctx context.Context, userID uuid.UUID, hostPort int) (*annotationdom.PortForwardMatch, error) {
	return s.repo.ResolvePortForward(ctx, userID, hostPort)
}

// GetScreenshotURL returns a presigned GET URL for an annotation's uploaded
// screenshot.
func (s *Service) GetScreenshotURL(ctx context.Context, projectID, annotationID uuid.UUID) (string, error) {
	a, err := s.repo.FindVisibleInProject(ctx, projectID, annotationID)
	if err != nil {
		return "", err
	}
	if a.ScreenshotFileID == nil {
		return "", annotationdom.ErrAnnotationScreenshotNotUploaded
	}
	f, err := s.files.FindFileByID(ctx, *a.ScreenshotFileID)
	if err != nil {
		return "", fmt.Errorf("annotation svc: find screenshot file: %w", err)
	}
	// Defense in depth: Create/CompleteScreenshotUpload already reject a
	// mismatched file at attach time, but re-checking the prefix here means
	// a future bug in either (or a screenshot_file_id set some other way)
	// can never turn into presigning a URL for a task attachment, doc, or
	// avatar instead of an actual screenshot. Not the uploader-identity half
	// of that check (see verifyAnnotationScreenshotFile's own doc comment on
	// why) — any project member with annotations.write may legitimately be
	// the one who attached this screenshot, not just annotation's creator.
	if !isAnnotationScreenshotFile(f) {
		return "", annotationdom.ErrAnnotationScreenshotMismatch
	}
	bucket := f.Bucket
	if bucket == "" {
		bucket = s.bucket
	}
	url, err := s.store.PresignGetObject(ctx, bucket, f.StorageKey, screenshotURLTTL, "")
	if err != nil {
		return "", fmt.Errorf("annotation svc: presign screenshot get: %w", err)
	}
	return url, nil
}

// taskTitleFromBody derives a task title from a comment body, truncated to
// a scannable length.
func taskTitleFromBody(body string) string {
	title := strings.TrimSpace(body)
	if title == "" {
		title = "Page comment"
	}
	title = strings.Join(strings.Fields(title), " ")
	runes := []rune(title)
	if len(runes) > maxTaskTitleLength {
		title = string(runes[:maxTaskTitleLength-1]) + "…"
	}
	return title
}

// sanitizeFileName strips path separators from a client-supplied file name
// before it's used as (part of) an object-store key — mirrors
// attachmentsvc's own sanitizeFileName.
func sanitizeFileName(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	if name == "" {
		return "screenshot.png"
	}
	return name
}

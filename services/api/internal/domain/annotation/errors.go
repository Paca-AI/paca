package annotationdom

import "errors"

// Annotation errors
var (
	ErrAnnotationNotFound  = errors.New("annotation not found")
	ErrAnnotationBodyEmpty = errors.New("comment body is empty")
	// ErrAnnotationAlreadyHasTask is returned by CreateTaskFromAnnotation
	// when the annotation already has a linked task — creating one is a
	// one-way action, not something a second click should silently repeat.
	ErrAnnotationAlreadyHasTask = errors.New("this comment already has a linked task")
	// ErrAnnotationTaskCreationInProgress is returned by
	// CreateTaskFromAnnotation when another (or a very recent) call has
	// already claimed this annotation for task creation — see
	// Repository.ClaimTaskCreation's doc comment for why this exists: it
	// closes the window where a retried request could otherwise create a
	// second task for the same annotation.
	ErrAnnotationTaskCreationInProgress = errors.New("a task is already being created for this comment")
	// ErrAnnotationScreenshotNotUploaded is returned by
	// CompleteScreenshotUpload when no matching InitiateScreenshotUpload
	// call preceded it for this annotation.
	ErrAnnotationScreenshotNotUploaded = errors.New("no pending screenshot upload for this annotation")
	// ErrAnnotationScreenshotMismatch is returned by Create,
	// CompleteScreenshotUpload, and GetScreenshotURL when the referenced
	// files.id isn't actually a screenshot the acting user uploaded via
	// InitiateScreenshotUpload — see verifyAnnotationScreenshotFile's own
	// doc comment for why this guard exists.
	ErrAnnotationScreenshotMismatch = errors.New("file does not belong to this annotation's screenshot upload")
)

// Comment errors
var (
	ErrCommentBodyEmpty = errors.New("comment body is empty")
)

// Port forward resolution errors
var (
	// ErrPortForwardNotFound is returned by ResolvePortForward when
	// hostPort doesn't currently belong to any port forward the requesting
	// user can see — either it isn't a real forward at all, or it belongs
	// to a project the user isn't a member of.
	ErrPortForwardNotFound = errors.New("no port forward found for this host port")
)

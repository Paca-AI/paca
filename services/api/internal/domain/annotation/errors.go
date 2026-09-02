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
	// ErrAnnotationScreenshotNotUploaded is returned by
	// CompleteScreenshotUpload when no matching InitiateScreenshotUpload
	// call preceded it for this annotation.
	ErrAnnotationScreenshotNotUploaded = errors.New("no pending screenshot upload for this annotation")
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

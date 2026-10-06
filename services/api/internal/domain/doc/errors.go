package docdom

import "errors"

// Sentinel domain errors for the document aggregate.
var (
	// Document errors.
	ErrDocNotFound     = errors.New("document: not found")
	ErrDocTitleInvalid = errors.New("document: title is empty or invalid")

	// Folder errors.
	ErrFolderNotFound     = errors.New("doc folder: not found")
	ErrFolderNameInvalid  = errors.New("doc folder: name is empty or invalid")
	ErrFolderNotInProject = errors.New("doc folder: folder does not belong to this project")
	ErrFolderSelfParent   = errors.New("doc folder: a folder cannot be its own parent")
	ErrFolderCycle        = errors.New("doc folder: moving the folder there would create a cycle")

	// Snapshot errors.
	ErrSnapshotNotFound = errors.New("doc snapshot: not found")

	// Activity / comment errors.
	ErrActivityNotFound      = errors.New("doc activity: not found")
	ErrActivityForbidden     = errors.New("doc activity: only the author can modify this comment")
	ErrActivityNotAComment   = errors.New("doc activity: this entry is not a comment and cannot be edited")
	ErrCommentContentInvalid = errors.New("doc activity: comment content must not be empty")

	// ErrCommentActorUnidentified is returned when the request authenticated
	// with the shared agent API key but did not supply an X-Agent-ID header.
	// That identity is never itself a project member, so there is no member
	// to attribute the comment to.
	ErrCommentActorUnidentified = errors.New("doc activity: request has no identifiable project member; pass X-Agent-ID when using the agent API key")
)

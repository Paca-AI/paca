package docdom

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Sentinel domain errors for the document aggregate.
var (
	// Document errors.
	ErrDocNotFound     = errors.New("document: not found")
	ErrDocTitleInvalid = errors.New("document: title is empty or invalid")

	// ErrNoChange is returned by an UpdateDocumentAtomic decide function to say
	// the write changes nothing: the repository skips the UPDATE, commits, and
	// returns the current row with a nil error.
	ErrNoChange = errors.New("doc: no change")

	// ErrVersionConflict means the document's content changed since the
	// version the caller named in If-Match. The error actually returned is a
	// *VersionConflictError, which matches it with errors.Is.
	ErrVersionConflict = errors.New("document: version conflict")

	// ErrDocLocked means the document is a locked managed copy and the write
	// would change its content or title without naming its repository as
	// the source.
	ErrDocLocked = errors.New("document: locked managed copy; edit it in its repository")
	// ErrDocLockNeedsSource means a write asked to lock a document that has
	// no source: only a managed copy can be locked.
	ErrDocLockNeedsSource = errors.New("document: only a document with a source can be locked")

	// ErrInvalidCursor is returned when a client-supplied tree-listing cursor
	// cannot be decoded.
	ErrInvalidCursor = errors.New("doc: invalid pagination cursor")

	// ErrTooManyDocIDs is returned by a batch read asked for more than
	// MaxBatchGetIDs documents.
	ErrTooManyDocIDs = errors.New("doc: too many document ids in one batch")

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

	// ErrCommentActorUnidentified is returned when the authenticated caller is
	// the built-in system actor, which is never itself a project member, so
	// there is no member to attribute the comment to.
	ErrCommentActorUnidentified = errors.New("doc activity: request has no identifiable project member")
)

// VersionConflictError is the ErrVersionConflict a write gets when its If-Match
// hash is not the stored content's hash. It names the version that is stored,
// so the caller can see what it lost to.
type VersionConflictError struct {
	CurrentHash string
	UpdatedAt   time.Time
	UpdatedBy   *uuid.UUID // project member; nil when unknown
	// Note, Client and Task are what the write that stored the current
	// content said about itself (empty when it said nothing).
	Note, Client, Task string
}

func (e *VersionConflictError) Error() string { return ErrVersionConflict.Error() }

// Is makes errors.Is(err, ErrVersionConflict) true.
func (e *VersionConflictError) Is(target error) bool { return target == ErrVersionConflict }

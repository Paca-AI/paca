package filedom

import "errors"

// Sentinel errors returned by the file manager.
var (
	ErrNodeNotFound = errors.New("file node not found")
	// ErrNotFolder is returned when a folder is required (a parent, a
	// destination) and the node is a file.
	ErrNotFolder = errors.New("file node is not a folder")
	// ErrNotFile is returned when a file is required (download, preview).
	ErrNotFile = errors.New("file node is not a file")
	// ErrNameInvalid is returned for an empty, too long or malformed name.
	ErrNameInvalid = errors.New("invalid file or folder name")
	// ErrNodeLocked is returned when a product-managed node is changed.
	ErrNodeLocked = errors.New("this item is managed by Paca and cannot be changed")
	// ErrMoveIntoSelf is returned when a folder is moved into itself or one of
	// its descendants.
	ErrMoveIntoSelf = errors.New("a folder cannot be moved into itself")
	// ErrNodeTrashed is returned when a trashed node is used as a parent or
	// target of an operation that needs a live one.
	ErrNodeTrashed = errors.New("file node is in the trash")
	// ErrNodeNotTrashed is returned by restore and permanent delete of a node
	// that is not in the trash.
	ErrNodeNotTrashed = errors.New("file node is not in the trash")
	// ErrUploadNotComplete is returned when a file's bytes were never uploaded.
	ErrUploadNotComplete = errors.New("file upload is not complete")
	// ErrUploadScopeMismatch is returned when a file being completed was
	// initiated in another scope.
	ErrUploadScopeMismatch = errors.New("file upload does not belong to this scope")
	// ErrFileTooLarge is returned when an upload exceeds MaxFileSize.
	ErrFileTooLarge = errors.New("file exceeds the maximum upload size")
	// ErrInvalidType is returned for an unknown type filter.
	ErrInvalidType = errors.New("unknown file type filter")
)

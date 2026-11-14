package docdom

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Documents addressed by path ("Folder/Sub folder/Title"): the write and read
// coding agents use to push a repository file to Paca and read it back in one
// request, instead of one request per folder.

var (
	// ErrDocPathInvalid means a document path has no title: it is empty or
	// made only of slashes and spaces, or it is too long.
	ErrDocPathInvalid = errors.New("document: path is empty or invalid")
	// ErrDocPathIsFolder means a path's last segment names a folder, not a
	// document.
	ErrDocPathIsFolder = errors.New("document: path names a folder")
	// ErrDocPathChanged is returned by DocPathWriter.WriteDocumentAtPath when
	// the path no longer resolves the way the caller saw it (a folder or the
	// document appeared, moved or was deleted in between). The service
	// resolves, authorizes and tries again.
	ErrDocPathChanged = errors.New("document: path changed while it was being written")
	// ErrDocSourceInvalid means a source has no repository or path, or a field
	// is longer than MaxSourceFieldLen.
	ErrDocSourceInvalid = errors.New("document: source needs a repository and a path, each at most 500 characters")
)

// maxDocPathLen bounds a document path, in bytes.
const maxDocPathLen = 4096

// SplitDocPath splits a document path into its folder names and the title:
// segments are separated by "/", trimmed, and empty ones are dropped. The
// last segment is the title. A path with no segment is ErrDocPathInvalid.
func SplitDocPath(path string) (folders []string, title string, err error) {
	if len(path) > maxDocPathLen {
		return nil, "", ErrDocPathInvalid
	}
	var segs []string
	for _, s := range strings.Split(path, "/") {
		if s = strings.TrimSpace(s); s != "" {
			segs = append(segs, s)
		}
	}
	if len(segs) == 0 {
		return nil, "", ErrDocPathInvalid
	}
	return segs[:len(segs)-1], segs[len(segs)-1], nil
}

// ValidateSource checks a source a write sets: repository and path are
// required, commit is optional, and each is at most MaxSourceFieldLen
// characters. Surrounding spaces are trimmed.
func ValidateSource(src *DocSource) (*DocSource, error) {
	if src == nil {
		return nil, nil
	}
	out := DocSource{
		Repository: strings.TrimSpace(src.Repository),
		Path:       strings.TrimSpace(src.Path),
		Commit:     strings.TrimSpace(src.Commit),
	}
	if out.Repository == "" || out.Path == "" {
		return nil, ErrDocSourceInvalid
	}
	for _, f := range []string{out.Repository, out.Path, out.Commit} {
		if utf8.RuneCountInString(f) > MaxSourceFieldLen {
			return nil, ErrDocSourceInvalid
		}
	}
	return &out, nil
}

// DocPathResolution is how far a path resolves in a project.
type DocPathResolution struct {
	// Depth is how many of the path's folders exist, from the root down.
	Depth int
	// FolderID is the deepest of those folders (nil = the project root).
	// When Depth equals the number of folders, it is the document's folder.
	FolderID *uuid.UUID
	// Doc is the document with the path's title in that folder; nil when
	// there is none (or when a folder is missing).
	Doc *Document
	// IsFolder: every folder exists, there is no document with the title,
	// and a folder has that name.
	IsFolder bool
}

// DocPathWrite is one create-or-update of the document at a path, run under a
// per-project lock by DocPathWriter.WriteDocumentAtPath.
type DocPathWrite struct {
	ProjectID uuid.UUID
	Folders   []string
	Title     string
	// Expect is the resolution the caller authorized. If the path resolves
	// to a different depth, folder or document under the lock, the write
	// returns ErrDocPathChanged and changes nothing.
	Expect DocPathResolution
	// NewFolder builds each missing folder (its ProjectID, ParentID and Name
	// are set by the repository).
	NewFolder func() *DocFolder
	// NewDoc builds the document to insert when there is none (its FolderID
	// and Title are set by the repository).
	NewDoc func() *Document
	// Decide updates an existing document under its row lock, as
	// UpdateDocumentAtomic's decide does (ErrNoChange writes nothing).
	Decide func(current *Document) error
}

// DocPathWriteResult is what WriteDocumentAtPath did.
type DocPathWriteResult struct {
	Doc     *Document
	Created bool
	// Folders are the folders it created, root first.
	Folders []*DocFolder
}

// DocPathRepository is the persistence of documents addressed by path.
type DocPathRepository interface {
	// ResolveDocPath resolves folders (case-insensitively, from the root;
	// among siblings with the same name the exact-case one wins, then the
	// first in manual order) and then the title in the last folder. Nothing is
	// locked.
	ResolveDocPath(ctx context.Context, projectID uuid.UUID, folders []string, title string) (*DocPathResolution, error)
	// WriteDocumentAtPath runs w in one transaction under the project's
	// folder lock: it resolves the path again, checks it against w.Expect,
	// creates the missing folders, then inserts the document or updates it
	// with w.Decide. Two writes of one path never create two documents or
	// two folders.
	WriteDocumentAtPath(ctx context.Context, w DocPathWrite) (*DocPathWriteResult, error)
}

// UpsertByPathInput carries a write of the document at a path.
type UpsertByPathInput struct {
	ProjectID uuid.UUID
	Path      string
	Content   json.RawMessage
	Source    *DocSource
	Lock      *bool
	// IfMatch is as UpdateDocumentInput.IfMatch. When the path has no
	// document yet, any If-Match fails with a *VersionConflictError.
	IfMatch *string
	Actor   *uuid.UUID
	// Authorize, when set, is called before anything is written with where
	// the write lands: the deepest folder of the path that exists (nil = the
	// root) and the document it would update (nil = it creates one). An
	// error aborts the write and is returned as is. It may be called again
	// if the path changes before the write.
	Authorize func(ctx context.Context, folderID *uuid.UUID, existing *Document) error
}

// DocPathService is the by-path use-case.
type DocPathService interface {
	// ResolveDocPath returns the document at path and its folder.
	// ErrDocNotFound when there is none (a folder may be missing),
	// ErrDocPathIsFolder when the path names a folder, ErrDocPathInvalid for
	// a path with no title.
	ResolveDocPath(ctx context.Context, projectID uuid.UUID, path string) (folderID *uuid.UUID, doc *Document, err error)
	// UpsertDocumentByPath creates the document at in.Path (and any missing
	// folder) or updates it as UpdateDocument does (If-Match, no-op, lock,
	// snapshot). created reports an insert.
	UpsertDocumentByPath(ctx context.Context, in UpsertByPathInput) (doc *Document, change DocChange, created bool, err error)
}

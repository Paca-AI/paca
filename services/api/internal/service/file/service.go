// Package filesvc implements the Drive-style file manager application service.
package filesvc

import (
	"context"
	"fmt"
	"mime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	filedom "github.com/Paca-AI/api/internal/domain/file"
	"github.com/Paca-AI/api/internal/platform/storage"
)

const presignedUploadTTL = time.Hour

// objectStatter is implemented by stores that can report an object's size.
type objectStatter interface {
	StatObject(ctx context.Context, bucket, key string) (int64, error)
}

// activeContentTypes can run script when a browser renders them from the
// storage origin, so they are only ever served as downloads.
func isActiveContent(contentType, name string) bool {
	ct, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		ct = strings.ToLower(strings.TrimSpace(contentType))
	}
	switch ct {
	case "text/html", "application/xhtml+xml", "image/svg+xml", "text/xml", "application/xml",
		"text/javascript", "application/javascript", "application/x-javascript", "text/css":
		return true
	}
	n := strings.ToLower(name)
	for _, ext := range []string{".html", ".htm", ".xhtml", ".svg", ".xml", ".js", ".mjs"} {
		if strings.HasSuffix(n, ext) {
			return true
		}
	}
	return false
}

// Service implements filedom.Service.
type Service struct {
	repo   filedom.Repository
	files  attachmentdom.FileRepository
	store  storage.Client
	bucket string
	now    func() time.Time

	realtime realtimeSink
}

var _ filedom.Service = (*Service)(nil)

// New returns a configured file manager service.
func New(repo filedom.Repository, files attachmentdom.FileRepository, store storage.Client, bucket string) *Service {
	return &Service{repo: repo, files: files, store: store, bucket: bucket, now: time.Now}
}

// node loads a node and hides it when it belongs to another scope.
func (s *Service) node(ctx context.Context, scope filedom.Scope, actor, id uuid.UUID) (*filedom.Node, error) {
	n, err := s.repo.GetNode(ctx, id, actor)
	if err != nil {
		return nil, err
	}
	if !scope.Contains(n) {
		return nil, filedom.ErrNodeNotFound
	}
	return n, nil
}

// liveNode is node, but the node must not be in the trash.
func (s *Service) liveNode(ctx context.Context, scope filedom.Scope, actor, id uuid.UUID) (*filedom.Node, error) {
	n, err := s.node(ctx, scope, actor, id)
	if err != nil {
		return nil, err
	}
	if n.DeletedAt != nil {
		return nil, filedom.ErrNodeTrashed
	}
	return n, nil
}

// destination validates a parent folder (nil is the scope root) that new or
// moved content is going into: it must be a live, unlocked folder of the scope.
func (s *Service) destination(ctx context.Context, scope filedom.Scope, actor uuid.UUID, parentID *uuid.UUID) (*filedom.Node, error) {
	if parentID == nil {
		return nil, nil
	}
	p, err := s.liveNode(ctx, scope, actor, *parentID)
	if err != nil {
		return nil, err
	}
	if !p.IsFolder() {
		return nil, filedom.ErrNotFolder
	}
	// Nothing is created or moved into the task attachment tree: it mirrors
	// the tasks' own attachments.
	if p.SystemKey != "" {
		return nil, filedom.ErrNodeLocked
	}
	return p, nil
}

// validateName trims and checks a file or folder name.
func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || len(name) > filedom.MaxNameLength || !utf8.ValidString(name) {
		return "", filedom.ErrNameInvalid
	}
	for _, r := range name {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return "", filedom.ErrNameInvalid
		}
	}
	return name, nil
}

// List returns one page of nodes of a folder, or a search over the scope.
func (s *Service) List(ctx context.Context, scope filedom.Scope, actor uuid.UUID, in filedom.ListInput) ([]*filedom.Node, int, error) {
	in.Query = strings.TrimSpace(in.Query)
	if in.Query == "" && in.ParentID != nil {
		p, err := s.liveNode(ctx, scope, actor, *in.ParentID)
		if err != nil {
			return nil, 0, err
		}
		if !p.IsFolder() {
			return nil, 0, filedom.ErrNotFolder
		}
	}
	return s.repo.List(ctx, filedom.ListParams{
		Scope: scope, ParentID: in.ParentID, UserID: actor, Query: in.Query, Type: in.Type,
		SortBy: in.SortBy, Desc: in.Desc, Limit: in.Limit, Offset: in.Offset,
	})
}

// Get returns one node.
func (s *Service) Get(ctx context.Context, scope filedom.Scope, actor, id uuid.UUID) (*filedom.Node, error) {
	return s.node(ctx, scope, actor, id)
}

// Path returns the breadcrumb of a node.
func (s *Service) Path(ctx context.Context, scope filedom.Scope, id uuid.UUID) ([]*filedom.Node, error) {
	if _, err := s.node(ctx, scope, uuid.Nil, id); err != nil {
		return nil, err
	}
	return s.repo.Path(ctx, id)
}

// CreateFolder creates a folder.
func (s *Service) CreateFolder(ctx context.Context, scope filedom.Scope, actor uuid.UUID, parentID *uuid.UUID, name string) (*filedom.Node, error) {
	name, err := validateName(name)
	if err != nil {
		return nil, err
	}
	if _, err := s.destination(ctx, scope, actor, parentID); err != nil {
		return nil, err
	}
	now := s.now()
	n := &filedom.Node{
		ID: uuid.New(), ProjectID: scope.ProjectID, ParentID: parentID, Kind: filedom.KindFolder,
		Name: name, CreatedBy: &actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.CreateFolder(ctx, n); err != nil {
		return nil, err
	}
	s.announce(ctx, scope, changeCreated, n, parentID)
	return s.repo.GetNode(ctx, n.ID, actor)
}

// Rename renames a node.
func (s *Service) Rename(ctx context.Context, scope filedom.Scope, actor, id uuid.UUID, name string) (*filedom.Node, error) {
	name, err := validateName(name)
	if err != nil {
		return nil, err
	}
	n, err := s.liveNode(ctx, scope, actor, id)
	if err != nil {
		return nil, err
	}
	if n.Locked() {
		return nil, filedom.ErrNodeLocked
	}
	if err := s.repo.Rename(ctx, n, name); err != nil {
		return nil, err
	}
	s.announce(ctx, scope, changeUpdated, n, n.ParentID)
	return s.repo.GetNode(ctx, id, actor)
}

// Update renames and/or moves a node. Everything is validated before anything
// is written, so a rejected move cannot leave a rename applied.
func (s *Service) Update(ctx context.Context, scope filedom.Scope, actor, id uuid.UUID, name *string, move bool, parentID *uuid.UUID) (*filedom.Node, error) {
	var newName string
	if name != nil {
		var err error
		if newName, err = validateName(*name); err != nil {
			return nil, err
		}
	}
	n, err := s.liveNode(ctx, scope, actor, id)
	if err != nil {
		return nil, err
	}
	if n.Locked() {
		return nil, filedom.ErrNodeLocked
	}
	if move {
		if _, err := s.destination(ctx, scope, actor, parentID); err != nil {
			return nil, err
		}
		if parentID != nil && n.IsFolder() {
			inside, err := s.repo.IsSelfOrDescendant(ctx, id, *parentID)
			if err != nil {
				return nil, err
			}
			if inside {
				return nil, filedom.ErrMoveIntoSelf
			}
		}
	}
	if name != nil {
		if err := s.repo.Rename(ctx, n, newName); err != nil {
			return nil, err
		}
	}
	if move {
		if err := s.repo.Move(ctx, n, parentID); err != nil {
			return nil, err
		}
	}
	// n.ParentID is where it was; the listings of both folders changed.
	s.announce(ctx, scope, changeUpdated, n, n.ParentID, parentIDIf(move, parentID, n.ParentID))
	return s.repo.GetNode(ctx, id, actor)
}

// Move moves a node to another folder of the same scope (nil: the root).
func (s *Service) Move(ctx context.Context, scope filedom.Scope, actor, id uuid.UUID, parentID *uuid.UUID) (*filedom.Node, error) {
	n, err := s.liveNode(ctx, scope, actor, id)
	if err != nil {
		return nil, err
	}
	if n.Locked() {
		return nil, filedom.ErrNodeLocked
	}
	if _, err := s.destination(ctx, scope, actor, parentID); err != nil {
		return nil, err
	}
	if parentID != nil && n.IsFolder() {
		inside, err := s.repo.IsSelfOrDescendant(ctx, id, *parentID)
		if err != nil {
			return nil, err
		}
		if inside {
			return nil, filedom.ErrMoveIntoSelf
		}
	}
	if err := s.repo.Move(ctx, n, parentID); err != nil {
		return nil, err
	}
	s.announce(ctx, scope, changeMoved, n, n.ParentID, parentID)
	return s.repo.GetNode(ctx, id, actor)
}

// Trash moves an item to the trash. Files that a task, document or annotation
// uses are deleted for good instead (see deleteReferencedFile).
func (s *Service) Trash(ctx context.Context, scope filedom.Scope, actor, id uuid.UUID) error {
	n, err := s.liveNode(ctx, scope, actor, id)
	if err != nil {
		return err
	}
	if n.SystemKey != "" {
		return filedom.ErrNodeLocked
	}
	if n.DeletedForGood() {
		if err := s.deleteReferencedFile(ctx, n); err != nil {
			return err
		}
		s.announce(ctx, scope, changeDeleted, n, n.ParentID)
		return nil
	}
	if err := s.repo.Trash(ctx, n, actor, s.now()); err != nil {
		return err
	}
	s.announce(ctx, scope, changeTrashed, n, n.ParentID)
	return nil
}

// deleteReferencedFile deletes a file that a task, document or annotation
// uses, for good, and the bytes with it. These files are never trashed: the
// owner would lose the file when the retention period ended, with nothing
// having told the user so when they asked for the delete (the web UI confirms
// it up front). The repository unlinks a task attachment in the same
// transaction as the delete, so the task loses it too; an annotation screenshot's row also detaches from its annotation and task
// through the foreign keys on files.
func (s *Service) deleteReferencedFile(ctx context.Context, n *filedom.Node) error {
	objs, err := s.repo.PurgeSubtree(ctx, n)
	if err != nil {
		return err
	}
	s.deleteObjects(ctx, objs)
	return nil
}

// Restore brings a trashed node back.
func (s *Service) Restore(ctx context.Context, scope filedom.Scope, actor, id uuid.UUID) (*filedom.Node, error) {
	n, err := s.node(ctx, scope, actor, id)
	if err != nil {
		return nil, err
	}
	if n.DeletedAt == nil {
		return nil, filedom.ErrNodeNotTrashed
	}
	if err := s.repo.Restore(ctx, n); err != nil {
		return nil, err
	}
	restored, err := s.repo.GetNode(ctx, id, actor)
	if err != nil {
		return nil, err
	}
	// Restore may put it back at the root when its folder is gone: announce
	// where it landed. A restored folder also brings its contents back, so its
	// own listing changed too.
	if restored.IsFolder() {
		s.announce(ctx, scope, changeRestored, restored, restored.ParentID, &restored.ID)
	} else {
		s.announce(ctx, scope, changeRestored, restored, restored.ParentID)
	}
	return restored, nil
}

// DeletePermanently removes a trashed node and its bytes.
func (s *Service) DeletePermanently(ctx context.Context, scope filedom.Scope, id uuid.UUID) error {
	n, err := s.node(ctx, scope, uuid.Nil, id)
	if err != nil {
		return err
	}
	if n.DeletedAt == nil {
		return filedom.ErrNodeNotTrashed
	}
	objs, err := s.repo.PurgeSubtree(ctx, n)
	if err != nil {
		return err
	}
	s.deleteObjects(ctx, objs)
	// Only the trash listing shows it, but name the folder all the same.
	s.announce(ctx, scope, changeDeleted, n, n.ParentID)
	return nil
}

// PurgeExpired removes everything trashed before the cutoff.
func (s *Service) PurgeExpired(ctx context.Context, before time.Time, limit int) (int, error) {
	objs, err := s.repo.PurgeTrashedBefore(ctx, before, limit)
	s.deleteObjects(ctx, objs)
	return len(objs), err
}

// deleteObjects removes objects from the store. The rows are already gone, so
// a failure only leaves an orphaned object; it must not fail the request.
func (s *Service) deleteObjects(ctx context.Context, objs []filedom.StorageObject) {
	for _, o := range objs {
		bucket := o.Bucket
		if bucket == "" {
			bucket = s.bucket
		}
		_ = s.store.DeleteObject(ctx, bucket, o.Key)
	}
}

// sanitizeKeyName makes a file name safe as the last segment of a storage key.
func sanitizeKeyName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, name)
	if name == "" {
		return "file"
	}
	return name
}

// InitiateUpload creates the pending file record and the upload session.
func (s *Service) InitiateUpload(ctx context.Context, scope filedom.Scope, actor uuid.UUID, in filedom.UploadInput) (*attachmentdom.UploadSession, error) {
	name, err := validateName(in.FileName)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.ContentType) == "" {
		in.ContentType = "application/octet-stream"
	}
	if in.FileSize <= 0 {
		return nil, attachmentdom.ErrFileSizeZero
	}
	if in.FileSize > filedom.MaxFileSize {
		return nil, filedom.ErrFileTooLarge
	}
	if _, err := s.destination(ctx, scope, actor, in.ParentID); err != nil {
		return nil, err
	}

	fileID := uuid.New()
	key := fmt.Sprintf("files/%s/%s/%s", scope.Key(), fileID, sanitizeKeyName(name))
	now := s.now()
	f := &attachmentdom.File{
		ID: fileID, StorageKey: key, Bucket: s.bucket, FileName: name, ContentType: in.ContentType,
		FileSize: in.FileSize, UploadStatus: attachmentdom.UploadStatusPending, UploadedBy: &actor,
		ProjectID: scope.ProjectID, FolderID: in.ParentID, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.files.CreateFile(ctx, f); err != nil {
		return nil, fmt.Errorf("file svc: create file: %w", err)
	}

	session := &attachmentdom.UploadSession{FileID: fileID}
	if in.FileSize >= storage.MultipartThreshold {
		mu, err := s.store.InitiateMultipartUpload(ctx, s.bucket, key, in.ContentType, in.FileSize, storage.DefaultPartSize, presignedUploadTTL)
		if err != nil {
			return nil, fmt.Errorf("file svc: initiate multipart: %w", err)
		}
		if err := s.files.UpdateFileStatus(ctx, fileID, attachmentdom.UploadStatusPending, &mu.UploadID); err != nil {
			return nil, fmt.Errorf("file svc: save upload id: %w", err)
		}
		session.IsMultipart = true
		session.Multipart = mu
		return session, nil
	}
	url, err := s.store.PresignPutObject(ctx, s.bucket, key, in.ContentType, presignedUploadTTL)
	if err != nil {
		return nil, fmt.Errorf("file svc: presign put: %w", err)
	}
	session.UploadURL = url
	return session, nil
}

// CompleteUpload finishes the upload; the file then shows up in its folder.
func (s *Service) CompleteUpload(ctx context.Context, scope filedom.Scope, actor uuid.UUID, in filedom.CompleteInput) (*filedom.Node, error) {
	f, err := s.files.FindFileByID(ctx, in.FileID)
	if err != nil {
		return nil, err
	}
	// The row records the scope and uploader the upload was started with.
	sameScope := (f.ProjectID == nil && scope.ProjectID == nil) ||
		(f.ProjectID != nil && scope.ProjectID != nil && *f.ProjectID == *scope.ProjectID)
	if !sameScope || !strings.HasPrefix(f.StorageKey, fmt.Sprintf("files/%s/%s/", scope.Key(), f.ID)) ||
		f.UploadedBy == nil || *f.UploadedBy != actor {
		return nil, filedom.ErrUploadScopeMismatch
	}
	if f.UploadStatus != attachmentdom.UploadStatusPending {
		return nil, attachmentdom.ErrUploadNotPending
	}
	// The folder may have been trashed or removed while the bytes uploaded.
	if _, err := s.destination(ctx, scope, actor, f.FolderID); err != nil {
		return nil, err
	}

	switch {
	case f.MultipartUploadID != nil && in.UploadID == nil:
		return nil, attachmentdom.ErrMultipartUploadIDRequired
	case f.MultipartUploadID == nil && in.UploadID != nil:
		return nil, attachmentdom.ErrNotMultipartUpload
	case f.MultipartUploadID != nil && *in.UploadID != *f.MultipartUploadID:
		return nil, attachmentdom.ErrUploadIDMismatch
	case f.MultipartUploadID != nil && len(in.Parts) == 0:
		return nil, attachmentdom.ErrMultipartPartsEmpty
	}
	if in.UploadID != nil {
		parts := make([]storage.CompletedPart, 0, len(in.Parts))
		for _, p := range in.Parts {
			parts = append(parts, storage.CompletedPart{PartNumber: p.PartNumber, ETag: p.ETag})
		}
		if err := s.store.CompleteMultipartUpload(ctx, f.Bucket, f.StorageKey, *in.UploadID, parts); err != nil {
			return nil, fmt.Errorf("file svc: complete multipart: %w", err)
		}
	}
	// The bytes must really be in the store, with the size the upload declared:
	// otherwise the listing would show a file that cannot be opened, with a
	// size the client made up.
	if st, ok := s.store.(objectStatter); ok {
		size, err := st.StatObject(ctx, f.Bucket, f.StorageKey)
		if err != nil || size != f.FileSize {
			return nil, filedom.ErrUploadNotComplete
		}
	}
	if err := s.files.UpdateFileStatus(ctx, f.ID, attachmentdom.UploadStatusUploaded, nil); err != nil {
		return nil, fmt.Errorf("file svc: update file status: %w", err)
	}
	node, err := s.repo.GetNode(ctx, f.ID, actor)
	if err != nil {
		return nil, err
	}
	s.announce(ctx, scope, changeCreated, node, node.ParentID)
	return node, nil
}

// DownloadURL presigns a GET for a file and records the open.
func (s *Service) DownloadURL(ctx context.Context, scope filedom.Scope, actor, id uuid.UUID, ttl time.Duration, forceDownload bool) (string, *filedom.Node, error) {
	n, err := s.liveNode(ctx, scope, actor, id)
	if err != nil {
		return "", nil, err
	}
	if n.IsFolder() {
		return "", nil, filedom.ErrNotFile
	}
	disposition := ""
	if forceDownload || isActiveContent(n.ContentType, n.Name) {
		name := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, n.Name)
		if name == "" {
			name = "file"
		}
		if disposition = mime.FormatMediaType("attachment", map[string]string{"filename": name}); disposition == "" {
			disposition = "attachment"
		}
	}
	bucket := n.Bucket
	if bucket == "" {
		bucket = s.bucket
	}
	url, err := s.store.PresignGetObject(ctx, bucket, n.StorageKey, ttl, disposition)
	if err != nil {
		return "", nil, fmt.Errorf("file svc: presign get: %w", err)
	}
	// Recents are a convenience: never fail a download over them.
	_ = s.repo.Touch(ctx, actor, id)
	return url, n, nil
}

// SetStarred stars or unstars a node for the caller.
func (s *Service) SetStarred(ctx context.Context, scope filedom.Scope, actor, id uuid.UUID, starred bool) error {
	if _, err := s.liveNode(ctx, scope, actor, id); err != nil {
		return err
	}
	return s.repo.SetStarred(ctx, actor, id, starred)
}

// ListStarred lists the caller's starred nodes.
func (s *Service) ListStarred(ctx context.Context, scope filedom.Scope, actor uuid.UUID, limit, offset int) ([]*filedom.Node, int, error) {
	return s.repo.ListStarred(ctx, scope, actor, limit, offset)
}

// ListRecent lists the nodes the caller opened last.
func (s *Service) ListRecent(ctx context.Context, scope filedom.Scope, actor uuid.UUID, limit int) ([]*filedom.Node, error) {
	return s.repo.ListRecent(ctx, scope, actor, limit)
}

// ListTrash lists the trashed items of the scope.
func (s *Service) ListTrash(ctx context.Context, scope filedom.Scope, actor uuid.UUID, limit, offset int) ([]*filedom.Node, int, error) {
	return s.repo.ListTrash(ctx, scope, actor, limit, offset)
}

// parentIDIf is the folder a node is in after an Update: the new one when it
// was moved, else where it already was.
func parentIDIf(move bool, to, current *uuid.UUID) *uuid.UUID {
	if move {
		return to
	}
	return current
}

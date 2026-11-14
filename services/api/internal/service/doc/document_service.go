// Package docsvc implements document management application services.
package docsvc

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/events"
)

// memberLookup resolves an account to its project membership.
type memberLookup interface {
	FindMember(ctx context.Context, projectID, accountID uuid.UUID) (*projectdom.ProjectMember, error)
}

// Service is the concrete implementation of docdom.Service.
type Service struct {
	repo       docdom.Repository
	memberRepo memberLookup
	realtime   realtimeSink
}

// realtimeSink is where folder changes are announced to live clients.
type realtimeSink interface {
	Publish(ctx context.Context, channel string, payload any) error
}

// WithRealtime sets where folder changes are announced. Folder changes are
// realtime-only: the activity log has no entry kind for them, but another
// user's open doc tree must follow them. Without a sink nothing is announced.
func (s *Service) WithRealtime(sink realtimeSink) *Service {
	s.realtime = sink
	return s
}

// announceFolder tells live clients a folder was created, renamed/moved or
// deleted. The event is checked as docs:read on the project (events.AuthzFor).
func (s *Service) announceFolder(ctx context.Context, topic string, f *docdom.DocFolder) {
	if s.realtime == nil || f == nil {
		return
	}
	payload := map[string]any{
		"project_id": f.ProjectID.String(),
		"folder_id":  f.ID.String(),
		"name":       f.Name,
	}
	if f.ParentID != nil {
		payload["parent_id"] = f.ParentID.String()
	}
	id := f.ID
	events.PublishRealtime(ctx, s.realtime, events.Event{
		Topic:      topic,
		Payload:    payload,
		ProjectID:  f.ProjectID,
		EntityType: events.EntityDoc,
		EntityID:   &id,
	})
}

// New returns a configured document service.
// memberRepo is used to resolve user UUIDs to project-member UUIDs before
// persisting created_by / updated_by fields; it may be nil (resolution is
// skipped and the field is stored as NULL).
func New(repo docdom.Repository, memberRepo memberLookup) *Service {
	return &Service{repo: repo, memberRepo: memberRepo}
}

// --- Folder Service ---------------------------------------------------------

// ListFolders returns one cursor-paginated level of the folder tree.
func (s *Service) ListFolders(ctx context.Context, projectID uuid.UUID, parentID *uuid.UUID, filter docdom.FolderFilter, cursorAfter *string, limit int) ([]*docdom.DocFolder, bool, error) {
	return s.repo.ListFolders(ctx, projectID, parentID, filter, cursorAfter, limit)
}

// FolderPath returns the folders from the root down to folderID.
func (s *Service) FolderPath(ctx context.Context, projectID, folderID uuid.UUID) ([]*docdom.DocFolder, error) {
	return s.repo.FolderPath(ctx, projectID, folderID)
}

// CreateFolder creates a new folder.
func (s *Service) CreateFolder(ctx context.Context, in docdom.CreateFolderInput) (*docdom.DocFolder, error) {
	f, err := s.createFolder(ctx, in)
	if err == nil {
		s.announceFolder(ctx, string(docdom.ActivityTypeFolderCreated), f)
	}
	return f, err
}

func (s *Service) createFolder(ctx context.Context, in docdom.CreateFolderInput) (*docdom.DocFolder, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || strings.Contains(name, "/") {
		return nil, docdom.ErrFolderNameInvalid
	}

	if in.ParentID != nil {
		parent, err := s.repo.FindFolderByID(ctx, *in.ParentID)
		if err != nil {
			return nil, err
		}
		if parent.ProjectID != in.ProjectID {
			return nil, docdom.ErrFolderNotInProject
		}
	}

	now := time.Now()
	f := &docdom.DocFolder{
		ID:        uuid.New(),
		ProjectID: in.ProjectID,
		ParentID:  in.ParentID,
		Name:      name,
		Position:  0,
		CreatedBy: s.resolveMember(ctx, in.CreatedBy, in.ProjectID),
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.repo.CreateFolder(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

// UpdateFolder updates the mutable fields of a folder. Validation runs inside
// the repository's locked read-modify-write so concurrent moves can't combine
// into a parent cycle.
func (s *Service) UpdateFolder(ctx context.Context, id uuid.UUID, in docdom.UpdateFolderInput) (*docdom.DocFolder, error) {
	f, err := s.updateFolder(ctx, id, in)
	if err == nil {
		s.announceFolder(ctx, string(docdom.ActivityTypeFolderUpdated), f)
	}
	return f, err
}

func (s *Service) updateFolder(ctx context.Context, id uuid.UUID, in docdom.UpdateFolderInput) (*docdom.DocFolder, error) {
	return s.repo.UpdateFolderAtomic(ctx, id, func(f *docdom.DocFolder, find func(uuid.UUID) (*docdom.DocFolder, error)) error {
		if f.ProjectID != in.ProjectID {
			return docdom.ErrFolderNotFound
		}

		if name := strings.TrimSpace(in.Name); name != "" && name != f.Name {
			if strings.Contains(name, "/") {
				return docdom.ErrFolderNameInvalid
			}
			f.Name = name
		}
		if in.ParentID != nil { // double-pointer present → update parent
			if *in.ParentID != nil {
				newParentID := **in.ParentID
				if newParentID == id {
					return docdom.ErrFolderSelfParent
				}
				parent, err := find(newParentID)
				if err != nil {
					return err
				}
				if parent.ProjectID != f.ProjectID {
					return docdom.ErrFolderNotInProject
				}
				// Walk up from the new parent; reaching f means f would end up
				// inside its own subtree.
				const maxDepth = 100
				cur := parent
				for range maxDepth {
					if cur.ParentID == nil {
						break
					}
					if *cur.ParentID == id {
						return docdom.ErrFolderCycle
					}
					if cur, err = find(*cur.ParentID); err != nil {
						return err
					}
				}
			}
			f.ParentID = *in.ParentID
		}
		if in.Position != nil {
			f.Position = *in.Position
		}
		f.UpdatedAt = time.Now()
		return nil
	})
}

// DeleteFolder deletes a folder.
func (s *Service) DeleteFolder(ctx context.Context, id uuid.UUID, projectID uuid.UUID) error {
	f, err := s.repo.FindFolderByID(ctx, id)
	if err != nil {
		return err
	}
	if f.ProjectID != projectID {
		return docdom.ErrFolderNotFound
	}
	if err := s.repo.DeleteFolder(ctx, id); err != nil {
		return err
	}
	s.announceFolder(ctx, string(docdom.ActivityTypeFolderDeleted), f)
	return nil
}

// --- Document Service -------------------------------------------------------

// ListDocuments returns one cursor-paginated page of a project's documents.
func (s *Service) ListDocuments(ctx context.Context, projectID uuid.UUID, filter docdom.DocumentFilter, cursorAfter *string, limit int) ([]*docdom.Document, bool, error) {
	return s.repo.ListDocuments(ctx, projectID, filter, cursorAfter, limit)
}

// SearchDocuments finds documents whose title or body contains query.
func (s *Service) SearchDocuments(ctx context.Context, projectID uuid.UUID, query string, limit int) ([]docdom.SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	docs, err := s.repo.SearchDocuments(ctx, projectID, query, limit)
	if err != nil {
		return nil, err
	}
	hits := make([]docdom.SearchHit, 0, len(docs))
	for _, d := range docs {
		hits = append(hits, docdom.NewSearchHit(d, query))
	}
	return hits, nil
}

// GetDocument returns a single document.
func (s *Service) GetDocument(ctx context.Context, id uuid.UUID) (*docdom.Document, error) {
	return s.repo.FindDocumentByID(ctx, id)
}

// CreateDocument creates a new document.
func (s *Service) CreateDocument(ctx context.Context, in docdom.CreateDocumentInput) (*docdom.Document, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "Untitled"
	} else if strings.Contains(title, "/") {
		return nil, docdom.ErrDocTitleInvalid
	}

	if in.FolderID != nil {
		folder, err := s.repo.FindFolderByID(ctx, *in.FolderID)
		if err != nil {
			return nil, err
		}
		if folder.ProjectID != in.ProjectID {
			return nil, docdom.ErrFolderNotInProject
		}
	}

	now := storedNow()
	memberID := s.resolveMember(ctx, in.CreatedBy, in.ProjectID)
	hash := docdom.ContentHash(in.Content)
	d := &docdom.Document{
		ID:          uuid.New(),
		ProjectID:   in.ProjectID,
		FolderID:    in.FolderID,
		Title:       title,
		Content:     in.Content,
		ContentHash: &hash,
		Position:    0,
		CreatedBy:   memberID,
		UpdatedBy:   memberID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.repo.CreateDocument(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

// UpdateDocument updates a document's mutable fields and creates a snapshot
// when the content or title changes. The read-modify-write runs under a row
// lock so a concurrent save isn't reverted, a concurrent delete isn't undone,
// and the If-Match and "did anything change" checks see the row that will be
// written. A write that changes nothing is not written at all.
func (s *Service) UpdateDocument(ctx context.Context, projectID, id uuid.UUID, in docdom.UpdateDocumentInput) (*docdom.Document, docdom.DocChange, error) {
	var change docdom.DocChange
	var oldContent json.RawMessage
	var oldTitle, oldHash string

	// Lookups that don't depend on the locked row run before the transaction,
	// so the row lock isn't held across extra round trips on other connections.
	if in.FolderID != nil && *in.FolderID != nil {
		folder, ferr := s.repo.FindFolderByID(ctx, **in.FolderID)
		if ferr != nil {
			return nil, docdom.DocChange{}, ferr
		}
		if folder.ProjectID != projectID {
			return nil, docdom.DocChange{}, docdom.ErrFolderNotInProject
		}
	}
	var newHash string
	if in.Content != nil {
		newHash = docdom.ContentHash(*in.Content)
	}
	updatedBy := s.resolveMember(ctx, in.UpdatedBy, projectID)

	d, err := s.repo.UpdateDocumentAtomic(ctx, id, func(d *docdom.Document) error {
		if d.ProjectID != projectID {
			return docdom.ErrDocNotFound
		}
		change = docdom.DocChange{}
		oldContent = d.Content
		oldTitle = d.Title
		oldHash = d.Hash()

		if in.IfMatch != nil && *in.IfMatch != "*" && *in.IfMatch != oldHash {
			return &docdom.VersionConflictError{CurrentHash: oldHash, UpdatedAt: d.UpdatedAt, UpdatedBy: d.UpdatedBy}
		}

		if in.Title != nil {
			title := strings.TrimSpace(*in.Title)
			if title == "" {
				return docdom.ErrDocTitleInvalid
			}
			if title != d.Title {
				if strings.Contains(title, "/") {
					return docdom.ErrDocTitleInvalid
				}
				d.Title = title
				change.Title = true
			}
		}
		if in.Content != nil && newHash != oldHash {
			d.Content = *in.Content
			change.Content = true
		}
		if in.FolderID != nil && !sameFolder(*in.FolderID, d.FolderID) {
			d.FolderID = *in.FolderID
			change.Folder = true
		}
		if in.Position != nil && *in.Position != d.Position {
			d.Position = *in.Position
			change.Position = true
		}
		if !change.Any() {
			return docdom.ErrNoChange
		}
		// Every real write stores the hash, so a row written before the
		// column existed gets one on its next write.
		h := d.Hash()
		if change.Content {
			h = newHash
		}
		d.ContentHash = &h
		d.UpdatedBy = updatedBy
		d.UpdatedAt = storedNow()
		return nil
	})
	if err != nil {
		return nil, docdom.DocChange{}, err
	}

	// Create a snapshot of the previous version whenever content or title changed.
	if change.Content || change.Title {
		snapTime := time.Now()
		snap := &docdom.DocSnapshot{
			ID:          uuid.New(),
			DocumentID:  d.ID,
			Title:       oldTitle,
			Content:     oldContent,
			ContentHash: &oldHash,
			CreatedBy:   d.UpdatedBy,
			CreatedAt:   snapTime,
		}
		// Snapshot creation failures are non-fatal; we still return the
		// updated document.
		if err := s.repo.CreateSnapshot(ctx, snap); err == nil {
			// Prune any other snapshots taken within the past 3 minutes so
			// that rapid edits do not accumulate excessive history entries.
			_ = s.repo.DeleteRecentSnapshotsExcept(ctx, d.ID, snap.ID, snapTime.Add(-3*time.Minute))
		}
	}

	return d, change, nil
}

// storedNow is the current time at the precision Postgres keeps (microseconds),
// so the document a write returns carries the same timestamps as the row a
// later read returns. Without it a write's response and the next read differ
// in their sub-microsecond digits, and so in the ETag built from them.
func storedNow() time.Time { return time.Now().Truncate(time.Microsecond) }

// sameFolder reports whether two optional folder ids name the same folder
// (both nil means both at the root).
func sameFolder(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// DeleteDocument soft-deletes a document.
func (s *Service) DeleteDocument(ctx context.Context, projectID, id uuid.UUID) error {
	d, err := s.repo.FindDocumentByID(ctx, id)
	if err != nil {
		return err
	}
	if d.ProjectID != projectID {
		return docdom.ErrDocNotFound
	}
	return s.repo.DeleteDocument(ctx, id)
}

// --- Snapshot Service -------------------------------------------------------

// ListSnapshots returns one page of a document's snapshots, newest first.
func (s *Service) ListSnapshots(ctx context.Context, projectID, documentID uuid.UUID, contentHash *string, cursorAfter *string, limit int) ([]*docdom.DocSnapshot, bool, error) {
	d, err := s.repo.FindDocumentByID(ctx, documentID)
	if err != nil {
		return nil, false, err
	}
	if d.ProjectID != projectID {
		return nil, false, docdom.ErrDocNotFound
	}
	return s.repo.ListSnapshots(ctx, documentID, contentHash, cursorAfter, limit)
}

// GetSnapshot returns a single snapshot.
func (s *Service) GetSnapshot(ctx context.Context, projectID, id uuid.UUID) (*docdom.DocSnapshot, error) {
	snap, err := s.repo.FindSnapshotByID(ctx, id)
	if err != nil {
		return nil, err
	}
	d, err := s.repo.FindDocumentByID(ctx, snap.DocumentID)
	if err != nil {
		return nil, err
	}
	if d.ProjectID != projectID {
		return nil, docdom.ErrSnapshotNotFound
	}
	return snap, nil
}

// --- helpers ----------------------------------------------------------------

// resolveMember converts a user UUID to a project_members.id UUID.
// Returns nil (no error) when memberRepo is not configured or when accountID is nil.
func (s *Service) resolveMember(ctx context.Context, accountID *uuid.UUID, projectID uuid.UUID) *uuid.UUID {
	if s.memberRepo == nil || accountID == nil {
		return nil
	}
	member, err := s.memberRepo.FindMember(ctx, projectID, *accountID)
	if err != nil {
		return nil
	}
	return &member.ID
}

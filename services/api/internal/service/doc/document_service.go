// Package docsvc implements document management application services.
package docsvc

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// Service is the concrete implementation of docdom.Service.
type Service struct {
	repo       docdom.Repository
	memberRepo memberLookup
}

// New returns a configured document service.
// memberRepo is used to resolve user UUIDs to project-member UUIDs before
// persisting created_by / updated_by fields; it may be nil (resolution is
// skipped and the field is stored as NULL).
func New(repo docdom.Repository, memberRepo memberLookup) *Service {
	return &Service{repo: repo, memberRepo: memberRepo}
}

// --- Folder Service ---------------------------------------------------------

// ListFolders returns all folders for a project.
func (s *Service) ListFolders(ctx context.Context, projectID uuid.UUID) ([]*docdom.DocFolder, error) {
	return s.repo.ListFolders(ctx, projectID)
}

// CreateFolder creates a new folder.
func (s *Service) CreateFolder(ctx context.Context, in docdom.CreateFolderInput) (*docdom.DocFolder, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
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

// UpdateFolder updates the mutable fields of a folder.
func (s *Service) UpdateFolder(ctx context.Context, id uuid.UUID, in docdom.UpdateFolderInput) (*docdom.DocFolder, error) {
	f, err := s.repo.FindFolderByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if f.ProjectID != in.ProjectID {
		return nil, docdom.ErrFolderNotFound
	}

	if name := strings.TrimSpace(in.Name); name != "" {
		f.Name = name
	}
	if in.ParentID != nil { // double-pointer present → update parent
		if *in.ParentID != nil {
			newParentID := **in.ParentID
			if newParentID == id {
				return nil, docdom.ErrFolderSelfParent
			}
			parent, err := s.repo.FindFolderByID(ctx, newParentID)
			if err != nil {
				return nil, err
			}
			if parent.ProjectID != f.ProjectID {
				return nil, docdom.ErrFolderNotInProject
			}
		}
		f.ParentID = *in.ParentID
	}
	if in.Position != nil {
		f.Position = *in.Position
	}
	f.UpdatedAt = time.Now()

	if err := s.repo.UpdateFolder(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
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
	return s.repo.DeleteFolder(ctx, id)
}

// --- Document Service -------------------------------------------------------

// ListDocuments returns non-deleted documents for a project.
func (s *Service) ListDocuments(ctx context.Context, projectID uuid.UUID, folderID *uuid.UUID) ([]*docdom.Document, error) {
	return s.repo.ListDocuments(ctx, projectID, folderID)
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

	now := time.Now()
	memberID := s.resolveMember(ctx, in.CreatedBy, in.ProjectID)
	d := &docdom.Document{
		ID:        uuid.New(),
		ProjectID: in.ProjectID,
		FolderID:  in.FolderID,
		Title:     title,
		Content:   in.Content,
		Position:  0,
		CreatedBy: memberID,
		UpdatedBy: memberID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.repo.CreateDocument(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

// UpdateDocument updates a document's mutable fields and creates a snapshot
// when the content changes.
func (s *Service) UpdateDocument(ctx context.Context, id uuid.UUID, in docdom.UpdateDocumentInput) (*docdom.Document, error) {
	d, err := s.repo.FindDocumentByID(ctx, id)
	if err != nil {
		return nil, err
	}

	contentChanged := false
	oldContent := d.Content
	oldTitle := d.Title

	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if title == "" {
			return nil, docdom.ErrDocTitleInvalid
		}
		d.Title = title
	}
	if in.Content != nil {
		// Only treat as changed if the raw JSON differs.
		if string(*in.Content) != string(d.Content) {
			contentChanged = true
		}
		d.Content = *in.Content
	}
	if in.FolderID != nil { // double-pointer present → update folder
		if *in.FolderID != nil {
			folder, ferr := s.repo.FindFolderByID(ctx, **in.FolderID)
			if ferr != nil {
				return nil, ferr
			}
			if folder.ProjectID != d.ProjectID {
				return nil, docdom.ErrFolderNotInProject
			}
		}
		d.FolderID = *in.FolderID
	}
	if in.Position != nil {
		d.Position = *in.Position
	}
	d.UpdatedBy = s.resolveMember(ctx, in.UpdatedBy, d.ProjectID)
	d.UpdatedAt = time.Now()

	if err := s.repo.UpdateDocument(ctx, d); err != nil {
		return nil, err
	}

	// Create a snapshot whenever content or title changed.
	if contentChanged || d.Title != oldTitle {
		snapTime := time.Now()
		snap := &docdom.DocSnapshot{
			ID:         uuid.New(),
			DocumentID: d.ID,
			Title:      oldTitle,
			Content:    oldContent,
			CreatedBy:  d.UpdatedBy,
			CreatedAt:  snapTime,
		}
		// Snapshot creation failures are non-fatal; we still return the
		// updated document.
		if err := s.repo.CreateSnapshot(ctx, snap); err == nil {
			// Prune any other snapshots taken within the past 3 minutes so
			// that rapid edits do not accumulate excessive history entries.
			_ = s.repo.DeleteRecentSnapshotsExcept(ctx, d.ID, snap.ID, snapTime.Add(-3*time.Minute))
		}
	}

	return d, nil
}

// DeleteDocument soft-deletes a document.
func (s *Service) DeleteDocument(ctx context.Context, id uuid.UUID) error {
	if _, err := s.repo.FindDocumentByID(ctx, id); err != nil {
		return err
	}
	return s.repo.DeleteDocument(ctx, id)
}

// --- Snapshot Service -------------------------------------------------------

// ListSnapshots returns snapshots for a document, newest first.
func (s *Service) ListSnapshots(ctx context.Context, documentID uuid.UUID) ([]*docdom.DocSnapshot, error) {
	if _, err := s.repo.FindDocumentByID(ctx, documentID); err != nil {
		return nil, err
	}
	return s.repo.ListSnapshots(ctx, documentID)
}

// GetSnapshot returns a single snapshot.
func (s *Service) GetSnapshot(ctx context.Context, id uuid.UUID) (*docdom.DocSnapshot, error) {
	snap, err := s.repo.FindSnapshotByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return snap, nil
}

// --- helpers ----------------------------------------------------------------

// resolveMember converts a user UUID to a project_members.id UUID.
// Returns nil (no error) when memberRepo is not configured or when userID is nil.
func (s *Service) resolveMember(ctx context.Context, userID *uuid.UUID, projectID uuid.UUID) *uuid.UUID {
	if s.memberRepo == nil || userID == nil {
		return nil
	}
	member, err := s.memberRepo.FindMemberByActor(ctx, projectID, *userID, nil)
	if err != nil {
		return nil
	}
	return &member.ID
}

// buildFieldChanges constructs the content for a doc.updated activity.
func buildFieldChanges(changes []docdom.FieldChange) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"changes": changes})
	return raw
}

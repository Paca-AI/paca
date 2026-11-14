package docsvc

import (
	"context"
	"errors"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// maxPathAttempts bounds how often UpsertDocumentByPath resolves and
// authorizes again when the path changes between its look and its write.
const maxPathAttempts = 3

// ResolveDocPath returns the document at path and its folder.
func (s *Service) ResolveDocPath(ctx context.Context, projectID uuid.UUID, path string) (*uuid.UUID, *docdom.Document, error) {
	folders, title, err := docdom.SplitDocPath(path)
	if err != nil {
		return nil, nil, err
	}
	res, err := s.repo.ResolveDocPath(ctx, projectID, folders, title)
	if err != nil {
		return nil, nil, err
	}
	switch {
	case res.IsFolder:
		return nil, nil, docdom.ErrDocPathIsFolder
	case res.Doc == nil:
		return nil, nil, docdom.ErrDocNotFound
	}
	return res.Doc.FolderID, res.Doc, nil
}

// UpsertDocumentByPath creates or updates the document at in.Path. It
// resolves the path, lets in.Authorize check where the write lands, then
// writes under the project's folder lock, which also checks that the path
// still resolves the same way. If it changed in between (another push
// created the document, a folder moved), it starts over, at most
// maxPathAttempts times.
func (s *Service) UpsertDocumentByPath(ctx context.Context, in docdom.UpsertByPathInput) (*docdom.Document, docdom.DocChange, bool, error) {
	folders, title, err := docdom.SplitDocPath(in.Path)
	if err != nil {
		return nil, docdom.DocChange{}, false, err
	}
	src, err := docdom.ValidateSource(in.Source)
	if err != nil {
		return nil, docdom.DocChange{}, false, err
	}
	for range maxPathAttempts {
		res, err := s.repo.ResolveDocPath(ctx, in.ProjectID, folders, title)
		if err != nil {
			return nil, docdom.DocChange{}, false, err
		}
		if res.IsFolder {
			return nil, docdom.DocChange{}, false, docdom.ErrDocPathIsFolder
		}
		if in.Authorize != nil {
			if err := in.Authorize(ctx, res.FolderID, res.Doc); err != nil {
				return nil, docdom.DocChange{}, false, err
			}
		}
		if res.Doc == nil && in.IfMatch != nil {
			// The caller names a version of a document that is not there
			// (deleted, moved, or never created): the precondition fails.
			return nil, docdom.DocChange{}, false, &docdom.VersionConflictError{}
		}
		d, change, created, err := s.writeAtPath(ctx, in, src, folders, title, *res)
		if errors.Is(err, docdom.ErrDocPathChanged) {
			continue
		}
		return d, change, created, err
	}
	return nil, docdom.DocChange{}, false, docdom.ErrDocPathChanged
}

func (s *Service) writeAtPath(ctx context.Context, in docdom.UpsertByPathInput, src *docdom.DocSource, folders []string, title string, expect docdom.DocPathResolution) (*docdom.Document, docdom.DocChange, bool, error) {
	content := in.Content
	u := s.newUpdate(ctx, in.ProjectID, docdom.UpdateDocumentInput{
		Content: &content, UpdatedBy: in.Actor, IfMatch: in.IfMatch, Source: src, Lock: in.Lock,
	})
	actor := u.updatedBy
	locked := in.Lock != nil && *in.Lock
	if locked && src == nil && (expect.Doc == nil || expect.Doc.Source == nil) {
		return nil, docdom.DocChange{}, false, docdom.ErrDocLockNeedsSource
	}
	now := storedNow()
	out, err := s.repo.WriteDocumentAtPath(ctx, docdom.DocPathWrite{
		ProjectID: in.ProjectID, Folders: folders, Title: title, Expect: expect,
		NewFolder: func() *docdom.DocFolder {
			return &docdom.DocFolder{ID: uuid.New(), CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
		},
		NewDoc: func() *docdom.Document {
			hash := docdom.ContentHash(content)
			return &docdom.Document{
				ID: uuid.New(), Content: content, ContentHash: &hash,
				CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now,
				Source: src, SourceLocked: locked,
			}
		},
		Decide: u.decide,
	})
	if err != nil {
		return nil, docdom.DocChange{}, false, err
	}
	for _, f := range out.Folders {
		s.announceFolder(ctx, string(docdom.ActivityTypeFolderCreated), f)
	}
	if out.Created {
		return out.Doc, docdom.DocChange{Content: true, Title: true, Source: src != nil, Lock: locked}, true, nil
	}
	s.snapshotOld(ctx, out.Doc, u)
	return out.Doc, u.change, false, nil
}

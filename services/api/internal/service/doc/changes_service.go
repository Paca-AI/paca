package docsvc

import (
	"context"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// ChangesService is the concrete implementation of docdom.ChangesService.
type ChangesService struct {
	repo docdom.ChangeRepository
}

// NewChangesService returns the change feed service over repo.
func NewChangesService(repo docdom.ChangeRepository) *ChangesService {
	return &ChangesService{repo: repo}
}

// Changes implements docdom.ChangesService.
//
// Everything is read from one snapshot, sequence first. The reply's cursor
// carries that sequence and a fingerprint of the documents visible at it. On
// the next pull the fingerprint is recomputed for the same documents (those
// alive at the cursor's sequence). If it still matches, no document the
// client knows about changed visibility, and the rows written after the
// cursor are the whole story. If it differs (a role edit, a folder or
// document moved under a denied folder, or back out), the reply is a reset: the
// full list of what is visible now, from which the client drops the rest. A
// reset never lists a document the caller cannot see, deleted or not.
func (s *ChangesService) Changes(ctx context.Context, projectID uuid.UUID, cursor string) (*docdom.ChangeSet, error) {
	var set docdom.ChangeSet
	err := s.repo.ReadDocChanges(ctx, func(r docdom.ChangeReader) error {
		seq, foldersSeq, err := r.DocSyncState(ctx, projectID)
		if err != nil {
			return err
		}
		live, err := r.VisibleFingerprint(ctx, projectID, nil)
		if err != nil {
			return err
		}
		set = docdom.ChangeSet{
			Cursor: docdom.EncodeChangeCursor(docdom.ChangeCursor{ProjectID: projectID, Seq: seq, Fingerprint: live}),
		}

		since, ok := docdom.DecodeChangeCursor(cursor)
		set.Reset = !ok || since.ProjectID != projectID || since.Seq > seq
		switch {
		case set.Reset:
		case since.Seq == seq:
			// Nothing was written since: "alive at the cursor" is "alive now".
			set.Reset = since.Fingerprint != live
		default:
			then, err := r.VisibleFingerprint(ctx, projectID, &since.Seq)
			if err != nil {
				return err
			}
			set.Reset = since.Fingerprint != then
		}

		switch {
		case set.Reset:
			if set.Docs, err = r.ListVisibleDocs(ctx, projectID); err != nil {
				return err
			}
		case since.Seq < seq:
			if set.Docs, err = r.ListDocChanges(ctx, projectID, since.Seq); err != nil {
				return err
			}
		}
		if set.Reset || foldersSeq > since.Seq {
			if set.Folders, err = r.ListAllFolders(ctx, projectID); err != nil {
				return err
			}
			set.FoldersIncluded = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &set, nil
}

// BatchGet implements docdom.ChangesService.
func (s *ChangesService) BatchGet(ctx context.Context, projectID uuid.UUID, ids []uuid.UUID) ([]*docdom.Document, error) {
	if len(ids) > docdom.MaxBatchGetIDs {
		return nil, docdom.ErrTooManyDocIDs
	}
	if len(ids) == 0 {
		return []*docdom.Document{}, nil
	}
	found, err := s.repo.FindDocumentsByIDs(ctx, projectID, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]*docdom.Document, len(found))
	for _, d := range found {
		byID[d.ID] = d
	}
	out := make([]*docdom.Document, 0, len(found))
	for _, id := range ids {
		if d, ok := byID[id]; ok {
			out = append(out, d)
			delete(byID, id) // a repeated id is returned once
		}
	}
	return out, nil
}

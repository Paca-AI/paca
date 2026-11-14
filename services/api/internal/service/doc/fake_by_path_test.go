package docsvc_test

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// In-memory by-path resolution and writes for fakeDocRepo, matching the
// real repository's rules (case-insensitive names, one write at a time).

func sameParent(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (r *fakeDocRepo) resolveLocked(projectID uuid.UUID, folders []string, title string) *docdom.DocPathResolution {
	res := &docdom.DocPathResolution{}
	var parent *uuid.UUID
	for _, name := range folders {
		var next *uuid.UUID
		for _, f := range r.folders {
			if f.ProjectID == projectID && sameParent(f.ParentID, parent) && strings.EqualFold(f.Name, name) {
				id := f.ID
				next = &id
				break
			}
		}
		if next == nil {
			return res
		}
		parent = next
		res.Depth++
		res.FolderID = next
	}
	for _, d := range r.docs {
		if d.ProjectID == projectID && d.DeletedAt == nil && sameParent(d.FolderID, parent) && strings.EqualFold(d.Title, title) {
			cp := *d
			res.Doc = &cp
			return res
		}
	}
	for _, f := range r.folders {
		if f.ProjectID == projectID && sameParent(f.ParentID, parent) && strings.EqualFold(f.Name, title) {
			res.IsFolder = true
		}
	}
	return res
}

func (r *fakeDocRepo) ResolveDocPath(_ context.Context, projectID uuid.UUID, folders []string, title string) (*docdom.DocPathResolution, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.resolveLocked(projectID, folders, title), nil
}

func (r *fakeDocRepo) WriteDocumentAtPath(ctx context.Context, w docdom.DocPathWrite) (*docdom.DocPathWriteResult, error) {
	r.mu.Lock()
	res := r.resolveLocked(w.ProjectID, w.Folders, w.Title)
	if res.Depth != w.Expect.Depth || !sameParent(res.FolderID, w.Expect.FolderID) || (res.Doc == nil) != (w.Expect.Doc == nil) ||
		(res.Doc != nil && res.Doc.ID != w.Expect.Doc.ID) {
		r.mu.Unlock()
		return nil, docdom.ErrDocPathChanged
	}
	if res.IsFolder {
		r.mu.Unlock()
		return nil, docdom.ErrDocPathIsFolder
	}
	out := &docdom.DocPathWriteResult{}
	parent := res.FolderID
	for _, name := range w.Folders[res.Depth:] {
		f := w.NewFolder()
		f.ProjectID, f.ParentID, f.Name = w.ProjectID, parent, name
		cp := *f
		r.folders[f.ID] = &cp
		out.Folders = append(out.Folders, f)
		id := f.ID
		parent = &id
	}
	if res.Doc == nil {
		d := w.NewDoc()
		d.ProjectID, d.FolderID, d.Title = w.ProjectID, parent, w.Title
		cp := *d
		r.docs[d.ID] = &cp
		r.mu.Unlock()
		out.Doc, out.Created = d, true
		return out, nil
	}
	r.mu.Unlock()
	d, err := r.UpdateDocumentAtomic(ctx, res.Doc.ID, w.Decide)
	if err != nil && !errors.Is(err, docdom.ErrNoChange) {
		return nil, err
	}
	out.Doc = d
	return out, nil
}

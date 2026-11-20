package dto

import (
	"time"

	"github.com/google/uuid"

	filedom "github.com/Paca-AI/api/internal/domain/file"
)

// CreateFileFolderRequest is the body of POST .../files/folders.
type CreateFileFolderRequest struct {
	ParentID *uuid.UUID `json:"parent_id"`
	Name     string     `json:"name" binding:"required"`
}

// UpdateFileNodeRequest is the body of PATCH .../files/nodes/{id}. Name renames;
// a present parent_id moves (null moves to the root), so the move flag tells
// "move to the root" from "do not move".
type UpdateFileNodeRequest struct {
	Name     *string    `json:"name"`
	Move     bool       `json:"move"`
	ParentID *uuid.UUID `json:"parent_id"`
}

// FileUploadRequest is the body of POST .../files/uploads/initiate.
type FileUploadRequest struct {
	ParentID    *uuid.UUID `json:"parent_id"`
	FileName    string     `json:"file_name"    binding:"required"`
	ContentType string     `json:"content_type"`
	FileSize    int64      `json:"file_size"    binding:"required,min=1"`
}

// FileUploadCompleteRequest is the body of POST .../files/uploads/complete.
type FileUploadCompleteRequest struct {
	FileID uuid.UUID `json:"file_id" binding:"required"`
	// UploadID and Parts are required only for multipart uploads.
	UploadID *string                `json:"upload_id"`
	Parts    []CompletedPartRequest `json:"parts"`
}

// StarFileNodeRequest is the body of PUT .../files/nodes/{id}/star.
type StarFileNodeRequest struct {
	Starred bool `json:"starred"`
}

// FileNodeResponse is one folder or file.
type FileNodeResponse struct {
	ID          uuid.UUID  `json:"id"`
	ProjectID   *uuid.UUID `json:"project_id"`
	ParentID    *uuid.UUID `json:"parent_id"`
	Kind        string     `json:"kind"`
	Name        string     `json:"name"`
	Source      string     `json:"source,omitempty"`
	ContentType string     `json:"content_type,omitempty"`
	Size        int64      `json:"size"`
	OwnerID     *uuid.UUID `json:"owner_id"`
	OwnerName   string     `json:"owner_name,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
	AccessedAt  *time.Time `json:"accessed_at,omitempty"`
	Starred     bool       `json:"starred"`
	// System is "task_attachments" or "task" for the product-managed folders.
	System string `json:"system,omitempty"`
	// Area is "docs" for the Documentation folder, what is inside it, and the
	// documents there: the docs actions apply as well as the files ones.
	Area string `json:"area,omitempty"`
	// Locked nodes cannot be renamed or moved; deleting one removes a task
	// attachment for good rather than trashing it. A document is not locked
	// here: it is renamed and moved through the docs API (the file routes
	// still refuse it).
	Locked bool `json:"locked"`
}

// FileNodeFromEntity maps a node to its response.
func FileNodeFromEntity(n *filedom.Node) FileNodeResponse {
	return FileNodeResponse{
		ID: n.ID, ProjectID: n.ProjectID, ParentID: n.ParentID, Kind: string(n.Kind), Name: n.Name, Source: n.Source,
		ContentType: n.ContentType, Size: n.Size, OwnerID: n.CreatedBy, OwnerName: n.OwnerName,
		CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt, DeletedAt: n.DeletedAt, AccessedAt: n.AccessedAt,
		Starred: n.Starred, System: n.SystemKey, Area: n.Area, Locked: n.Locked() && n.Kind != filedom.KindDoc,
	}
}

// FileNodesFromEntities maps nodes to responses, never returning nil.
func FileNodesFromEntities(nodes []*filedom.Node) []FileNodeResponse {
	out := make([]FileNodeResponse, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, FileNodeFromEntity(n))
	}
	return out
}

// FileRootResponse is one top-level entry of the general Files view.
type FileRootResponse struct {
	// Kind is "general" for the workspace's own files, "project" for a
	// project's.
	Kind      string     `json:"kind"`
	ProjectID *uuid.UUID `json:"project_id,omitempty"`
	Name      string     `json:"name"`
}

// FileDownloadResponse is a presigned URL plus what a viewer needs to render it.
type FileDownloadResponse struct {
	URL         string    `json:"url"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// FileRelationsResponse is where else a file is used.
type FileRelationsResponse struct {
	Task      *FileTaskRef      `json:"task"`
	Documents []FileDocumentRef `json:"documents"`
}

// FileTaskRef names the task a file is attached to.
type FileTaskRef struct {
	ID     uuid.UUID `json:"id"`
	Key    string    `json:"key"`
	Number int64     `json:"number"`
	Title  string    `json:"title"`
}

// FileDocumentRef names a document that references a file.
type FileDocumentRef struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}

// FileRelationsFromEntity maps relations to their response.
func FileRelationsFromEntity(r *filedom.Relations) FileRelationsResponse {
	out := FileRelationsResponse{Documents: make([]FileDocumentRef, 0, len(r.Documents))}
	if r.Task != nil {
		out.Task = &FileTaskRef{ID: r.Task.ID, Key: r.Task.Key, Number: r.Task.Number, Title: r.Task.Title}
	}
	for _, d := range r.Documents {
		out.Documents = append(out.Documents, FileDocumentRef{ID: d.ID, Title: d.Title})
	}
	return out
}

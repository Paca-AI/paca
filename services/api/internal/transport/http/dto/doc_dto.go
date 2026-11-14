package dto

import (
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// =============================================================================
// Folder DTOs
// =============================================================================

// CreateFolderRequest is the body for POST /projects/:projectId/docs/folders.
type CreateFolderRequest struct {
	Name     string     `json:"name" binding:"required"`
	ParentID *uuid.UUID `json:"parent_id"`
}

// UpdateFolderRequest is the body for PATCH /projects/:projectId/docs/folders/:folderId.
type UpdateFolderRequest struct {
	Name     string       `json:"name"`
	ParentID OptionalUUID `json:"parent_id"`
	Position *int         `json:"position"`
}

// DocFolderResponse is the public representation of a doc folder.
type DocFolderResponse struct {
	ID        uuid.UUID  `json:"id"`
	ProjectID uuid.UUID  `json:"project_id"`
	ParentID  *uuid.UUID `json:"parent_id,omitempty"`
	Name      string     `json:"name"`
	Position  int        `json:"position"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// DocFolderFromEntity maps a domain DocFolder to a DocFolderResponse DTO.
func DocFolderFromEntity(f *docdom.DocFolder) DocFolderResponse {
	return DocFolderResponse{
		ID:        f.ID,
		ProjectID: f.ProjectID,
		ParentID:  f.ParentID,
		Name:      f.Name,
		Position:  f.Position,
		CreatedAt: f.CreatedAt,
		UpdatedAt: f.UpdatedAt,
	}
}

// =============================================================================
// Document DTOs
// =============================================================================

// CreateDocumentRequest is the body for POST /projects/:projectId/docs.
type CreateDocumentRequest struct {
	Title    string          `json:"title"`
	FolderID *uuid.UUID      `json:"folder_id"`
	Content  json.RawMessage `json:"content"`
	WriteNote
}

// Limits on a write's note, client label and task reference, in characters.
const (
	MaxWriteNoteLen   = 500
	MaxWriteClientLen = 200
	MaxWriteTaskLen   = 50
)

// WriteNote is the optional plain text a document write carries: why it was
// made (Note), what made it (Client, an opaque label such as
// "claude-code/<session>", never used for authorization) and the task it
// belongs to (Task, free text such as "PACA-88"). It is stored with the
// write's activity entry.
type WriteNote struct {
	Note   string `json:"note"`
	Client string `json:"client"`
	Task   string `json:"task"`
}

// Validate checks the length limits.
func (n WriteNote) Validate() error {
	for _, f := range []struct {
		name, value string
		max         int
	}{{"note", n.Note, MaxWriteNoteLen}, {"client", n.Client, MaxWriteClientLen}, {"task", n.Task, MaxWriteTaskLen}} {
		if utf8.RuneCountInString(f.value) > f.max {
			return apierr.New(apierr.CodeDocNoteInvalid, fmt.Sprintf("%s: at most %d characters", f.name, f.max))
		}
	}
	return nil
}

// Fields returns the non-empty values keyed by their JSON names, for an
// activity entry's content.
func (n WriteNote) Fields() map[string]string {
	out := map[string]string{}
	if n.Note != "" {
		out["note"] = n.Note
	}
	if n.Client != "" {
		out["client"] = n.Client
	}
	if n.Task != "" {
		out["task"] = n.Task
	}
	return out
}

// UpdateDocumentRequest is the body for PATCH /projects/:projectId/docs/:docId.
type UpdateDocumentRequest struct {
	Title    *string      `json:"title"`
	Content  OptionalJSON `json:"content"`
	FolderID OptionalUUID `json:"folder_id"`
	Position *int         `json:"position"`
	// SourceLocked locks (true) or unlocks (false) a managed copy. The source
	// itself is set only by a push to the document's path.
	SourceLocked *bool `json:"source_locked"`
	WriteNote
}

// PutDocumentByPathRequest is the body for PUT /projects/:projectId/docs/by-path.
type PutDocumentByPathRequest struct {
	// Path is "Folder/Sub folder/Title": segments are trimmed, empty ones
	// dropped, the last is the title. Missing folders are created.
	Path    string          `json:"path"`
	Content json.RawMessage `json:"content"`
	// Source names the repository file this document is a managed copy of.
	Source *DocSourceRequest `json:"source"`
	// Lock sets source_locked; locking needs Source.
	Lock *bool `json:"lock"`
	WriteNote
}

// DocSourceRequest is the source a by-path write sets.
type DocSourceRequest struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
	Commit     string `json:"commit"`
}

// Entity maps the request source to the domain type (nil stays nil).
func (s *DocSourceRequest) Entity() *docdom.DocSource {
	if s == nil {
		return nil
	}
	return &docdom.DocSource{Repository: s.Repository, Path: s.Path, Commit: s.Commit}
}

// DocumentWriteResponse is a document written by path, plus whether the write
// changed what a reader sees.
type DocumentWriteResponse struct {
	DocumentResponse
	Changed bool `json:"changed"`
}

// DocumentResponse is the full public representation of a document (including content).
type DocumentResponse struct {
	ID        uuid.UUID       `json:"id"`
	ProjectID uuid.UUID       `json:"project_id"`
	FolderID  *uuid.UUID      `json:"folder_id,omitempty"`
	Title     string          `json:"title"`
	Content   json.RawMessage `json:"content"`
	// ContentHash identifies the content version: the same blocks give the
	// same hash whatever their block ids. It is the If-Match token for writes
	// and the first part of the ETag.
	ContentHash string     `json:"content_hash"`
	Position    int        `json:"position"`
	CreatedBy   *uuid.UUID `json:"created_by,omitempty"`
	UpdatedBy   *uuid.UUID `json:"updated_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	// Source names the repository file a managed copy is pushed from; absent
	// for a document created in Paca.
	Source       *DocSourceResponse `json:"source,omitempty"`
	SourceLocked bool               `json:"source_locked"`
}

// DocSourceResponse is the repository file a managed document copy comes from.
type DocSourceResponse struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
	Commit     string `json:"commit,omitempty"`
}

func docSourceResponse(src *docdom.DocSource) *DocSourceResponse {
	if src == nil {
		return nil
	}
	return &DocSourceResponse{Repository: src.Repository, Path: src.Path, Commit: src.Commit}
}

// DocumentListItemResponse is a lightweight representation used in list responses
// (content is omitted to keep responses lean).
type DocumentListItemResponse struct {
	ID        uuid.UUID  `json:"id"`
	ProjectID uuid.UUID  `json:"project_id"`
	FolderID  *uuid.UUID `json:"folder_id,omitempty"`
	Title     string     `json:"title"`
	Position  int        `json:"position"`
	CreatedBy *uuid.UUID `json:"created_by,omitempty"`
	UpdatedBy *uuid.UUID `json:"updated_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	// Source and SourceLocked let the tree mark managed copies.
	Source       *DocSourceResponse `json:"source,omitempty"`
	SourceLocked bool               `json:"source_locked"`
}

// DocumentFromEntity maps a domain Document to a DocumentResponse DTO.
func DocumentFromEntity(d *docdom.Document) DocumentResponse {
	return DocumentResponse{
		ID:          d.ID,
		ProjectID:   d.ProjectID,
		FolderID:    d.FolderID,
		Title:       d.Title,
		Content:     d.Content,
		ContentHash: d.Hash(),
		Position:    d.Position,
		CreatedBy:   d.CreatedBy,
		UpdatedBy:   d.UpdatedBy,
		CreatedAt:   d.CreatedAt,
		UpdatedAt:   d.UpdatedAt,

		Source:       docSourceResponse(d.Source),
		SourceLocked: d.SourceLocked,
	}
}

// DocumentListItemFromEntity maps a domain Document to a DocumentListItemResponse DTO.
func DocumentListItemFromEntity(d *docdom.Document) DocumentListItemResponse {
	return DocumentListItemResponse{
		ID:        d.ID,
		ProjectID: d.ProjectID,
		FolderID:  d.FolderID,
		Title:     d.Title,
		Position:  d.Position,
		CreatedBy: d.CreatedBy,
		UpdatedBy: d.UpdatedBy,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,

		Source:       docSourceResponse(d.Source),
		SourceLocked: d.SourceLocked,
	}
}

// =============================================================================
// Snapshot DTOs
// =============================================================================

// DocSnapshotResponse is the public representation of a document snapshot.
type DocSnapshotResponse struct {
	ID             uuid.UUID       `json:"id"`
	DocumentID     uuid.UUID       `json:"document_id"`
	Title          string          `json:"title"`
	Content        json.RawMessage `json:"content,omitempty"`
	ContentHash    *string         `json:"content_hash,omitempty"` // absent on snapshots taken before hashes were stored
	SnapshotNumber int64           `json:"snapshot_number"`
	CreatedBy      *uuid.UUID      `json:"created_by,omitempty"`
	CreatedByName  string          `json:"created_by_name,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// DocSnapshotFromEntity maps a domain DocSnapshot to a DocSnapshotResponse DTO.
func DocSnapshotFromEntity(s *docdom.DocSnapshot) DocSnapshotResponse {
	return DocSnapshotResponse{
		ID:             s.ID,
		DocumentID:     s.DocumentID,
		Title:          s.Title,
		Content:        s.Content,
		ContentHash:    s.ContentHash,
		SnapshotNumber: s.SnapshotNumber,
		CreatedBy:      s.CreatedBy,
		CreatedByName:  s.CreatedByName,
		CreatedAt:      s.CreatedAt,
	}
}

// =============================================================================
// Activity DTOs
// =============================================================================

// AddDocCommentRequest is the body for POST /projects/:projectId/docs/:docId/comments.
type AddDocCommentRequest struct {
	Content json.RawMessage `json:"content" binding:"required"`
}

// UpdateDocCommentRequest is the body for PATCH /projects/:projectId/docs/:docId/comments/:commentId.
type UpdateDocCommentRequest struct {
	Content json.RawMessage `json:"content" binding:"required"`
}

// DocActivityResponse is the public representation of a doc activity entry.
type DocActivityResponse struct {
	ID            uuid.UUID  `json:"id"`
	DocumentID    uuid.UUID  `json:"document_id"`
	ActorID       *uuid.UUID `json:"actor_id,omitempty"`
	ActorName     string     `json:"actor_name,omitempty"`
	ActorUsername string     `json:"actor_username,omitempty"`
	// ActorAvatarURL/ActorAvatarThumbURL are presigned GET URLs, populated by
	// the handler (not this mapper) via attachmentdom.AvatarService — nil
	// when the actor has no avatar.
	ActorAvatarURL      *string         `json:"actor_avatar_url,omitempty"`
	ActorAvatarThumbURL *string         `json:"actor_avatar_thumb_url,omitempty"`
	ActivityType        string          `json:"activity_type"`
	Content             json.RawMessage `json:"content"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	DeletedAt           *time.Time      `json:"deleted_at,omitempty"`
}

// DocActivityFromEntity maps a domain Activity to a DocActivityResponse DTO.
func DocActivityFromEntity(a *docdom.Activity) DocActivityResponse {
	return DocActivityResponse{
		ID:            a.ID,
		DocumentID:    a.DocumentID,
		ActorID:       a.ActorID,
		ActorName:     a.ActorName,
		ActorUsername: a.ActorUsername,
		ActivityType:  string(a.ActivityType),
		Content:       a.Content,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
		DeletedAt:     a.DeletedAt,
	}
}

// DocumentSearchHitResponse is one result of a title/content document search:
// list-item metadata plus where the query matched and an excerpt (empty for
// title matches). Content is never included.
type DocumentSearchHitResponse struct {
	ID        uuid.UUID  `json:"id"`
	ProjectID uuid.UUID  `json:"project_id"`
	FolderID  *uuid.UUID `json:"folder_id,omitempty"`
	Title     string     `json:"title"`
	MatchedIn string     `json:"matched_in"`
	Snippet   string     `json:"snippet,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// DocumentSearchHitFromEntity maps a domain SearchHit to its DTO.
func DocumentSearchHitFromEntity(h docdom.SearchHit) DocumentSearchHitResponse {
	return DocumentSearchHitResponse{
		ID:        h.Document.ID,
		ProjectID: h.Document.ProjectID,
		FolderID:  h.Document.FolderID,
		Title:     h.Document.Title,
		MatchedIn: h.MatchedIn,
		Snippet:   h.Snippet,
		UpdatedAt: h.Document.UpdatedAt,
	}
}

package docdom

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"

	"github.com/google/uuid"
)

// The document change feed: what a client that mirrors a project's documents
// needs to catch up since its last pull. See docs/api/doc-change-feed.md.

// MaxBatchGetIDs is the most documents one batch read returns.
const MaxBatchGetIDs = 100

// maxChangeCursorLen bounds what DecodeChangeCursor will look at. A real
// cursor is about 150 characters.
const maxChangeCursorLen = 1024

// ChangeCursor is a position in a project's change feed.
type ChangeCursor struct {
	// ProjectID is the project the cursor was issued for.
	ProjectID uuid.UUID `json:"p"`
	// Seq is the project's change sequence (doc_sync_state.seq) the client has
	// caught up to.
	Seq int64 `json:"s"`
	// Fingerprint identifies the set of documents the caller could see at Seq
	// (see ChangeReader.VisibleFingerprint). The next pull recomputes it for
	// the same documents: a different value means a document became visible or
	// invisible without being written, which seq alone cannot show.
	Fingerprint string `json:"f"`
}

// EncodeChangeCursor returns the opaque form of c: base64url of its JSON.
func EncodeChangeCursor(c ChangeCursor) string {
	raw, _ := json.Marshal(c) // a struct of a UUID, an int and a string cannot fail
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeChangeCursor parses a cursor a client sent back. ok is false for
// anything that is not a well-formed cursor; the caller answers that with a
// reset, never an error, so a client with a corrupt or outdated cursor
// recovers on its own. The cursor is not signed: it grants nothing, because
// every reply is filtered by what the caller may read now.
func DecodeChangeCursor(s string) (c ChangeCursor, ok bool) {
	if s == "" || len(s) > maxChangeCursorLen {
		return ChangeCursor{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return ChangeCursor{}, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&c); err != nil {
		return ChangeCursor{}, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return ChangeCursor{}, false
	}
	if c.ProjectID == uuid.Nil || c.Seq < 0 || c.Fingerprint == "" {
		return ChangeCursor{}, false
	}
	return c, true
}

// ChangeSet is one reply of the change feed.
type ChangeSet struct {
	// Cursor is what the client sends as "since" on its next pull.
	Cursor string
	// Reset means Docs is the full list of documents the caller can see, not
	// a list of changes: the client drops every document that is not in it.
	Reset bool
	// Docs are the changed documents (or all visible ones on a reset), without
	// their bodies. A document with DeletedAt set is a tombstone: it was
	// deleted since the cursor. ContentHash is always set on a live document.
	Docs []*Document
	// FoldersIncluded says whether Folders is the project's folder tree (a
	// folder changed since the cursor, or this is a reset). When false the
	// tree is unchanged and Folders is nil.
	FoldersIncluded bool
	Folders         []*DocFolder
}

// ChangeReader reads the change feed's inputs from one consistent snapshot of
// the database (see ChangeRepository.ReadDocChanges). Every method that
// returns documents applies the caller's IAM scope for "doc" from ctx.
type ChangeReader interface {
	// DocSyncState returns the project's change sequence and the sequence of
	// its last folder change (both zero for a project with no documents or
	// folders yet).
	DocSyncState(ctx context.Context, projectID uuid.UUID) (seq, foldersSeq int64, err error)
	// VisibleFingerprint returns a hash of the ids of the documents the caller
	// can see. With upToSeq nil it covers every live document. With upToSeq
	// set it covers the documents that were alive at that sequence: created at
	// or before it, and either not deleted or deleted after it. Visibility is
	// always judged as it is now, which is the point: the same set of
	// documents hashing differently means visibility changed.
	VisibleFingerprint(ctx context.Context, projectID uuid.UUID, upToSeq *int64) (string, error)
	// ListDocChanges returns the documents written after sinceSeq that the
	// caller can see, oldest change first, without their bodies. A deleted
	// document is included (as a tombstone) only when it existed at sinceSeq;
	// one created and deleted since then was never known to the client.
	ListDocChanges(ctx context.Context, projectID uuid.UUID, sinceSeq int64) ([]*Document, error)
	// ListVisibleDocs returns every live document the caller can see, without
	// their bodies.
	ListVisibleDocs(ctx context.Context, projectID uuid.UUID) ([]*Document, error)
	// ListAllFolders returns the project's folder tree.
	ListAllFolders(ctx context.Context, projectID uuid.UUID) ([]*DocFolder, error)
}

// ChangeRepository is the persistence contract of the change feed and the
// batch read.
type ChangeRepository interface {
	// ReadDocChanges calls fn with a reader bound to one read-only snapshot,
	// so the sequence, the fingerprints and the lists fn reads describe the
	// same moment: a write that commits while fn runs is in none of them and
	// arrives with the next pull.
	ReadDocChanges(ctx context.Context, fn func(r ChangeReader) error) error
	// FindDocumentsByIDs returns the live documents of the project among ids
	// that the caller can see (IAM scope for "doc" from ctx), with their
	// bodies, in no particular order. Unknown, deleted, foreign and invisible
	// ids are left out without an error.
	FindDocumentsByIDs(ctx context.Context, projectID uuid.UUID, ids []uuid.UUID) ([]*Document, error)
}

// ChangesService defines the change feed use-cases.
type ChangesService interface {
	// Changes returns what changed in the project's documents since cursor.
	// An empty, malformed or foreign cursor, or one whose visibility
	// fingerprint no longer matches, gets a reset instead of an error.
	Changes(ctx context.Context, projectID uuid.UUID, cursor string) (*ChangeSet, error)
	// BatchGet returns the documents among ids the caller can see, with their
	// bodies, in the order asked for and without duplicates. More than
	// MaxBatchGetIDs ids is ErrTooManyDocIDs.
	BatchGet(ctx context.Context, projectID uuid.UUID, ids []uuid.UUID) ([]*Document, error)
}

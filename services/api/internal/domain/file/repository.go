package filedom

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ListParams selects and orders the nodes of one listing.
type ListParams struct {
	Scope Scope
	// ParentID is the folder to list; nil lists the scope root. Ignored when
	// Query is set (a search covers the whole scope).
	ParentID *uuid.UUID
	// UserID is the caller, for the Starred flag.
	UserID uuid.UUID
	Query  string
	Type   string
	SortBy string
	Desc   bool
	Limit  int
	Offset int
}

// Repository is the persistence contract of the file manager.
type Repository interface {
	// GetNode returns the node, trashed or not, with the caller's star state.
	GetNode(ctx context.Context, id, userID uuid.UUID) (*Node, error)
	// List returns one page of live nodes (folders first) and the total.
	List(ctx context.Context, p ListParams) (nodes []*Node, total int, err error)
	ListStarred(ctx context.Context, scope Scope, userID uuid.UUID, limit, offset int) ([]*Node, int, error)
	ListRecent(ctx context.Context, scope Scope, userID uuid.UUID, limit int) ([]*Node, error)
	// ListTrash returns the roots of the trashed subtrees of the scope.
	ListTrash(ctx context.Context, scope Scope, userID uuid.UUID, limit, offset int) ([]*Node, int, error)
	// Path returns the ancestors of the node and the node itself, root first.
	Path(ctx context.Context, id uuid.UUID) ([]*Node, error)
	// CreateFolder inserts a folder.
	CreateFolder(ctx context.Context, n *Node) error
	// Rename and Move change a folder or a file (n.Kind says which).
	Rename(ctx context.Context, n *Node, name string) error
	Move(ctx context.Context, n *Node, parentID *uuid.UUID) error
	// IsSelfOrDescendant reports whether candidate is the folder or lies below it.
	IsSelfOrDescendant(ctx context.Context, folder, candidate uuid.UUID) (bool, error)
	// Trash marks the node, and for a folder everything below it, trashed
	// with one shared timestamp; Restore reverses it. Restore reparents to
	// the scope root when the parent is still trashed.
	Trash(ctx context.Context, n *Node, by uuid.UUID, at time.Time) error
	Restore(ctx context.Context, n *Node) error
	// PurgeSubtree permanently removes the node (and a folder's contents),
	// unlinking any task attachment that uses a file in the same transaction, and
	// returns the objects to delete from the object store.
	PurgeSubtree(ctx context.Context, n *Node) ([]StorageObject, error)
	// PurgeTrashedBefore does the same for every trashed subtree older than t.
	PurgeTrashedBefore(ctx context.Context, t time.Time, limit int) ([]StorageObject, error)
	SetStarred(ctx context.Context, userID, itemID uuid.UUID, starred bool) error
	// Relations returns the task and documents that use a project file.
	Relations(ctx context.Context, projectID, fileID uuid.UUID) (*Relations, error)
	// Touch records that the user opened the item.
	Touch(ctx context.Context, userID, itemID uuid.UUID) error
}

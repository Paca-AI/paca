package activitysvc

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	activitydom "github.com/Paca-AI/api/internal/domain/activity"
)

// MemoryRepository is an in-memory activitydom.Repository for tests and for
// wiring a service without Postgres. It resolves no actor or entity titles.
type MemoryRepository struct {
	mu   sync.Mutex
	rows map[uuid.UUID]*activitydom.Activity
}

// NewMemoryRepository returns an empty MemoryRepository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{rows: map[uuid.UUID]*activitydom.Activity{}}
}

// Create stores a copy of a; an existing ID is left untouched.
func (r *MemoryRepository) Create(_ context.Context, a *activitydom.Activity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.rows[a.ID]; ok {
		return nil
	}
	cp := *a
	r.rows[a.ID] = &cp
	return nil
}

// List returns matching entries newest first.
func (r *MemoryRepository) List(_ context.Context, f activitydom.ListFilter, limit int) ([]*activitydom.Activity, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*activitydom.Activity
	for _, a := range r.rows {
		if matches(a, f) {
			cp := *a
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID.String() > out[j].ID.String()
	})
	if len(out) > limit {
		return out[:limit], true, nil
	}
	return out, false, nil
}

func matches(a *activitydom.Activity, f activitydom.ListFilter) bool {
	switch {
	case a.ProjectID != f.ProjectID, a.DeletedAt != nil:
		return false
	case len(f.EntityTypes) > 0 && !slices.Contains(f.EntityTypes, a.EntityType):
		return false
	case len(f.Origins) > 0 && !slices.Contains(f.Origins, a.Origin):
		return false
	case len(f.ActivityTypes) > 0 && !slices.Contains(f.ActivityTypes, a.ActivityType):
		return false
	case len(f.ActorMemberIDs) > 0 && (a.ActorID == nil || !slices.Contains(f.ActorMemberIDs, *a.ActorID)):
		return false
	case f.CreatedAfter != nil && a.CreatedAt.Before(*f.CreatedAfter):
		return false
	case f.CreatedBefore != nil && !a.CreatedAt.Before(*f.CreatedBefore):
		return false
	case f.Search != "" && !strings.Contains(strings.ToLower(a.EntityTitle+" "+string(a.Content)), strings.ToLower(f.Search)):
		return false
	case f.Cursor != nil && !(a.CreatedAt.Before(f.Cursor.CreatedAt) ||
		(a.CreatedAt.Equal(f.Cursor.CreatedAt) && a.ID.String() < f.Cursor.ID.String())):
		return false
	}
	return true
}

// ListForEntity returns one entity's non-deleted entries oldest first.
func (r *MemoryRepository) ListForEntity(_ context.Context, entityType string, entityID uuid.UUID) ([]*activitydom.Activity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*activitydom.Activity
	for _, a := range r.rows {
		if a.EntityType == entityType && a.EntityIDOrNil() == entityID && a.DeletedAt == nil {
			cp := *a
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// FindByID returns a copy of one entry or activitydom.ErrNotFound.
func (r *MemoryRepository) FindByID(_ context.Context, id uuid.UUID) (*activitydom.Activity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.rows[id]
	if !ok {
		return nil, activitydom.ErrNotFound
	}
	cp := *a
	return &cp, nil
}

// UpdateContent replaces an entry's content.
func (r *MemoryRepository) UpdateContent(_ context.Context, id uuid.UUID, content json.RawMessage, updatedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a, ok := r.rows[id]; ok {
		a.Content = content
		a.UpdatedAt = updatedAt
	}
	return nil
}

// SoftDelete marks an entry deleted.
func (r *MemoryRepository) SoftDelete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a, ok := r.rows[id]; ok {
		now := time.Now()
		a.DeletedAt = &now
	}
	return nil
}

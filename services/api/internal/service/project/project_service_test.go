package projectsvc

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	projectdom "github.com/paca/api/internal/domain/project"
	taskdom "github.com/paca/api/internal/domain/task"
)

// ---------------------------------------------------------------------------
// Minimal fake project repository
// ---------------------------------------------------------------------------

type fakeProjectRepo struct {
	mu       sync.Mutex
	projects map[uuid.UUID]*projectdom.Project
	roles    map[uuid.UUID]*projectdom.ProjectRole
	members  []projectdom.ProjectMember
}

func newFakeProjectRepo() *fakeProjectRepo {
	return &fakeProjectRepo{
		projects: make(map[uuid.UUID]*projectdom.Project),
		roles:    make(map[uuid.UUID]*projectdom.ProjectRole),
	}
}

func (r *fakeProjectRepo) List(_ context.Context, _, _ int) ([]*projectdom.Project, int64, error) {
	return nil, 0, nil
}
func (r *fakeProjectRepo) ListAccessible(_ context.Context, _ uuid.UUID, _, _ int) ([]*projectdom.Project, int64, error) {
	return nil, 0, nil
}
func (r *fakeProjectRepo) FindByID(_ context.Context, id uuid.UUID) (*projectdom.Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.projects[id]
	if !ok {
		return nil, projectdom.ErrNotFound
	}
	return p, nil
}
func (r *fakeProjectRepo) Create(_ context.Context, p *projectdom.Project) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projects[p.ID] = p
	return nil
}
func (r *fakeProjectRepo) Update(_ context.Context, p *projectdom.Project) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projects[p.ID] = p
	return nil
}
func (r *fakeProjectRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.projects, id)
	return nil
}
func (r *fakeProjectRepo) ListRoles(_ context.Context, _ uuid.UUID) ([]*projectdom.ProjectRole, error) {
	return nil, nil
}
func (r *fakeProjectRepo) FindRoleByID(_ context.Context, id uuid.UUID) (*projectdom.ProjectRole, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	role, ok := r.roles[id]
	if !ok {
		return nil, projectdom.ErrRoleNotFound
	}
	return role, nil
}
func (r *fakeProjectRepo) FindRoleByName(_ context.Context, _ uuid.UUID, _ string) (*projectdom.ProjectRole, error) {
	return nil, projectdom.ErrRoleNotFound
}
func (r *fakeProjectRepo) CreateRole(_ context.Context, role *projectdom.ProjectRole) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roles[role.ID] = role
	return nil
}
func (r *fakeProjectRepo) UpdateRole(_ context.Context, _ *projectdom.ProjectRole) error { return nil }
func (r *fakeProjectRepo) DeleteRole(_ context.Context, _ uuid.UUID) error               { return nil }
func (r *fakeProjectRepo) CountMembersWithRole(_ context.Context, _ uuid.UUID) (int64, error) {
	return 0, nil
}
func (r *fakeProjectRepo) ListMembers(_ context.Context, _ uuid.UUID) ([]*projectdom.ProjectMember, error) {
	return nil, nil
}
func (r *fakeProjectRepo) FindMember(_ context.Context, _ uuid.UUID, _ uuid.UUID) (*projectdom.ProjectMember, error) {
	return nil, projectdom.ErrMemberNotFound
}
func (r *fakeProjectRepo) AddMember(_ context.Context, m *projectdom.ProjectMember) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.members = append(r.members, *m)
	return nil
}
func (r *fakeProjectRepo) UpdateMemberRole(_ context.Context, _, _, _ uuid.UUID) error {
	return nil
}
func (r *fakeProjectRepo) RemoveMember(_ context.Context, _, _ uuid.UUID) error { return nil }

var _ projectdom.Repository = (*fakeProjectRepo)(nil)

// ---------------------------------------------------------------------------
// Fake task bootstrapper
// ---------------------------------------------------------------------------

type fakeTaskBootstrapper struct {
	mu       sync.Mutex
	types    []*taskdom.TaskType
	statuses []*taskdom.TaskStatus
}

func (f *fakeTaskBootstrapper) CreateTaskType(_ context.Context, t *taskdom.TaskType) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *t
	f.types = append(f.types, &cp)
	return nil
}

func (f *fakeTaskBootstrapper) CreateTaskStatus(_ context.Context, s *taskdom.TaskStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *s
	f.statuses = append(f.statuses, &cp)
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCreate_SeedsDefaultTaskTypesAndStatuses(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProjectRepo()
	tb := &fakeTaskBootstrapper{}
	svc := New(repo, tb)

	creatorID := uuid.New()
	p, err := svc.Create(ctx, projectdom.CreateProjectInput{
		Name:        "Seeding Test",
		Description: "desc",
		CreatedBy:   &creatorID,
	})
	if err != nil {
		t.Fatalf("Create returned unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("Create returned nil project")
	}

	// Verify 3 default task types are created.
	const wantTypes = 3
	if got := len(tb.types); got != wantTypes {
		t.Errorf("expected %d default task types, got %d", wantTypes, got)
	}
	typeNames := map[string]bool{}
	for _, tt := range tb.types {
		if tt.ProjectID != p.ID {
			t.Errorf("task type %q has wrong project id: want %s, got %s", tt.Name, p.ID, tt.ProjectID)
		}
		if tt.CreatedAt.IsZero() || tt.UpdatedAt.IsZero() {
			t.Errorf("task type %q has zero timestamp", tt.Name)
		}
		typeNames[tt.Name] = true
	}
	for _, name := range []string{"Task", "Bug", "Story"} {
		if !typeNames[name] {
			t.Errorf("missing expected default task type %q", name)
		}
	}

	// Verify 4 default task statuses are created.
	const wantStatuses = 4
	if got := len(tb.statuses); got != wantStatuses {
		t.Errorf("expected %d default task statuses, got %d", wantStatuses, got)
	}
	statusNames := map[string]taskdom.StatusCategory{}
	for _, ts := range tb.statuses {
		if ts.ProjectID != p.ID {
			t.Errorf("task status %q has wrong project id: want %s, got %s", ts.Name, p.ID, ts.ProjectID)
		}
		if ts.Position <= 0 {
			t.Errorf("task status %q has non-positive position: %d", ts.Name, ts.Position)
		}
		if ts.CreatedAt.IsZero() || ts.UpdatedAt.IsZero() {
			t.Errorf("task status %q has zero timestamp", ts.Name)
		}
		statusNames[ts.Name] = ts.Category
	}
	expected := map[string]taskdom.StatusCategory{
		"Backlog":     taskdom.StatusCategoryBacklog,
		"Todo":        taskdom.StatusCategoryTodo,
		"In Progress": taskdom.StatusCategoryInProgress,
		"Done":        taskdom.StatusCategoryDone,
	}
	for name, wantCat := range expected {
		if gotCat, ok := statusNames[name]; !ok {
			t.Errorf("missing expected default task status %q", name)
		} else if gotCat != wantCat {
			t.Errorf("task status %q: expected category %q, got %q", name, wantCat, gotCat)
		}
	}
}

func TestCreate_SeedsWithCorrectTimestamps(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProjectRepo()
	tb := &fakeTaskBootstrapper{}
	svc := New(repo, tb)

	before := time.Now().Truncate(time.Second)
	_, err := svc.Create(ctx, projectdom.CreateProjectInput{Name: "Timestamp Test"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	after := time.Now().Add(time.Second)

	for _, tt := range tb.types {
		if tt.CreatedAt.Before(before) || tt.CreatedAt.After(after) {
			t.Errorf("task type %q CreatedAt out of expected range: %v", tt.Name, tt.CreatedAt)
		}
	}
	for _, ts := range tb.statuses {
		if ts.CreatedAt.Before(before) || ts.CreatedAt.After(after) {
			t.Errorf("task status %q CreatedAt out of expected range: %v", ts.Name, ts.CreatedAt)
		}
	}
}

func TestCreate_NilTaskRepo_DoesNotPanic(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProjectRepo()
	svc := New(repo, nil) // nil task repo is allowed

	_, err := svc.Create(ctx, projectdom.CreateProjectInput{Name: "No Task Repo"})
	if err != nil {
		t.Fatalf("expected no error with nil task repo, got: %v", err)
	}
}

package projectsvc

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// ---------------------------------------------------------------------------
// Minimal fake avatar service
// ---------------------------------------------------------------------------

// fakeAvatarService is a bare-bones attachmentdom.AvatarService double —
// CompleteAvatarUpload always returns nextKeys, and DeleteAvatarObjects
// records what it was asked to delete so tests can assert the *previous*
// avatar's keys were cleaned up after a replace.
type fakeAvatarService struct {
	mu          sync.Mutex
	nextKeys    *attachmentdom.AvatarKeys
	completeErr error
	deletedKeys []string
}

func (f *fakeAvatarService) InitiateAvatarUpload(context.Context, attachmentdom.AvatarUploadInput) (*attachmentdom.UploadSession, error) {
	return &attachmentdom.UploadSession{FileID: uuid.New(), UploadURL: "https://fake/upload"}, nil
}

func (f *fakeAvatarService) CompleteAvatarUpload(context.Context, attachmentdom.AvatarCompleteInput) (*attachmentdom.AvatarKeys, error) {
	if f.completeErr != nil {
		return nil, f.completeErr
	}
	return f.nextKeys, nil
}

func (f *fakeAvatarService) ResolveAvatarURL(context.Context, *string) (*string, error) {
	return nil, nil
}

func (f *fakeAvatarService) DeleteAvatarObjects(_ context.Context, keys ...*string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		if k != nil && *k != "" {
			f.deletedKeys = append(f.deletedKeys, *k)
		}
	}
}

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
func (r *fakeProjectRepo) CountDistinctAgentsByProjects(_ context.Context, _ []uuid.UUID) (int64, error) {
	return 0, nil
}
func (r *fakeProjectRepo) FindMember(_ context.Context, _ uuid.UUID, _ uuid.UUID) (*projectdom.ProjectMember, error) {
	return nil, projectdom.ErrMemberNotFound
}
func (r *fakeProjectRepo) FindMemberByAgent(_ context.Context, _ uuid.UUID, _ uuid.UUID) (*projectdom.ProjectMember, error) {
	return nil, projectdom.ErrMemberNotFound
}
func (r *fakeProjectRepo) FindMemberByActor(_ context.Context, _, _ uuid.UUID, _ *uuid.UUID) (*projectdom.ProjectMember, error) {
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
func (r *fakeProjectRepo) FindMemberByUserProject(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
	return nil, projectdom.ErrMemberNotFound
}
func (r *fakeProjectRepo) FindMemberByID(_ context.Context, _ uuid.UUID) (*projectdom.ProjectMember, error) {
	return nil, projectdom.ErrMemberNotFound
}
func (r *fakeProjectRepo) AddAgentMember(_ context.Context, _, _, _, _ uuid.UUID) error { return nil }
func (r *fakeProjectRepo) RemoveAgentMember(_ context.Context, _, _ uuid.UUID) error    { return nil }
func (r *fakeProjectRepo) UpdateMemberRoleByMemberID(_ context.Context, _, _ uuid.UUID) error {
	return nil
}
func (r *fakeProjectRepo) RemoveMemberByMemberID(_ context.Context, _ uuid.UUID) error { return nil }

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
	svc := New(repo, tb, nil)

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

	// Verify 4 default task types are created (Task, Bug, Story + Epic system type).
	const wantTypes = 4
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
	for _, name := range []string{"Task", "Bug", "Story", "Epic"} {
		if !typeNames[name] {
			t.Errorf("missing expected default task type %q", name)
		}
	}

	// Verify Epic is the only system type.
	for _, tt := range tb.types {
		if tt.Name == "Epic" {
			if !tt.IsSystem {
				t.Errorf("expected task type %q to have IsSystem=true", tt.Name)
			}
		} else {
			if tt.IsSystem {
				t.Errorf("expected task type %q to have IsSystem=false", tt.Name)
			}
		}
	}

	// Verify "Task" is the only default type.
	for _, tt := range tb.types {
		if tt.Name == "Task" {
			if !tt.IsDefault {
				t.Errorf("expected task type %q to have IsDefault=true", tt.Name)
			}
		} else {
			if tt.IsDefault {
				t.Errorf("expected task type %q to have IsDefault=false", tt.Name)
			}
		}
	}

	// Verify descriptions are set for default task types
	expectedDescriptions := map[string]string{
		"Task":    "A general work item that needs to be completed",
		"Bug":     "An issue or defect that needs to be fixed",
		"Story":   "A user-facing feature or requirement",
		"Epic":    "A large body of work that can be broken down into smaller tasks",
		"Subtask": "A smaller piece of work within a parent task",
	}
	for _, tt := range tb.types {
		if expectedDesc, ok := expectedDescriptions[tt.Name]; ok {
			if tt.Description == nil {
				t.Errorf("task type %q missing description", tt.Name)
			} else if *tt.Description != expectedDesc {
				t.Errorf("task type %q: expected description %q, got %q", tt.Name, expectedDesc, *tt.Description)
			}
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
	svc := New(repo, tb, nil)

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
	svc := New(repo, nil, nil) // nil task repo is allowed

	_, err := svc.Create(ctx, projectdom.CreateProjectInput{Name: "No Task Repo"})
	if err != nil {
		t.Fatalf("expected no error with nil task repo, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Task ID prefix tests
// ---------------------------------------------------------------------------

func TestCreate_AutoGeneratesPrefix_SingleWord(t *testing.T) {
	ctx := context.Background()
	svc := New(newFakeProjectRepo(), nil, nil)

	p, err := svc.Create(ctx, projectdom.CreateProjectInput{Name: "paca"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if p.TaskIDPrefix != "PACA" {
		t.Errorf("expected prefix PACA, got %q", p.TaskIDPrefix)
	}
}

func TestCreate_AutoGeneratesPrefix_MultiWord(t *testing.T) {
	ctx := context.Background()
	svc := New(newFakeProjectRepo(), nil, nil)

	p, err := svc.Create(ctx, projectdom.CreateProjectInput{Name: "My Awesome Project"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if p.TaskIDPrefix != "MAP" {
		t.Errorf("expected prefix MAP, got %q", p.TaskIDPrefix)
	}
}

func TestCreate_ExplicitPrefixUsed(t *testing.T) {
	ctx := context.Background()
	svc := New(newFakeProjectRepo(), nil, nil)

	p, err := svc.Create(ctx, projectdom.CreateProjectInput{Name: "My Project", TaskIDPrefix: "MP2"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if p.TaskIDPrefix != "MP2" {
		t.Errorf("expected prefix MP2, got %q", p.TaskIDPrefix)
	}
}

func TestCreate_InvalidPrefixReturnsError(t *testing.T) {
	ctx := context.Background()
	svc := New(newFakeProjectRepo(), nil, nil)

	_, err := svc.Create(ctx, projectdom.CreateProjectInput{Name: "My Project", TaskIDPrefix: "my prefix!"})
	if err == nil {
		t.Fatal("expected error for invalid prefix, got nil")
	}
}

func TestUpdate_PrefixUpdated(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProjectRepo()
	svc := New(repo, nil, nil)

	p, _ := svc.Create(ctx, projectdom.CreateProjectInput{Name: "MyProj"})

	updated, err := svc.Update(ctx, p.ID, projectdom.UpdateProjectInput{TaskIDPrefix: "XYZ"})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if updated.TaskIDPrefix != "XYZ" {
		t.Errorf("expected prefix XYZ after update, got %q", updated.TaskIDPrefix)
	}
}

func TestUpdate_InvalidPrefixReturnsError(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProjectRepo()
	svc := New(repo, nil, nil)

	p, _ := svc.Create(ctx, projectdom.CreateProjectInput{Name: "MyProj"})

	_, err := svc.Update(ctx, p.ID, projectdom.UpdateProjectInput{TaskIDPrefix: "bad prefix"})
	if err == nil {
		t.Fatal("expected error for invalid prefix on update, got nil")
	}
}

// ---------------------------------------------------------------------------
// Delete tests
// ---------------------------------------------------------------------------

func TestDelete_ExistingProject_Succeeds(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProjectRepo()
	svc := New(repo, nil, nil)

	p, err := svc.Create(ctx, projectdom.CreateProjectInput{Name: "To Delete"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete returned unexpected error: %v", err)
	}

	_, err = repo.FindByID(ctx, p.ID)
	if !errors.Is(err, projectdom.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDelete_NonExistentProject_ReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	svc := New(newFakeProjectRepo(), nil, nil)

	err := svc.Delete(ctx, uuid.New())
	if !errors.Is(err, projectdom.ErrNotFound) {
		t.Errorf("expected ErrNotFound for missing project, got %v", err)
	}
}

func TestDelete_AlreadyDeletedProject_ReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProjectRepo()
	svc := New(repo, nil, nil)

	p, err := svc.Create(ctx, projectdom.CreateProjectInput{Name: "Double Delete"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Delete(ctx, p.ID); err != nil {
		t.Fatalf("first Delete: %v", err)
	}

	// Second delete must return ErrNotFound because FindByID filters deleted projects.
	err = svc.Delete(ctx, p.ID)
	if !errors.Is(err, projectdom.ErrNotFound) {
		t.Errorf("expected ErrNotFound on re-delete, got %v", err)
	}
}

func TestSuggestPrefix_Cases(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"PACA", "PACA"},
		{"Platform v3", "PV"},
		{"my project roadmap design", "MPRD"},
		{"", "PROJ"},
		{"  ", "PROJ"},
	}
	for _, tc := range cases {
		got := suggestPrefix(tc.name)
		if got != tc.want {
			t.Errorf("suggestPrefix(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Avatar
// ---------------------------------------------------------------------------

func TestInitiateAvatarUpload_NoAvatarService_ReturnsError(t *testing.T) {
	svc := New(newFakeProjectRepo(), nil, nil) // WithAvatarService never called
	_, err := svc.InitiateAvatarUpload(context.Background(), uuid.New(), "me.png", "image/png", 1024, uuid.New())
	if !errors.Is(err, ErrAvatarServiceRequired) {
		t.Fatalf("expected ErrAvatarServiceRequired, got %v", err)
	}
}

func TestCompleteAvatarUpload_SwapsKeysAndDeletesOld(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProjectRepo()
	oldKey, oldThumbKey := "avatars/projects/p1/old/full.png", "avatars/projects/p1/old/thumb.png"
	projectID := uuid.New()
	if err := repo.Create(ctx, &projectdom.Project{
		ID:             projectID,
		Name:           "Has Avatar",
		AvatarKey:      &oldKey,
		AvatarThumbKey: &oldThumbKey,
		CreatedAt:      time.Now(),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	avatarSvc := &fakeAvatarService{
		nextKeys: &attachmentdom.AvatarKeys{Key: "avatars/projects/p1/new/full.png", ThumbKey: "avatars/projects/p1/new/thumb.png"},
	}
	svc := New(repo, nil, nil).WithAvatarService(avatarSvc)

	p, err := svc.CompleteAvatarUpload(ctx, projectID, uuid.New())
	if err != nil {
		t.Fatalf("CompleteAvatarUpload: %v", err)
	}
	if p.AvatarKey == nil || *p.AvatarKey != avatarSvc.nextKeys.Key {
		t.Errorf("expected AvatarKey %q, got %v", avatarSvc.nextKeys.Key, p.AvatarKey)
	}
	if p.AvatarThumbKey == nil || *p.AvatarThumbKey != avatarSvc.nextKeys.ThumbKey {
		t.Errorf("expected AvatarThumbKey %q, got %v", avatarSvc.nextKeys.ThumbKey, p.AvatarThumbKey)
	}

	stored, err := repo.FindByID(ctx, projectID)
	if err != nil {
		t.Fatalf("FindByID after complete: %v", err)
	}
	if stored.AvatarKey == nil || *stored.AvatarKey != avatarSvc.nextKeys.Key {
		t.Errorf("persisted AvatarKey not updated, got %v", stored.AvatarKey)
	}

	avatarSvc.mu.Lock()
	defer avatarSvc.mu.Unlock()
	if len(avatarSvc.deletedKeys) != 2 {
		t.Fatalf("expected the two old keys to be deleted, got %v", avatarSvc.deletedKeys)
	}
	deleted := map[string]bool{avatarSvc.deletedKeys[0]: true, avatarSvc.deletedKeys[1]: true}
	if !deleted[oldKey] || !deleted[oldThumbKey] {
		t.Errorf("expected old keys %q/%q to be deleted, got %v", oldKey, oldThumbKey, avatarSvc.deletedKeys)
	}
}

func TestRemoveAvatar_NoExistingAvatar_NoOps(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProjectRepo()
	projectID := uuid.New()
	if err := repo.Create(ctx, &projectdom.Project{ID: projectID, Name: "No Avatar", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	avatarSvc := &fakeAvatarService{}
	svc := New(repo, nil, nil).WithAvatarService(avatarSvc)

	if _, err := svc.RemoveAvatar(ctx, projectID); err != nil {
		t.Fatalf("RemoveAvatar: %v", err)
	}

	avatarSvc.mu.Lock()
	defer avatarSvc.mu.Unlock()
	if len(avatarSvc.deletedKeys) != 0 {
		t.Errorf("expected no delete calls when project has no avatar, got %v", avatarSvc.deletedKeys)
	}
}

func TestRemoveAvatar_ClearsKeysAndDeletesObjects(t *testing.T) {
	ctx := context.Background()
	repo := newFakeProjectRepo()
	key, thumbKey := "avatars/projects/p2/full.png", "avatars/projects/p2/thumb.png"
	projectID := uuid.New()
	if err := repo.Create(ctx, &projectdom.Project{
		ID: projectID, Name: "Has Avatar", AvatarKey: &key, AvatarThumbKey: &thumbKey, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	avatarSvc := &fakeAvatarService{}
	svc := New(repo, nil, nil).WithAvatarService(avatarSvc)

	p, err := svc.RemoveAvatar(ctx, projectID)
	if err != nil {
		t.Fatalf("RemoveAvatar: %v", err)
	}
	if p.AvatarKey != nil || p.AvatarThumbKey != nil {
		t.Errorf("expected avatar keys cleared, got %v / %v", p.AvatarKey, p.AvatarThumbKey)
	}

	avatarSvc.mu.Lock()
	defer avatarSvc.mu.Unlock()
	if len(avatarSvc.deletedKeys) != 2 {
		t.Errorf("expected both keys deleted, got %v", avatarSvc.deletedKeys)
	}
}

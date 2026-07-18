package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/authz"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
	docsvc "github.com/Paca-AI/api/internal/service/doc"
	projectsvc "github.com/Paca-AI/api/internal/service/project"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
	tasksvc "github.com/Paca-AI/api/internal/service/task"
	usersvc "github.com/Paca-AI/api/internal/service/user"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/router"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Fake doc repository
// ---------------------------------------------------------------------------

type fakeDocRepoIT struct {
	mu         sync.RWMutex
	folders    map[uuid.UUID]*docdom.DocFolder
	docs       map[uuid.UUID]*docdom.Document
	snapshots  map[uuid.UUID]*docdom.DocSnapshot
	activities map[uuid.UUID]*docdom.Activity
}

func newFakeDocRepoIT() *fakeDocRepoIT {
	return &fakeDocRepoIT{
		folders:    make(map[uuid.UUID]*docdom.DocFolder),
		docs:       make(map[uuid.UUID]*docdom.Document),
		snapshots:  make(map[uuid.UUID]*docdom.DocSnapshot),
		activities: make(map[uuid.UUID]*docdom.Activity),
	}
}

func (r *fakeDocRepoIT) ListFolders(_ context.Context, projectID uuid.UUID) ([]*docdom.DocFolder, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*docdom.DocFolder
	for _, f := range r.folders {
		if f.ProjectID == projectID {
			cp := *f
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeDocRepoIT) FindFolderByID(_ context.Context, id uuid.UUID) (*docdom.DocFolder, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.folders[id]
	if !ok {
		return nil, docdom.ErrFolderNotFound
	}
	cp := *f
	return &cp, nil
}

func (r *fakeDocRepoIT) CreateFolder(_ context.Context, f *docdom.DocFolder) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *f
	r.folders[f.ID] = &cp
	return nil
}

func (r *fakeDocRepoIT) UpdateFolder(_ context.Context, f *docdom.DocFolder) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.folders[f.ID]; !ok {
		return docdom.ErrFolderNotFound
	}
	cp := *f
	r.folders[f.ID] = &cp
	return nil
}

func (r *fakeDocRepoIT) DeleteFolder(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.folders, id)
	return nil
}

func (r *fakeDocRepoIT) ListDocuments(_ context.Context, projectID uuid.UUID, folderID *uuid.UUID) ([]*docdom.Document, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*docdom.Document
	for _, d := range r.docs {
		if d.ProjectID != projectID || d.DeletedAt != nil {
			continue
		}
		if folderID != nil {
			if d.FolderID == nil || *d.FolderID != *folderID {
				continue
			}
		}
		cp := *d
		out = append(out, &cp)
	}
	return out, nil
}

func (r *fakeDocRepoIT) FindDocumentByID(_ context.Context, id uuid.UUID) (*docdom.Document, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.docs[id]
	if !ok || d.DeletedAt != nil {
		return nil, docdom.ErrDocNotFound
	}
	cp := *d
	return &cp, nil
}

func (r *fakeDocRepoIT) CreateDocument(_ context.Context, d *docdom.Document) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *d
	r.docs[d.ID] = &cp
	return nil
}

func (r *fakeDocRepoIT) UpdateDocument(_ context.Context, d *docdom.Document) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.docs[d.ID]; !ok {
		return docdom.ErrDocNotFound
	}
	cp := *d
	r.docs[d.ID] = &cp
	return nil
}

func (r *fakeDocRepoIT) DeleteDocument(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.docs[id]
	if !ok || d.DeletedAt != nil {
		return docdom.ErrDocNotFound
	}
	now := time.Now()
	d.DeletedAt = &now
	return nil
}

func (r *fakeDocRepoIT) ListSnapshots(_ context.Context, documentID uuid.UUID) ([]*docdom.DocSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*docdom.DocSnapshot
	for _, s := range r.snapshots {
		if s.DocumentID == documentID {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeDocRepoIT) FindSnapshotByID(_ context.Context, id uuid.UUID) (*docdom.DocSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.snapshots[id]
	if !ok {
		return nil, docdom.ErrSnapshotNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *fakeDocRepoIT) FindLatestSnapshot(_ context.Context, documentID uuid.UUID) (*docdom.DocSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var latest *docdom.DocSnapshot
	for _, s := range r.snapshots {
		if s.DocumentID != documentID {
			continue
		}
		if latest == nil || s.SnapshotNumber > latest.SnapshotNumber {
			cp := *s
			latest = &cp
		}
	}
	return latest, nil
}

func (r *fakeDocRepoIT) CreateSnapshot(_ context.Context, s *docdom.DocSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	maxNum := int64(0)
	for _, existing := range r.snapshots {
		if existing.DocumentID == s.DocumentID && existing.SnapshotNumber > maxNum {
			maxNum = existing.SnapshotNumber
		}
	}
	s.SnapshotNumber = maxNum + 1
	cp := *s
	r.snapshots[s.ID] = &cp
	return nil
}

func (r *fakeDocRepoIT) DeleteRecentSnapshotsExcept(_ context.Context, documentID uuid.UUID, excludeID uuid.UUID, since time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, s := range r.snapshots {
		if s.DocumentID == documentID && s.ID != excludeID && !s.CreatedAt.Before(since) {
			delete(r.snapshots, id)
		}
	}
	return nil
}

func (r *fakeDocRepoIT) ListActivities(_ context.Context, documentID uuid.UUID) ([]*docdom.Activity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*docdom.Activity
	for _, a := range r.activities {
		if a.DocumentID == documentID && a.DeletedAt == nil {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeDocRepoIT) FindActivityByID(_ context.Context, id uuid.UUID) (*docdom.Activity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.activities[id]
	if !ok {
		return nil, docdom.ErrActivityNotFound
	}
	cp := *a
	return &cp, nil
}

func (r *fakeDocRepoIT) CreateActivity(_ context.Context, a *docdom.Activity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *a
	r.activities[a.ID] = &cp
	return nil
}

func (r *fakeDocRepoIT) UpdateActivity(_ context.Context, a *docdom.Activity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.activities[a.ID]; !ok {
		return docdom.ErrActivityNotFound
	}
	cp := *a
	r.activities[a.ID] = &cp
	return nil
}

func (r *fakeDocRepoIT) DeleteActivity(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.activities[id]
	if !ok {
		return docdom.ErrActivityNotFound
	}
	now := time.Now()
	a.DeletedAt = &now
	return nil
}

// ---------------------------------------------------------------------------
// Doc member lookup stub — resolves any actor to a ProjectMember whose ID
// is the agent ID (when present) or the user UUID.
// ---------------------------------------------------------------------------

// The one deliberate exception mirrors production: userdom.SystemActorUserID
// with no agentID (the shared agent API key used without X-Agent-ID) never
// resolves, since that identity is never itself a project member.
type fakeDocMemberLookup struct{}

func (f *fakeDocMemberLookup) FindMemberByActor(_ context.Context, _, actorID uuid.UUID, agentID *uuid.UUID) (*projectdom.ProjectMember, error) {
	if agentID != nil {
		return &projectdom.ProjectMember{ID: *agentID}, nil
	}
	if actorID == userdom.SystemActorUserID {
		return nil, projectdom.ErrMemberNotFound
	}
	return &projectdom.ProjectMember{ID: actorID}, nil
}

// ---------------------------------------------------------------------------
// Router builder
// ---------------------------------------------------------------------------

func buildDocTestRouter(docRepo *fakeDocRepoIT, store *projectPermStore) http.Handler {
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	refreshStore := &fakeRefreshStore{}
	userRepo := newFakeUserRepo()
	authService := authsvc.New(userRepo, tm, refreshStore, 168*time.Hour, 24*time.Hour)
	userService := usersvc.New(userRepo)
	projectRepo := newFakeProjectRepo()
	taskRepo := newFakeTaskRepoIT()
	projectService := projectsvc.New(projectRepo, taskRepo)
	taskService := tasksvc.New(taskRepo)
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	sprintService := sprintsvc.New(sprintRepo, taskRepo)
	viewService := sprintsvc.NewViewService(viewRepo)
	activityRepo := newFakeTaskActivityRepo()
	activityService := tasksvc.NewActivityService(activityRepo, &fakeActivityMemberRepo{}, nil)
	docService := docsvc.New(docRepo, &fakeDocMemberLookup{})
	docActivityService := docsvc.NewActivityService(docRepo, &fakeDocMemberLookup{}, nil)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return router.New(router.Deps{
		TokenManager:         tm,
		Authorizer:           authz.NewAuthorizer(store),
		ProjectVisibilitySvc: projectService,
		Health:               handler.NewHealthHandler(),
		Auth:                 handler.NewAuthHandler(authService, testCookieCfg),
		User:                 handler.NewUserHandler(userService),
		GlobalRole:           handler.NewGlobalRoleHandler(&fakeGlobalRoleService{}, authz.NewAuthorizer(store)),
		Project:              handler.NewProjectHandler(projectService, authz.NewAuthorizer(store)),
		Task:                 handler.NewTaskHandler(taskService, viewService, activityService),
		Sprint:               handler.NewSprintHandler(sprintService, viewService),
		View:                 handler.NewViewHandler(viewService),
		Document:             handler.NewDocumentHandler(docService, docActivityService),
		Log:                  log,
	})
}

func issueDocToken(t *testing.T, subject string) string {
	t.Helper()
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	tok, err := tm.IssueAccess(subject, "doc-user", "USER", "fam-doc", false)
	if err != nil {
		t.Fatalf("issue doc token: %v", err)
	}
	return tok
}

// docIDFrom decodes data.id from a handler JSON response.
func docIDFrom(t *testing.T, label string, body []byte) string {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode %s response: %v", label, err)
	}
	id, _ := env.Data["id"].(string)
	if id == "" {
		t.Fatalf("missing id in %s response: %s", label, string(body))
	}
	return id
}

// docListCount decodes data.items and returns its length.
func docListCount(t *testing.T, body []byte) int {
	t.Helper()
	var env struct {
		Data struct {
			Items []any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	return len(env.Data.Items)
}

// ---------------------------------------------------------------------------
// Folder tests
// ---------------------------------------------------------------------------

func TestIntegrationDocFolders_CRUD(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionDocsRead, authz.PermissionDocsWrite},
		},
	}
	r := buildDocTestRouter(docRepo, store)
	tok := issueDocToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/docs/folders", projectID)

	// Create folder
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name": "Architecture",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create folder: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	folderID := docIDFrom(t, "folder", createW.Body.Bytes())

	// List folders
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list folders: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	if count := docListCount(t, listW.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 folder, got %d", count)
	}

	// Update folder
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+folderID, tok, map[string]any{
		"name": "Architecture & Design",
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("update folder: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}

	// Delete folder
	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, base+"/"+folderID, tok, nil))
	if delW.Code != http.StatusNoContent {
		t.Fatalf("delete folder: expected 204, got %d (%s)", delW.Code, delW.Body.String())
	}

	// Verify removed from list
	listAfterW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if count := docListCount(t, listAfterW.Body.Bytes()); count != 0 {
		t.Errorf("expected 0 folders after delete, got %d", count)
	}
}

func TestIntegrationDocFolders_InvalidNameReturns400(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionDocsWrite},
		},
	}
	r := buildDocTestRouter(docRepo, store)
	tok := issueDocToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/docs/folders", projectID), tok, map[string]any{"name": "   "}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "DOC_FOLDER_NAME_INVALID" {
		t.Errorf("expected DOC_FOLDER_NAME_INVALID, got %q", code)
	}
}

func TestIntegrationDocFolders_DeleteNotFoundReturns404(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionDocsWrite},
		},
	}
	r := buildDocTestRouter(docRepo, store)
	tok := issueDocToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodDelete,
		fmt.Sprintf("/api/v1/projects/%s/docs/folders/%s", projectID, uuid.New()), tok, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "DOC_FOLDER_NOT_FOUND" {
		t.Errorf("expected DOC_FOLDER_NOT_FOUND, got %q", code)
	}
}

func TestIntegrationDocFolders_NoPermissionReturns403(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionDocsRead}, // read-only, no write
		},
	}
	r := buildDocTestRouter(docRepo, store)
	tok := issueDocToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/docs/folders", projectID), tok, map[string]any{"name": "X"}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (%s)", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Document CRUD tests
// ---------------------------------------------------------------------------

func TestIntegrationDocuments_CRUD(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionDocsRead, authz.PermissionDocsWrite},
		},
	}
	r := buildDocTestRouter(docRepo, store)
	tok := issueDocToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/docs", projectID)

	// Create document
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title":   "Getting Started",
		"content": json.RawMessage(`[]`),
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create document: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	docID := docIDFrom(t, "document", createW.Body.Bytes())

	// List documents
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list documents: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	if count := docListCount(t, listW.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 document, got %d", count)
	}

	// Get document
	getW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base+"/"+docID, tok, nil))
	if getW.Code != http.StatusOK {
		t.Fatalf("get document: expected 200, got %d (%s)", getW.Code, getW.Body.String())
	}

	// Update document title
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+docID, tok, map[string]any{
		"title": "Getting Started Guide",
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("update document: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}

	// Delete document
	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, base+"/"+docID, tok, nil))
	if delW.Code != http.StatusNoContent {
		t.Fatalf("delete document: expected 204, got %d (%s)", delW.Code, delW.Body.String())
	}

	// Verify gone
	listAfterW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if count := docListCount(t, listAfterW.Body.Bytes()); count != 0 {
		t.Errorf("expected 0 documents after delete, got %d", count)
	}
}

func TestIntegrationDocuments_GetNotFoundReturns404(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionDocsRead},
		},
	}
	r := buildDocTestRouter(docRepo, store)
	tok := issueDocToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%s/docs/%s", projectID, uuid.New()), tok, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "DOC_NOT_FOUND" {
		t.Errorf("expected DOC_NOT_FOUND, got %q", code)
	}
}

func TestIntegrationDocuments_EmptyTitleCreatesUntitled(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionDocsWrite},
		},
	}
	r := buildDocTestRouter(docRepo, store)
	tok := issueDocToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/docs", projectID), tok, map[string]any{"title": "   "}))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", w.Code, w.Body.String())
	}

	// Confirm the title was set to "Untitled"
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if title, _ := env.Data["title"].(string); title != "Untitled" {
		t.Errorf("expected title=Untitled, got %q", title)
	}
}

func TestIntegrationDocuments_UpdateWithEmptyTitleReturns400(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionDocsRead, authz.PermissionDocsWrite},
		},
	}
	r := buildDocTestRouter(docRepo, store)
	tok := issueDocToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/docs", projectID)

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"title": "Doc"}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", createW.Code)
	}
	docID := docIDFrom(t, "document", createW.Body.Bytes())

	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+docID, tok, map[string]any{"title": "   "}))
	if patchW.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", patchW.Code, patchW.Body.String())
	}
	if code := decodeErrorCode(t, patchW); code != "DOC_TITLE_INVALID" {
		t.Errorf("expected DOC_TITLE_INVALID, got %q", code)
	}
}

func TestIntegrationDocuments_FilterByFolder(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionDocsRead, authz.PermissionDocsWrite},
		},
	}
	r := buildDocTestRouter(docRepo, store)
	tok := issueDocToken(t, uuid.NewString())
	docBase := fmt.Sprintf("/api/v1/projects/%s/docs", projectID)
	folderBase := fmt.Sprintf("/api/v1/projects/%s/docs/folders", projectID)

	// Create a folder
	fW := serve(r, authedJSONReq(t.Context(), http.MethodPost, folderBase, tok, map[string]any{"name": "API Docs"}))
	if fW.Code != http.StatusCreated {
		t.Fatalf("create folder: expected 201, got %d", fW.Code)
	}
	folderID := docIDFrom(t, "folder", fW.Body.Bytes())

	// Create one doc in the folder and one at root
	serve(r, authedJSONReq(t.Context(), http.MethodPost, docBase, tok, map[string]any{
		"title":     "In Folder",
		"folder_id": folderID,
	}))
	serve(r, authedJSONReq(t.Context(), http.MethodPost, docBase, tok, map[string]any{
		"title": "Root Doc",
	}))

	// List all — expect 2
	all := serve(r, authedJSONReq(t.Context(), http.MethodGet, docBase, tok, nil))
	if count := docListCount(t, all.Body.Bytes()); count != 2 {
		t.Errorf("expected 2 documents total, got %d", count)
	}

	// Filter by folder — expect 1
	filtered := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		docBase+"?folder_id="+folderID, tok, nil))
	if count := docListCount(t, filtered.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 document in folder, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Snapshot tests
// ---------------------------------------------------------------------------

func TestIntegrationDocuments_Snapshots(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionDocsRead, authz.PermissionDocsWrite},
		},
	}
	r := buildDocTestRouter(docRepo, store)
	tok := issueDocToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/docs", projectID)

	// Create document with initial content
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title":   "API Reference",
		"content": json.RawMessage(`[{"type":"paragraph","content":[{"type":"text","text":"v1"}]}]`),
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", createW.Code)
	}
	docID := docIDFrom(t, "document", createW.Body.Bytes())
	snapBase := base + "/" + docID + "/snapshots"

	// No snapshots initially
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, snapBase, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list snapshots: expected 200, got %d", listW.Code)
	}
	if count := docListCount(t, listW.Body.Bytes()); count != 0 {
		t.Errorf("expected 0 snapshots initially, got %d", count)
	}

	// Update content — triggers snapshot creation
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+docID, tok, map[string]any{
		"content": json.RawMessage(`[{"type":"paragraph","content":[{"type":"text","text":"v2"}]}]`),
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}

	// Now 1 snapshot
	listAfterW := serve(r, authedJSONReq(t.Context(), http.MethodGet, snapBase, tok, nil))
	if count := docListCount(t, listAfterW.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 snapshot after content change, got %d", count)
	}

	// Get the snapshot by ID
	snapID := func() string {
		var env struct {
			Data struct {
				Items []map[string]any `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(listAfterW.Body.Bytes(), &env); err != nil {
			t.Fatalf("unmarshal snapshot list: %v", err)
		}
		if len(env.Data.Items) == 0 {
			return ""
		}
		id, _ := env.Data.Items[0]["id"].(string)
		return id
	}()
	if snapID == "" {
		t.Fatal("could not extract snapshot id")
	}

	getSnapW := serve(r, authedJSONReq(t.Context(), http.MethodGet, snapBase+"/"+snapID, tok, nil))
	if getSnapW.Code != http.StatusOK {
		t.Fatalf("get snapshot: expected 200, got %d (%s)", getSnapW.Code, getSnapW.Body.String())
	}
}

func TestIntegrationDocuments_SnapshotNotFoundReturns404(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionDocsRead, authz.PermissionDocsWrite},
		},
	}
	r := buildDocTestRouter(docRepo, store)
	tok := issueDocToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/docs", projectID)

	// Create document first
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"title": "Doc"}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", createW.Code)
	}
	docID := docIDFrom(t, "document", createW.Body.Bytes())

	w := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("%s/%s/snapshots/%s", base, docID, uuid.New()), tok, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "DOC_SNAPSHOT_NOT_FOUND" {
		t.Errorf("expected DOC_SNAPSHOT_NOT_FOUND, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// Activity / comment tests
// ---------------------------------------------------------------------------

func TestIntegrationDocuments_Comments_CRUD(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	userID := uuid.NewString()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionDocsRead, authz.PermissionDocsWrite},
		},
	}
	r := buildDocTestRouter(docRepo, store)
	tok := issueDocToken(t, userID)
	base := fmt.Sprintf("/api/v1/projects/%s/docs", projectID)

	// Create document
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"title": "Doc"}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", createW.Code)
	}
	docID := docIDFrom(t, "document", createW.Body.Bytes())
	actBase := base + "/" + docID
	commentBase := actBase + "/comments"

	// Add comment
	addW := serve(r, authedJSONReq(t.Context(), http.MethodPost, commentBase, tok, map[string]any{
		"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": "Great documentation!"}}}},
	}))
	if addW.Code != http.StatusCreated {
		t.Fatalf("add comment: expected 201, got %d (%s)", addW.Code, addW.Body.String())
	}
	commentID := docIDFrom(t, "comment", addW.Body.Bytes())

	// List activities — should include at least the comment (system events like
	// doc.created go through the Valkey stream and are persisted by the
	// DocActivityConsumer; the consumer does not run in integration tests so
	// only comment activities are written directly to the DB here).
	activitiesW := serve(r, authedJSONReq(t.Context(), http.MethodGet, actBase+"/activities", tok, nil))
	if activitiesW.Code != http.StatusOK {
		t.Fatalf("list activities: expected 200, got %d (%s)", activitiesW.Code, activitiesW.Body.String())
	}
	actCount := docListCount(t, activitiesW.Body.Bytes())
	if actCount < 1 {
		t.Errorf("expected at least 1 activity, got %d", actCount)
	}

	// Update comment
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, commentBase+"/"+commentID, tok, map[string]any{
		"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": "Excellent documentation!"}}}},
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("update comment: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}

	// Delete comment
	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, commentBase+"/"+commentID, tok, nil))
	if delW.Code != http.StatusNoContent {
		t.Fatalf("delete comment: expected 204, got %d (%s)", delW.Code, delW.Body.String())
	}
}

func TestIntegrationDocuments_AddEmptyCommentReturns400(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionDocsRead, authz.PermissionDocsWrite},
		},
	}
	r := buildDocTestRouter(docRepo, store)
	tok := issueDocToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/docs", projectID)

	// Create document
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"title": "Doc"}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", createW.Code)
	}
	docID := docIDFrom(t, "document", createW.Body.Bytes())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		base+"/"+docID+"/comments", tok, map[string]any{"content": "   "}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "DOC_COMMENT_CONTENT_INVALID" {
		t.Errorf("expected DOC_COMMENT_CONTENT_INVALID, got %q", code)
	}
}

// TestIntegrationDocuments_AddStringCommentReturns400 guards against GitHub
// issue #233: a bare JSON string (not a BlockNote block array, and not the
// legacy {"text": "..."} shape) must be rejected rather than stored, since
// the web app's block renderers crash on it.
func TestIntegrationDocuments_AddStringCommentReturns400(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionDocsRead, authz.PermissionDocsWrite},
		},
	}
	r := buildDocTestRouter(docRepo, store)
	tok := issueDocToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/docs", projectID)

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"title": "Doc"}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", createW.Code)
	}
	docID := docIDFrom(t, "document", createW.Body.Bytes())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		base+"/"+docID+"/comments", tok, map[string]any{"content": "just a plain string"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "DOC_COMMENT_CONTENT_INVALID" {
		t.Errorf("expected DOC_COMMENT_CONTENT_INVALID, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// Authorization tests
// ---------------------------------------------------------------------------

func TestIntegrationDocuments_UnauthenticatedReturns401(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{}
	r := buildDocTestRouter(docRepo, store)

	// No token — request without Authorization header
	req := authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%s/docs", projectID), "", nil)
	req.Header.Del("Authorization")
	w := serve(r, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestIntegrationDocuments_ForbiddenReturns403(t *testing.T) {
	docRepo := newFakeDocRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionDocsRead}, // read-only
		},
	}
	r := buildDocTestRouter(docRepo, store)
	tok := issueDocToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/docs", projectID), tok, map[string]any{"title": "X"}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (%s)", w.Code, w.Body.String())
	}
}

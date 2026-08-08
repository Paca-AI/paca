package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/platform/storage"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	attachmentsvc "github.com/Paca-AI/api/internal/service/attachment"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
	projectsvc "github.com/Paca-AI/api/internal/service/project"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
	tasksvc "github.com/Paca-AI/api/internal/service/task"
	usersvc "github.com/Paca-AI/api/internal/service/user"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/router"
)

// ---------------------------------------------------------------------------
// Fake attachment repository
// ---------------------------------------------------------------------------

type fakeAttachmentRepo struct {
	mu          sync.RWMutex
	files       map[uuid.UUID]*attachmentdom.File
	attachments map[uuid.UUID]*attachmentdom.TaskAttachment
}

func newFakeAttachmentRepo() *fakeAttachmentRepo {
	return &fakeAttachmentRepo{
		files:       make(map[uuid.UUID]*attachmentdom.File),
		attachments: make(map[uuid.UUID]*attachmentdom.TaskAttachment),
	}
}

func (r *fakeAttachmentRepo) CreateFile(_ context.Context, f *attachmentdom.File) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *f
	r.files[f.ID] = &cp
	return nil
}

func (r *fakeAttachmentRepo) FindFileByID(_ context.Context, id uuid.UUID) (*attachmentdom.File, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.files[id]
	if !ok {
		return nil, attachmentdom.ErrFileNotFound
	}
	cp := *f
	return &cp, nil
}

func (r *fakeAttachmentRepo) UpdateFileStatus(_ context.Context, id uuid.UUID, status attachmentdom.UploadStatus, multipartUploadID *string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.files[id]
	if !ok {
		return attachmentdom.ErrFileNotFound
	}
	f.UploadStatus = status
	if multipartUploadID != nil {
		s := *multipartUploadID
		f.MultipartUploadID = &s
	} else {
		f.MultipartUploadID = nil
	}
	return nil
}

func (r *fakeAttachmentRepo) DeleteFile(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.files, id)
	return nil
}

func (r *fakeAttachmentRepo) ListTaskAttachments(_ context.Context, taskID uuid.UUID) ([]*attachmentdom.TaskAttachment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*attachmentdom.TaskAttachment
	for _, a := range r.attachments {
		if a.TaskID == taskID {
			cp := *a
			// Eagerly load file.
			if f, ok := r.files[a.FileID]; ok {
				fileCp := *f
				cp.File = &fileCp
			}
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeAttachmentRepo) FindTaskAttachmentByID(_ context.Context, id uuid.UUID) (*attachmentdom.TaskAttachment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.attachments[id]
	if !ok {
		return nil, attachmentdom.ErrAttachmentNotFound
	}
	cp := *a
	return &cp, nil
}

func (r *fakeAttachmentRepo) CreateTaskAttachment(_ context.Context, a *attachmentdom.TaskAttachment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *a
	r.attachments[a.ID] = &cp
	return nil
}

func (r *fakeAttachmentRepo) DeleteTaskAttachment(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.attachments[id]; !ok {
		return attachmentdom.ErrAttachmentNotFound
	}
	delete(r.attachments, id)
	return nil
}

// ---------------------------------------------------------------------------
// Fake storage client
// ---------------------------------------------------------------------------

type fakeStorageClient struct {
	mu               sync.Mutex
	presignedURLs    map[string]string // key → put URL
	getURLs          map[string]string // key → get URL
	multipartUploads map[string]*storage.MultipartUpload
	deletedKeys      []string
	objects          map[string][]byte // key → uploaded bytes (GetObject/PutObject)
}

func newFakeStorageClient() *fakeStorageClient {
	return &fakeStorageClient{
		presignedURLs:    make(map[string]string),
		getURLs:          make(map[string]string),
		multipartUploads: make(map[string]*storage.MultipartUpload),
		objects:          make(map[string][]byte),
	}
}

func (c *fakeStorageClient) PresignPutObject(_ context.Context, bucket, key, _ string, _ time.Duration) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	url := fmt.Sprintf("https://fake-storage/%s/%s?sig=put", bucket, key)
	c.presignedURLs[key] = url
	return url, nil
}

func (c *fakeStorageClient) PresignGetObject(_ context.Context, bucket, key string, _ time.Duration, contentDisposition string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	url := fmt.Sprintf("https://fake-storage/%s/%s?sig=get", bucket, key)
	if contentDisposition != "" {
		url += "&cd=" + contentDisposition
	}
	c.getURLs[key] = url
	return url, nil
}

func (c *fakeStorageClient) InitiateMultipartUpload(_ context.Context, bucket, key, _ string, totalSize, partSize int64, _ time.Duration) (*storage.MultipartUpload, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	uploadID := "fake-upload-id-" + key
	numParts := int((totalSize + partSize - 1) / partSize)
	parts := make([]storage.PresignedPart, numParts)
	for i := range parts {
		parts[i] = storage.PresignedPart{
			PartNumber: i + 1,
			UploadURL:  fmt.Sprintf("https://fake-storage/%s/%s?part=%d&sig=put", bucket, key, i+1),
		}
	}
	mu := &storage.MultipartUpload{UploadID: uploadID, Parts: parts}
	c.multipartUploads[key] = mu
	return mu, nil
}

func (c *fakeStorageClient) CompleteMultipartUpload(_ context.Context, _, key, _ string, _ []storage.CompletedPart) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.multipartUploads, key)
	return nil
}

func (c *fakeStorageClient) AbortMultipartUpload(_ context.Context, _, key, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.multipartUploads, key)
	return nil
}

func (c *fakeStorageClient) DeleteObject(_ context.Context, _, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deletedKeys = append(c.deletedKeys, key)
	return nil
}

func (c *fakeStorageClient) EnsureBucket(_ context.Context, _ string) error { return nil }

func (c *fakeStorageClient) GetObject(_ context.Context, _, key string, maxBytes int64) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.objects[key]
	if !ok {
		return nil, fmt.Errorf("fake storage: object %q not found", key)
	}
	// Mirror S3Client's io.LimitReader truncation so tests exercise the same
	// "reader capped, not just length-checked after the fact" behavior.
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return data[:maxBytes], nil
	}
	return data, nil
}

func (c *fakeStorageClient) PutObject(_ context.Context, _, key, _ string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.objects[key] = data
	return nil
}

// ---------------------------------------------------------------------------
// Router builder for attachment tests
// ---------------------------------------------------------------------------

func buildAttachmentTestRouter(attachRepo *fakeAttachmentRepo, store *fakeStorageClient, permStore *projectPermStore, tasks ...*taskdom.Task) http.Handler {
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	refreshStore := &fakeRefreshStore{}
	userRepo := newFakeUserRepo()
	authService := authsvc.New(userRepo, tm, refreshStore, 168*time.Hour, 24*time.Hour)
	userService := usersvc.New(userRepo)
	projectRepo := newFakeProjectRepo()
	taskRepo := newFakeTaskRepoIT()
	for _, t := range tasks {
		_ = taskRepo.CreateTask(context.Background(), t)
	}
	projectService := projectsvc.New(projectRepo, taskRepo, nil)
	taskService := tasksvc.New(taskRepo)
	sprintService := sprintsvc.New(newFakeSprintRepoIT(), taskRepo, nil)
	viewService := sprintsvc.NewViewService(newFakeViewRepoIT(), nil)
	active := tasksvc.NewActivityService(newFakeTaskActivityRepo(), &fakeActivityMemberRepo{}, nil)
	attachmentService := attachmentsvc.New(attachRepo, attachmentsvc.NewTaskOwnerChecker(taskRepo), store, "test-bucket")
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return router.New(router.Deps{
		TokenManager:         tm,
		Authorizer:           authz.NewAuthorizer(permStore),
		ProjectVisibilitySvc: projectService,
		Health:               handler.NewHealthHandler(),
		Auth:                 handler.NewAuthHandler(authService, testCookieCfg),
		User:                 handler.NewUserHandler(userService),
		GlobalRole:           handler.NewGlobalRoleHandler(&fakeGlobalRoleService{}),
		Project:              handler.NewProjectHandler(projectService, authz.NewAuthorizer(permStore)),
		Task:                 handler.NewTaskHandler(taskService, viewService, active),
		Sprint:               handler.NewSprintHandler(sprintService, viewService),
		View:                 handler.NewViewHandler(viewService),
		Attachment:           handler.NewAttachmentHandler(attachmentService),
		Log:                  log,
	})
}

// buildAvatarTestRouter wires a router with the user self-service avatar
// endpoints backed by real (fake) storage — unlike buildAttachmentTestRouter,
// it returns the user repo so tests can seed a user and read back the
// avatar keys persisted by CompleteAvatarUpload.
func buildAvatarTestRouter(attachRepo *fakeAttachmentRepo, store *fakeStorageClient) (http.Handler, *fakeUserRepo) {
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	refreshStore := &fakeRefreshStore{}
	userRepo := newFakeUserRepo()
	authService := authsvc.New(userRepo, tm, refreshStore, 168*time.Hour, 24*time.Hour)
	taskRepo := newFakeTaskRepoIT()
	attachmentService := attachmentsvc.New(attachRepo, attachmentsvc.NewTaskOwnerChecker(taskRepo), store, "test-bucket")
	userService := usersvc.New(userRepo).WithAvatarService(attachmentService)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	h := router.New(router.Deps{
		TokenManager: tm,
		Authorizer:   authz.NewAuthorizer(&projectPermStore{}),
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService, testCookieCfg),
		User:         handler.NewUserHandler(userService, authService).WithAvatarService(attachmentService),
		GlobalRole:   handler.NewGlobalRoleHandler(&fakeGlobalRoleService{}),
		Log:          log,
	})
	return h, userRepo
}

// buildProjectAvatarTestRouter wires a router with the project avatar
// endpoints (WithProjectAvatarService) backed by real (fake) storage, plus
// enough of the project CRUD surface to create a project to run avatar
// requests against.
func buildProjectAvatarTestRouter(attachRepo *fakeAttachmentRepo, store *fakeStorageClient, permStore *projectPermStore) http.Handler {
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	refreshStore := &fakeRefreshStore{}
	userRepo := newFakeUserRepo()
	authService := authsvc.New(userRepo, tm, refreshStore, 168*time.Hour, 24*time.Hour)
	projectRepo := newFakeProjectRepo()
	taskRepo := newFakeTaskRepoIT()
	projectService := projectsvc.New(projectRepo, taskRepo, nil)
	attachmentService := attachmentsvc.New(attachRepo, attachmentsvc.NewTaskOwnerChecker(taskRepo), store, "test-bucket")
	projectService.WithAvatarService(attachmentService)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return router.New(router.Deps{
		TokenManager:         tm,
		Authorizer:           authz.NewAuthorizer(permStore),
		ProjectVisibilitySvc: projectService,
		Health:               handler.NewHealthHandler(),
		Auth:                 handler.NewAuthHandler(authService, testCookieCfg),
		GlobalRole:           handler.NewGlobalRoleHandler(&fakeGlobalRoleService{}),
		Project: handler.NewProjectHandler(projectService, authz.NewAuthorizer(permStore),
			handler.WithProjectAvatarService(attachmentService)),
		Log: log,
	})
}

// fullPermStore returns a projectPermStore granting all task/attachment perms for the given project.
func fullPermStore(projectID uuid.UUID) *projectPermStore {
	return &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {
				authz.PermissionTasksRead,
				authz.PermissionTasksWrite,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func issueAttachToken(t *testing.T, subject string) string {
	t.Helper()
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	tok, err := tm.IssueAccess(subject, "attach-user", "USER", "fam-attach", false)
	if err != nil {
		t.Fatalf("issue attach token: %v", err)
	}
	return tok
}

func attachPath(projectID, taskID, suffix string) string {
	return fmt.Sprintf("/api/v1/projects/%s/tasks/%s/attachments%s", projectID, taskID, suffix)
}

func seedTask(projectID, taskID uuid.UUID) *taskdom.Task {
	return &taskdom.Task{
		ID:        taskID,
		ProjectID: projectID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func decodeAttachData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return env.Data
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestInitiateUpload_SinglePart(t *testing.T) {
	projectID := uuid.New()
	taskID := uuid.New()
	userID := uuid.New()
	repo := newFakeAttachmentRepo()
	store := newFakeStorageClient()
	r := buildAttachmentTestRouter(repo, store, fullPermStore(projectID), seedTask(projectID, taskID))
	tok := issueAttachToken(t, userID.String())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/initiate-upload"), tok, map[string]any{
		"file_name":    "report.pdf",
		"content_type": "application/pdf",
		"file_size":    1024,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	data := decodeAttachData(t, w)
	if data["is_multipart"] == true {
		t.Errorf("expected single-part upload for small file")
	}
	uploadURL, _ := data["upload_url"].(string)
	if uploadURL == "" {
		t.Errorf("expected non-empty upload_url for single-part upload")
	}
	if _, ok := data["file_id"]; !ok {
		t.Errorf("expected file_id in response")
	}
}

func TestInitiateUpload_Multipart(t *testing.T) {
	projectID := uuid.New()
	taskID := uuid.New()
	repo := newFakeAttachmentRepo()
	store := newFakeStorageClient()
	r := buildAttachmentTestRouter(repo, store, fullPermStore(projectID), seedTask(projectID, taskID))
	tok := issueAttachToken(t, uuid.New().String())

	// 12 MiB > MultipartThreshold (5 MiB)
	const fileSize = 12 * 1024 * 1024
	w := serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/initiate-upload"), tok, map[string]any{
		"file_name":    "large-video.mp4",
		"content_type": "video/mp4",
		"file_size":    fileSize,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	data := decodeAttachData(t, w)
	if data["is_multipart"] != true {
		t.Errorf("expected multipart upload for large file, got %v", data)
	}
	multipart, ok := data["multipart"].(map[string]any)
	if !ok {
		t.Fatalf("expected multipart object in response, got %T", data["multipart"])
	}
	parts, _ := multipart["parts"].([]any)
	if len(parts) == 0 {
		t.Errorf("expected at least one presigned part URL")
	}
}

func TestInitiateUpload_Validation(t *testing.T) {
	projectID := uuid.New()
	taskID := uuid.New()
	repo := newFakeAttachmentRepo()
	store := newFakeStorageClient()
	r := buildAttachmentTestRouter(repo, store, fullPermStore(projectID), seedTask(projectID, taskID))
	tok := issueAttachToken(t, uuid.New().String())

	cases := []struct {
		name       string
		body       map[string]any
		wantStatus int
	}{
		{
			name:       "missing file_name",
			body:       map[string]any{"content_type": "image/png", "file_size": 100},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing content_type",
			body:       map[string]any{"file_name": "img.png", "file_size": 100},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "zero file_size",
			body:       map[string]any{"file_name": "img.png", "content_type": "image/png", "file_size": 0},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/initiate-upload"), tok, tc.body))
			if w.Code != tc.wantStatus {
				t.Errorf("expected %d, got %d: %s", tc.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestInitiateUpload_Unauthenticated(t *testing.T) {
	projectID := uuid.New()
	taskID := uuid.New()
	r := buildAttachmentTestRouter(newFakeAttachmentRepo(), newFakeStorageClient(), fullPermStore(projectID), seedTask(projectID, taskID))

	body, _ := json.Marshal(map[string]any{"file_name": "f.pdf", "content_type": "application/pdf", "file_size": 100})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/initiate-upload"), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := serve(r, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestCompleteUpload_SinglePart(t *testing.T) {
	projectID := uuid.New()
	taskID := uuid.New()
	repo := newFakeAttachmentRepo()
	store := newFakeStorageClient()
	r := buildAttachmentTestRouter(repo, store, fullPermStore(projectID), seedTask(projectID, taskID))
	tok := issueAttachToken(t, uuid.New().String())

	// Step 1: Initiate upload to get file_id.
	wInit := serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/initiate-upload"), tok, map[string]any{
		"file_name":    "doc.pdf",
		"content_type": "application/pdf",
		"file_size":    2048,
	}))
	if wInit.Code != http.StatusCreated {
		t.Fatalf("initiate: expected 201, got %d: %s", wInit.Code, wInit.Body.String())
	}
	initData := decodeAttachData(t, wInit)
	fileID, _ := initData["file_id"].(string)
	if fileID == "" {
		t.Fatalf("no file_id in initiate response")
	}

	// Step 2: Complete upload.
	wComplete := serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/complete-upload"), tok, map[string]any{
		"file_id": fileID,
	}))
	if wComplete.Code != http.StatusCreated {
		t.Fatalf("complete: expected 201, got %d: %s", wComplete.Code, wComplete.Body.String())
	}
	completeData := decodeAttachData(t, wComplete)
	if completeData["task_id"] == nil {
		t.Errorf("expected task_id in complete response")
	}
	fileObj, _ := completeData["file"].(map[string]any)
	if fileObj == nil {
		t.Errorf("expected file object in complete response")
	}
}

func TestCompleteUpload_FileNotFound(t *testing.T) {
	projectID := uuid.New()
	taskID := uuid.New()
	r := buildAttachmentTestRouter(newFakeAttachmentRepo(), newFakeStorageClient(), fullPermStore(projectID), seedTask(projectID, taskID))
	tok := issueAttachToken(t, uuid.New().String())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/complete-upload"), tok, map[string]any{
		"file_id": uuid.New().String(),
	}))
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCompleteUpload_AlreadyCompleted(t *testing.T) {
	projectID := uuid.New()
	taskID := uuid.New()
	repo := newFakeAttachmentRepo()
	store := newFakeStorageClient()
	r := buildAttachmentTestRouter(repo, store, fullPermStore(projectID), seedTask(projectID, taskID))
	tok := issueAttachToken(t, uuid.New().String())

	// Initiate.
	wInit := serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/initiate-upload"), tok, map[string]any{
		"file_name":    "x.png",
		"content_type": "image/png",
		"file_size":    512,
	}))
	fileID, _ := decodeAttachData(t, wInit)["file_id"].(string)

	// Complete once.
	completeBody := map[string]any{"file_id": fileID}
	wFirst := serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/complete-upload"), tok, completeBody))
	if wFirst.Code != http.StatusCreated {
		t.Fatalf("first complete: expected 201, got %d", wFirst.Code)
	}

	// Complete again → 409 (upload not pending).
	wSecond := serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/complete-upload"), tok, completeBody))
	if wSecond.Code != http.StatusConflict {
		t.Errorf("second complete: expected 409, got %d: %s", wSecond.Code, wSecond.Body.String())
	}
}

func TestListTaskAttachments(t *testing.T) {
	projectID := uuid.New()
	taskID := uuid.New()
	repo := newFakeAttachmentRepo()
	store := newFakeStorageClient()
	r := buildAttachmentTestRouter(repo, store, fullPermStore(projectID), seedTask(projectID, taskID))
	tok := issueAttachToken(t, uuid.New().String())

	path := attachPath(projectID.String(), taskID.String(), "")

	// List empty.
	wEmpty := serve(r, authedJSONReq(t.Context(), http.MethodGet, path, tok, nil))
	if wEmpty.Code != http.StatusOK {
		t.Fatalf("list empty: expected 200, got %d", wEmpty.Code)
	}
	var listEnvEmpty struct {
		Data struct {
			Items []any `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(wEmpty.Body).Decode(&listEnvEmpty); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listEnvEmpty.Data.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(listEnvEmpty.Data.Items))
	}

	// Add two attachments via initiate + complete.
	for i := range 2 {
		wInit := serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/initiate-upload"), tok, map[string]any{
			"file_name":    fmt.Sprintf("f%d.txt", i),
			"content_type": "text/plain",
			"file_size":    100,
		}))
		fid, _ := decodeAttachData(t, wInit)["file_id"].(string)
		serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/complete-upload"), tok, map[string]any{"file_id": fid}))
	}

	wList := serve(r, authedJSONReq(t.Context(), http.MethodGet, path, tok, nil))
	if wList.Code != http.StatusOK {
		t.Fatalf("list after uploads: expected 200, got %d", wList.Code)
	}
	var listEnv struct {
		Data struct {
			Items []any `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(wList.Body).Decode(&listEnv); err != nil {
		t.Fatalf("decode list2: %v", err)
	}
	if len(listEnv.Data.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(listEnv.Data.Items))
	}
}

func TestGetDownloadURL(t *testing.T) {
	projectID := uuid.New()
	taskID := uuid.New()
	repo := newFakeAttachmentRepo()
	store := newFakeStorageClient()
	r := buildAttachmentTestRouter(repo, store, fullPermStore(projectID), seedTask(projectID, taskID))
	tok := issueAttachToken(t, uuid.New().String())

	// Create an attachment.
	wInit := serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/initiate-upload"), tok, map[string]any{
		"file_name":    "img.jpg",
		"content_type": "image/jpeg",
		"file_size":    4096,
	}))
	fileID, _ := decodeAttachData(t, wInit)["file_id"].(string)

	wComplete := serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/complete-upload"), tok, map[string]any{"file_id": fileID}))
	attachmentID, _ := decodeAttachData(t, wComplete)["id"].(string)

	// Preview URL (no download param).
	dlPath := attachPath(projectID.String(), taskID.String(), fmt.Sprintf("/%s/download-url", attachmentID))
	wPreview := serve(r, authedJSONReq(t.Context(), http.MethodGet, dlPath, tok, nil))
	if wPreview.Code != http.StatusOK {
		t.Fatalf("download-url: expected 200, got %d: %s", wPreview.Code, wPreview.Body.String())
	}
	var previewEnv struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(wPreview.Body).Decode(&previewEnv); err != nil {
		t.Fatalf("decode download url: %v", err)
	}
	if previewEnv.Data.URL == "" {
		t.Errorf("expected non-empty download URL")
	}
	if bytes.Contains([]byte(previewEnv.Data.URL), []byte("attachment")) {
		t.Errorf("preview URL should not contain attachment content-disposition, got %q", previewEnv.Data.URL)
	}

	// Force-download URL (?download=true).
	wForce := serve(r, authedJSONReq(t.Context(), http.MethodGet, dlPath+"?download=true", tok, nil))
	if wForce.Code != http.StatusOK {
		t.Fatalf("download-url force: expected 200, got %d: %s", wForce.Code, wForce.Body.String())
	}
	var forceEnv struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(wForce.Body).Decode(&forceEnv); err != nil {
		t.Fatalf("decode download url force: %v", err)
	}
	if !bytes.Contains([]byte(forceEnv.Data.URL), []byte("attachment")) {
		t.Errorf("force-download URL should contain attachment content-disposition, got %q", forceEnv.Data.URL)
	}
}

func TestGetDownloadURL_NotFound(t *testing.T) {
	projectID := uuid.New()
	taskID := uuid.New()
	r := buildAttachmentTestRouter(newFakeAttachmentRepo(), newFakeStorageClient(), fullPermStore(projectID), seedTask(projectID, taskID))
	tok := issueAttachToken(t, uuid.New().String())

	dlPath := attachPath(projectID.String(), taskID.String(), fmt.Sprintf("/%s/download-url", uuid.New().String()))
	w := serve(r, authedJSONReq(t.Context(), http.MethodGet, dlPath, tok, nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteTaskAttachment(t *testing.T) {
	projectID := uuid.New()
	taskID := uuid.New()
	repo := newFakeAttachmentRepo()
	store := newFakeStorageClient()
	r := buildAttachmentTestRouter(repo, store, fullPermStore(projectID), seedTask(projectID, taskID))
	tok := issueAttachToken(t, uuid.New().String())

	// Create attachment.
	wInit := serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/initiate-upload"), tok, map[string]any{
		"file_name":    "to-delete.txt",
		"content_type": "text/plain",
		"file_size":    128,
	}))
	fileID, _ := decodeAttachData(t, wInit)["file_id"].(string)

	wComplete := serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectID.String(), taskID.String(), "/complete-upload"), tok, map[string]any{"file_id": fileID}))
	attachmentID, _ := decodeAttachData(t, wComplete)["id"].(string)

	// Delete.
	wDel := serve(r, authedJSONReq(t.Context(), http.MethodDelete, attachPath(projectID.String(), taskID.String(), fmt.Sprintf("/%s", attachmentID)), tok, nil))
	if wDel.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", wDel.Code, wDel.Body.String())
	}

	// List should now be empty.
	wList := serve(r, authedJSONReq(t.Context(), http.MethodGet, attachPath(projectID.String(), taskID.String(), ""), tok, nil))
	var listEnv struct {
		Data struct {
			Items []any `json:"items"`
		} `json:"data"`
	}
	_ = json.NewDecoder(wList.Body).Decode(&listEnv)
	if len(listEnv.Data.Items) != 0 {
		t.Errorf("expected 0 items after delete, got %d", len(listEnv.Data.Items))
	}

	// File record should still exist (only unlinked, not deleted).
	fid, _ := uuid.Parse(fileID)
	if _, err := repo.FindFileByID(context.Background(), fid); err != nil {
		t.Errorf("file record should still exist after attachment delete: %v", err)
	}

	// Storage object should NOT have been deleted.
	if len(store.deletedKeys) != 0 {
		t.Errorf("expected 0 deleted storage keys (only unlink), got %d", len(store.deletedKeys))
	}
}

func TestDeleteTaskAttachment_NotFound(t *testing.T) {
	projectID := uuid.New()
	taskID := uuid.New()
	r := buildAttachmentTestRouter(newFakeAttachmentRepo(), newFakeStorageClient(), fullPermStore(projectID), seedTask(projectID, taskID))
	tok := issueAttachToken(t, uuid.New().String())

	w := serve(r, authedJSONReq(t.Context(), http.MethodDelete, attachPath(projectID.String(), taskID.String(), fmt.Sprintf("/%s", uuid.New().String())), tok, nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCrossProjectAccess_Denied(t *testing.T) {
	projectA := uuid.New()
	projectB := uuid.New()
	taskA := uuid.New()
	userID := uuid.New()
	repo := newFakeAttachmentRepo()
	store := newFakeStorageClient()
	permStore := fullPermStore(projectA)
	permStore.projectPerms[projectB] = permStore.projectPerms[projectA]
	r := buildAttachmentTestRouter(repo, store, permStore, seedTask(projectA, taskA))
	tok := issueAttachToken(t, userID.String())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost, attachPath(projectB.String(), taskA.String(), "/initiate-upload"), tok, map[string]any{
		"file_name":    "stolen.pdf",
		"content_type": "application/pdf",
		"file_size":    1024,
	}))
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-project initiate: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	wList := serve(r, authedJSONReq(t.Context(), http.MethodGet, attachPath(projectB.String(), taskA.String(), ""), tok, nil))
	if wList.Code != http.StatusNotFound {
		t.Errorf("cross-project list: expected 404, got %d: %s", wList.Code, wList.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Avatar tests
// ---------------------------------------------------------------------------

// fakePNG returns a tiny solid-color PNG so CompleteAvatarUpload has real
// image bytes to decode/resize.
func fakePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture PNG: %v", err)
	}
	return buf.Bytes()
}

// keyFromFakeUploadURL recovers the storage key the test harness's
// fakeStorageClient embedded in a presigned PUT URL
// ("https://fake-storage/{bucket}/{key}?sig=put"), so the test can simulate
// the client's direct-to-storage PUT by writing straight into the fake's
// object map before calling complete-upload.
func keyFromFakeUploadURL(t *testing.T, bucket, uploadURL string) string {
	t.Helper()
	prefix := fmt.Sprintf("https://fake-storage/%s/", bucket)
	key, ok := strings.CutPrefix(uploadURL, prefix)
	if !ok {
		t.Fatalf("upload URL %q does not have expected prefix %q", uploadURL, prefix)
	}
	key, _, _ = strings.Cut(key, "?")
	return key
}

func TestAvatarUpload_SelfService(t *testing.T) {
	userID := uuid.New()
	attachRepo := newFakeAttachmentRepo()
	store := newFakeStorageClient()
	r, userRepo := buildAvatarTestRouter(attachRepo, store)
	if err := userRepo.Create(context.Background(), &userdom.User{
		ID:       userID,
		Username: "avatartester",
		FullName: "Avatar Tester",
		Role:     userdom.RoleUser,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	tok := issueAttachToken(t, userID.String())

	pngBytes := fakePNG(t, 300, 200)

	// Step 1: initiate.
	wInit := serve(r, authedJSONReq(t.Context(), http.MethodPost, "/api/v1/users/me/avatar/initiate-upload", tok, map[string]any{
		"file_name":    "me.png",
		"content_type": "image/png",
		"file_size":    len(pngBytes),
	}))
	if wInit.Code != http.StatusCreated {
		t.Fatalf("initiate: expected 201, got %d: %s", wInit.Code, wInit.Body.String())
	}
	initData := decodeAttachData(t, wInit)
	fileID, _ := initData["file_id"].(string)
	uploadURL, _ := initData["upload_url"].(string)
	if fileID == "" || uploadURL == "" {
		t.Fatalf("missing file_id/upload_url in initiate response: %v", initData)
	}

	// Step 2: simulate the client's direct-to-storage PUT.
	store.PutObject(context.Background(), "test-bucket", keyFromFakeUploadURL(t, "test-bucket", uploadURL), "image/png", pngBytes) //nolint:errcheck

	// Step 3: complete.
	wComplete := serve(r, authedJSONReq(t.Context(), http.MethodPost, "/api/v1/users/me/avatar/complete-upload", tok, map[string]any{
		"file_id": fileID,
	}))
	if wComplete.Code != http.StatusOK {
		t.Fatalf("complete: expected 200, got %d: %s", wComplete.Code, wComplete.Body.String())
	}
	completeData := decodeAttachData(t, wComplete)
	avatarURL, _ := completeData["avatar_url"].(string)
	thumbURL, _ := completeData["avatar_thumb_url"].(string)
	if avatarURL == "" || thumbURL == "" {
		t.Fatalf("expected avatar_url and avatar_thumb_url in complete response, got %v", completeData)
	}

	// Step 4: GetMe should now also report the avatar.
	wMe := serve(r, authedJSONReq(t.Context(), http.MethodGet, "/api/v1/users/me", tok, nil))
	if wMe.Code != http.StatusOK {
		t.Fatalf("get me: expected 200, got %d: %s", wMe.Code, wMe.Body.String())
	}
	meData := decodeAttachData(t, wMe)
	if meData["avatar_url"] == nil || meData["avatar_url"] == "" {
		t.Errorf("expected GetMe to report avatar_url after upload, got %v", meData)
	}

	// Step 5: remove the avatar.
	wDelete := serve(r, authedJSONReq(t.Context(), http.MethodDelete, "/api/v1/users/me/avatar", tok, nil))
	if wDelete.Code != http.StatusOK {
		t.Fatalf("delete avatar: expected 200, got %d: %s", wDelete.Code, wDelete.Body.String())
	}
	deleteData := decodeAttachData(t, wDelete)
	if deleteData["avatar_url"] != nil {
		t.Errorf("expected avatar_url to be cleared after delete, got %v", deleteData["avatar_url"])
	}
}

func TestAvatarUpload_RejectsNonImageContentType(t *testing.T) {
	userID := uuid.New()
	r, userRepo := buildAvatarTestRouter(newFakeAttachmentRepo(), newFakeStorageClient())
	if err := userRepo.Create(context.Background(), &userdom.User{
		ID: userID, Username: "avatartester2", FullName: "Avatar Tester 2", Role: userdom.RoleUser,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	tok := issueAttachToken(t, userID.String())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost, "/api/v1/users/me/avatar/initiate-upload", tok, map[string]any{
		"file_name":    "malware.exe",
		"content_type": "application/octet-stream",
		"file_size":    1024,
	}))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-image content type, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAvatarUpload_RejectsOversizedFile(t *testing.T) {
	userID := uuid.New()
	r, userRepo := buildAvatarTestRouter(newFakeAttachmentRepo(), newFakeStorageClient())
	if err := userRepo.Create(context.Background(), &userdom.User{
		ID: userID, Username: "avatartester3", FullName: "Avatar Tester 3", Role: userdom.RoleUser,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	tok := issueAttachToken(t, userID.String())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost, "/api/v1/users/me/avatar/initiate-upload", tok, map[string]any{
		"file_name":    "huge.png",
		"content_type": "image/png",
		"file_size":    attachmentdom.MaxAvatarUploadSize + 1,
	}))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for oversized file, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAvatarUpload_OwnerMismatch_RejectsCompletingAnotherUsersUpload proves a
// file initiated for one owner can't be completed against a different
// owner's avatar, even if the second owner learns the file_id — the storage
// key embeds the owning user's ID, and CompleteAvatarUpload checks it.
func TestAvatarUpload_OwnerMismatch_RejectsCompletingAnotherUsersUpload(t *testing.T) {
	userA, userB := uuid.New(), uuid.New()
	repo := newFakeAttachmentRepo()
	store := newFakeStorageClient()
	r, userRepo := buildAvatarTestRouter(repo, store)
	for _, u := range []*userdom.User{
		{ID: userA, Username: "owner-a", FullName: "Owner A", Role: userdom.RoleUser},
		{ID: userB, Username: "owner-b", FullName: "Owner B", Role: userdom.RoleUser},
	} {
		if err := userRepo.Create(context.Background(), u); err != nil {
			t.Fatalf("seed user %s: %v", u.Username, err)
		}
	}
	tokA := issueAttachToken(t, userA.String())
	tokB := issueAttachToken(t, userB.String())

	pngBytes := fakePNG(t, 50, 50)

	// User A initiates and "uploads" to storage.
	wInit := serve(r, authedJSONReq(t.Context(), http.MethodPost, "/api/v1/users/me/avatar/initiate-upload", tokA, map[string]any{
		"file_name":    "a.png",
		"content_type": "image/png",
		"file_size":    len(pngBytes),
	}))
	if wInit.Code != http.StatusCreated {
		t.Fatalf("initiate as user A: expected 201, got %d: %s", wInit.Code, wInit.Body.String())
	}
	initData := decodeAttachData(t, wInit)
	fileID, _ := initData["file_id"].(string)
	uploadURL, _ := initData["upload_url"].(string)
	store.PutObject(context.Background(), "test-bucket", keyFromFakeUploadURL(t, "test-bucket", uploadURL), "image/png", pngBytes) //nolint:errcheck

	// User B tries to complete A's upload as their own avatar.
	wComplete := serve(r, authedJSONReq(t.Context(), http.MethodPost, "/api/v1/users/me/avatar/complete-upload", tokB, map[string]any{
		"file_id": fileID,
	}))
	if wComplete.Code != http.StatusNotFound {
		t.Fatalf("complete as user B: expected 404 (owner mismatch), got %d: %s", wComplete.Code, wComplete.Body.String())
	}

	// User A can still complete their own upload afterwards.
	wCompleteA := serve(r, authedJSONReq(t.Context(), http.MethodPost, "/api/v1/users/me/avatar/complete-upload", tokA, map[string]any{
		"file_id": fileID,
	}))
	if wCompleteA.Code != http.StatusOK {
		t.Fatalf("complete as user A: expected 200, got %d: %s", wCompleteA.Code, wCompleteA.Body.String())
	}
}

// TestAvatarUpload_ActualBytesExceedDeclaredSize_RejectedAtComplete proves
// that a client can't bypass the size cap by declaring a small file_size at
// initiate time and then PUTting more bytes directly to the presigned URL —
// CompleteAvatarUpload re-checks the actual downloaded object size before
// doing any decode work on it.
func TestAvatarUpload_ActualBytesExceedDeclaredSize_RejectedAtComplete(t *testing.T) {
	userID := uuid.New()
	store := newFakeStorageClient()
	r, userRepo := buildAvatarTestRouter(newFakeAttachmentRepo(), store)
	if err := userRepo.Create(context.Background(), &userdom.User{
		ID: userID, Username: "avatartester4", FullName: "Avatar Tester 4", Role: userdom.RoleUser,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	tok := issueAttachToken(t, userID.String())

	// Declare a small, well-under-the-cap size at initiate time.
	wInit := serve(r, authedJSONReq(t.Context(), http.MethodPost, "/api/v1/users/me/avatar/initiate-upload", tok, map[string]any{
		"file_name":    "lies.png",
		"content_type": "image/png",
		"file_size":    1024,
	}))
	if wInit.Code != http.StatusCreated {
		t.Fatalf("initiate: expected 201, got %d: %s", wInit.Code, wInit.Body.String())
	}
	initData := decodeAttachData(t, wInit)
	fileID, _ := initData["file_id"].(string)
	uploadURL, _ := initData["upload_url"].(string)

	// Simulate the client PUTting more bytes than it declared — nothing
	// about the presigned URL itself enforces the declared size.
	oversized := bytes.Repeat([]byte{0xAB}, attachmentdom.MaxAvatarUploadSize+1024)
	store.PutObject(context.Background(), "test-bucket", keyFromFakeUploadURL(t, "test-bucket", uploadURL), "image/png", oversized) //nolint:errcheck

	wComplete := serve(r, authedJSONReq(t.Context(), http.MethodPost, "/api/v1/users/me/avatar/complete-upload", tok, map[string]any{
		"file_id": fileID,
	}))
	if wComplete.Code != http.StatusBadRequest {
		t.Errorf("complete with oversized actual upload: expected 400, got %d: %s", wComplete.Code, wComplete.Body.String())
	}
}

// TestAvatarUpload_Project_FullFlow covers the initiate/upload/complete/
// delete HTTP flow for a project avatar — the self-service user flow is
// covered by TestAvatarUpload_SelfService above, but that leaves the
// project (and agent) owner kinds without any full-router coverage of their
// own; a project avatar exercises the same CompleteAvatarUpload code path
// through a different owner kind and permission-middleware branch
// (PermissionProjectsWrite instead of the always-self "me" routes).
func TestAvatarUpload_Project_FullFlow(t *testing.T) {
	store := newFakeStorageClient()
	permStore := &projectPermStore{
		globalPerms: []authz.Permission{
			authz.PermissionProjectsRead,
			authz.PermissionProjectsWrite,
			authz.PermissionProjectsCreate,
		},
	}
	r := buildProjectAvatarTestRouter(newFakeAttachmentRepo(), store, permStore)
	tok := issueProjectToken(t, uuid.NewString())

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, "/api/v1/projects", tok, map[string]any{
		"name": "Avatar Project",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	projectID := projectIDFromCreate(t, createW)
	avatarPath := "/api/v1/projects/" + projectID + "/avatar"

	pngBytes := fakePNG(t, 120, 80)

	wInit := serve(r, authedJSONReq(t.Context(), http.MethodPost, avatarPath+"/initiate-upload", tok, map[string]any{
		"file_name":    "project.png",
		"content_type": "image/png",
		"file_size":    len(pngBytes),
	}))
	if wInit.Code != http.StatusCreated {
		t.Fatalf("initiate: expected 201, got %d: %s", wInit.Code, wInit.Body.String())
	}
	initData := decodeAttachData(t, wInit)
	fileID, _ := initData["file_id"].(string)
	uploadURL, _ := initData["upload_url"].(string)
	store.PutObject(context.Background(), "test-bucket", keyFromFakeUploadURL(t, "test-bucket", uploadURL), "image/png", pngBytes) //nolint:errcheck

	wComplete := serve(r, authedJSONReq(t.Context(), http.MethodPost, avatarPath+"/complete-upload", tok, map[string]any{
		"file_id": fileID,
	}))
	if wComplete.Code != http.StatusOK {
		t.Fatalf("complete: expected 200, got %d: %s", wComplete.Code, wComplete.Body.String())
	}
	completeData := decodeAttachData(t, wComplete)
	if completeData["avatar_url"] == nil || completeData["avatar_url"] == "" {
		t.Errorf("expected avatar_url in complete response, got %v", completeData)
	}

	wGet := serve(r, authedJSONReq(t.Context(), http.MethodGet, "/api/v1/projects/"+projectID, tok, nil))
	if wGet.Code != http.StatusOK {
		t.Fatalf("get project: expected 200, got %d: %s", wGet.Code, wGet.Body.String())
	}
	getData := decodeAttachData(t, wGet)
	if getData["avatar_url"] == nil || getData["avatar_url"] == "" {
		t.Errorf("expected GetProject to report avatar_url after upload, got %v", getData)
	}

	wDelete := serve(r, authedJSONReq(t.Context(), http.MethodDelete, avatarPath, tok, nil))
	if wDelete.Code != http.StatusOK {
		t.Fatalf("delete avatar: expected 200, got %d: %s", wDelete.Code, wDelete.Body.String())
	}
	deleteData := decodeAttachData(t, wDelete)
	if deleteData["avatar_url"] != nil {
		t.Errorf("expected avatar_url to be cleared after delete, got %v", deleteData["avatar_url"])
	}
}

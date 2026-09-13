package annotationsvc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	annotationdom "github.com/Paca-AI/api/internal/domain/annotation"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// ---------------------------------------------------------------------------
// verifyAnnotationScreenshotFile
// ---------------------------------------------------------------------------

func TestVerifyAnnotationScreenshotFile(t *testing.T) {
	uploader := uuid.New()
	other := uuid.New()

	cases := []struct {
		name    string
		file    *attachmentdom.File
		wantErr bool
	}{
		{
			name:    "correct prefix and matching uploader passes",
			file:    &attachmentdom.File{StorageKey: "annotations/" + uuid.New().String() + "/screenshot.png", UploadedBy: &uploader},
			wantErr: false,
		},
		{
			name:    "wrong domain prefix is rejected (task attachment)",
			file:    &attachmentdom.File{StorageKey: "tasks/" + uuid.New().String() + "/file.png", UploadedBy: &uploader},
			wantErr: true,
		},
		{
			name:    "wrong domain prefix is rejected (doc file)",
			file:    &attachmentdom.File{StorageKey: "docs/" + uuid.New().String() + "/file.png", UploadedBy: &uploader},
			wantErr: true,
		},
		{
			name:    "wrong domain prefix is rejected (avatar)",
			file:    &attachmentdom.File{StorageKey: "avatars/user/" + uuid.New().String() + "/full.png", UploadedBy: &uploader},
			wantErr: true,
		},
		{
			name:    "correct prefix but a different uploader is rejected",
			file:    &attachmentdom.File{StorageKey: "annotations/" + uuid.New().String() + "/screenshot.png", UploadedBy: &other},
			wantErr: true,
		},
		{
			name:    "correct prefix but no uploader recorded is rejected",
			file:    &attachmentdom.File{StorageKey: "annotations/" + uuid.New().String() + "/screenshot.png", UploadedBy: nil},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyAnnotationScreenshotFile(tc.file, uploader)
			if tc.wantErr && !errors.Is(err, annotationdom.ErrAnnotationScreenshotMismatch) {
				t.Fatalf("expected ErrAnnotationScreenshotMismatch, got %v", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// fakes — only the methods CompleteScreenshotUpload/GetScreenshotURL
// actually call are meaningfully implemented; the rest are unused stubs
// satisfying the interfaces.
// ---------------------------------------------------------------------------

type fakeAnnotationRepo struct {
	annotations map[uuid.UUID]*annotationdom.PageAnnotation
	// claimTaskCreationOverride, when set, replaces the default
	// map-based ClaimTaskCreation behavior — used to simulate a claim
	// already held by another (or a very recent) call.
	claimTaskCreationOverride func() (bool, error)
	// setTaskIDFailures counts down on each SetTaskID call, failing while
	// > 0 — used to simulate transient failures that setTaskIDWithRetry
	// should recover from.
	setTaskIDFailures int
	setTaskIDCalls    int
}

func (r *fakeAnnotationRepo) ListForPage(context.Context, uuid.UUID, string) ([]*annotationdom.PageAnnotation, error) {
	return nil, nil
}
func (r *fakeAnnotationRepo) ListForPortForward(context.Context, uuid.UUID) ([]*annotationdom.PageAnnotation, error) {
	return nil, nil
}
func (r *fakeAnnotationRepo) FindVisibleInProject(_ context.Context, projectID, annotationID uuid.UUID) (*annotationdom.PageAnnotation, error) {
	a, ok := r.annotations[annotationID]
	if !ok || a.ProjectID != projectID {
		return nil, annotationdom.ErrAnnotationNotFound
	}
	return a, nil
}
func (r *fakeAnnotationRepo) SearchInProject(context.Context, uuid.UUID, annotationdom.SearchFilter) ([]*annotationdom.PageAnnotation, bool, error) {
	return nil, false, nil
}
func (r *fakeAnnotationRepo) Create(context.Context, *annotationdom.PageAnnotation) error {
	return nil
}
func (r *fakeAnnotationRepo) SetScreenshotFileID(_ context.Context, id, fileID uuid.UUID) error {
	if a, ok := r.annotations[id]; ok {
		a.ScreenshotFileID = &fileID
	}
	return nil
}
func (r *fakeAnnotationRepo) SetStatus(context.Context, uuid.UUID, string, *uuid.UUID, *time.Time) error {
	return nil
}
func (r *fakeAnnotationRepo) SetTaskID(_ context.Context, id, taskID uuid.UUID) error {
	r.setTaskIDCalls++
	if r.setTaskIDFailures > 0 {
		r.setTaskIDFailures--
		return errors.New("transient failure")
	}
	if a, ok := r.annotations[id]; ok {
		a.TaskID = &taskID
	}
	return nil
}
func (r *fakeAnnotationRepo) ClaimTaskCreation(_ context.Context, id uuid.UUID) (bool, error) {
	if r.claimTaskCreationOverride != nil {
		return r.claimTaskCreationOverride()
	}
	a, ok := r.annotations[id]
	if !ok || a.TaskID != nil {
		return false, nil
	}
	return true, nil
}
func (r *fakeAnnotationRepo) AddComment(context.Context, *annotationdom.AnnotationComment) error {
	return nil
}
func (r *fakeAnnotationRepo) CreatePendingScreenshotFile(context.Context, uuid.UUID, uuid.UUID, string, string, string, string, int64) error {
	return nil
}
func (r *fakeAnnotationRepo) MarkScreenshotFileUploaded(context.Context, uuid.UUID) error {
	return nil
}
func (r *fakeAnnotationRepo) ResolvePortForward(context.Context, uuid.UUID, int) (*annotationdom.PortForwardMatch, error) {
	return nil, annotationdom.ErrPortForwardNotFound
}

var _ annotationdom.Repository = (*fakeAnnotationRepo)(nil)

type fakeFileFinder struct {
	files map[uuid.UUID]*attachmentdom.File
}

func (f *fakeFileFinder) FindFileByID(_ context.Context, id uuid.UUID) (*attachmentdom.File, error) {
	file, ok := f.files[id]
	if !ok {
		return nil, errors.New("file not found")
	}
	return file, nil
}

var _ FileFinder = (*fakeFileFinder)(nil)

// ---------------------------------------------------------------------------
// CompleteScreenshotUpload / GetScreenshotURL
// ---------------------------------------------------------------------------

func TestCompleteScreenshotUpload_ForeignFile_Rejected(t *testing.T) {
	projectID := uuid.New()
	annotationID := uuid.New()
	victim := uuid.New()
	attacker := uuid.New()

	// A file the victim uploaded through their own InitiateScreenshotUpload
	// call, in some other project/annotation the attacker has no access to.
	foreignFileID := uuid.New()
	repo := &fakeAnnotationRepo{annotations: map[uuid.UUID]*annotationdom.PageAnnotation{
		annotationID: {ID: annotationID, ProjectID: projectID, CreatedBy: attacker},
	}}
	files := &fakeFileFinder{files: map[uuid.UUID]*attachmentdom.File{
		foreignFileID: {ID: foreignFileID, StorageKey: "annotations/" + foreignFileID.String() + "/shot.png", UploadedBy: &victim},
	}}
	svc := New(repo, nil, nil, nil, files, nil, "test-bucket")

	// The attacker knows foreignFileID (e.g. leaked, or from another
	// annotation they can see) and tries to attach it to their own
	// annotation by completing "their own" upload with it.
	_, err := svc.CompleteScreenshotUpload(context.Background(), projectID, annotationID, foreignFileID, attacker)
	if !errors.Is(err, annotationdom.ErrAnnotationScreenshotMismatch) {
		t.Fatalf("expected ErrAnnotationScreenshotMismatch, got %v", err)
	}
	if repo.annotations[annotationID].ScreenshotFileID != nil {
		t.Error("a rejected foreign file must never be attached to the annotation")
	}
}

func TestCompleteScreenshotUpload_OwnFile_Succeeds(t *testing.T) {
	projectID := uuid.New()
	annotationID := uuid.New()
	uploader := uuid.New()

	fileID := uuid.New()
	repo := &fakeAnnotationRepo{annotations: map[uuid.UUID]*annotationdom.PageAnnotation{
		annotationID: {ID: annotationID, ProjectID: projectID, CreatedBy: uploader},
	}}
	files := &fakeFileFinder{files: map[uuid.UUID]*attachmentdom.File{
		fileID: {ID: fileID, StorageKey: "annotations/" + fileID.String() + "/shot.png", UploadedBy: &uploader},
	}}
	svc := New(repo, nil, nil, nil, files, nil, "test-bucket")

	if _, err := svc.CompleteScreenshotUpload(context.Background(), projectID, annotationID, fileID, uploader); err != nil {
		t.Fatalf("expected the uploader's own screenshot to be accepted, got %v", err)
	}
	if repo.annotations[annotationID].ScreenshotFileID == nil || *repo.annotations[annotationID].ScreenshotFileID != fileID {
		t.Error("expected the annotation's screenshot_file_id to be set")
	}
}

// fakeStoreNoGet fails any attempt to actually presign, so
// GetScreenshotURL_LegitimateScreenshot_Succeeds only proves the ownership
// check passed, not that presigning itself works — mirrors
// attachmentsvc's own doc_service_test.go convention of using an
// unimplemented store stub for the same reason.
type fakeStoreNoGet struct{}

func (fakeStoreNoGet) PresignPutObject(context.Context, string, string, string, time.Duration) (string, error) {
	return "", errors.New("not implemented")
}
func (fakeStoreNoGet) PresignGetObject(context.Context, string, string, time.Duration, string) (string, error) {
	return "https://example.com/presigned", nil
}

var _ ObjectStore = fakeStoreNoGet{}

func TestGetScreenshotURL_WrongDomainPrefix_Rejected(t *testing.T) {
	// Simulates a screenshot_file_id that ended up pointing at a file from
	// a completely different domain (a task attachment, doc, or avatar —
	// e.g. a future bug elsewhere, or data from before this check existed)
	// — GetScreenshotURL must not hand out a presigned URL for it, even
	// though Create/CompleteScreenshotUpload already guard the write side.
	projectID := uuid.New()
	annotationID := uuid.New()
	creator := uuid.New()

	fileID := uuid.New()
	repo := &fakeAnnotationRepo{annotations: map[uuid.UUID]*annotationdom.PageAnnotation{
		annotationID: {ID: annotationID, ProjectID: projectID, CreatedBy: creator, ScreenshotFileID: &fileID},
	}}
	files := &fakeFileFinder{files: map[uuid.UUID]*attachmentdom.File{
		fileID: {ID: fileID, StorageKey: "tasks/" + fileID.String() + "/file.png", UploadedBy: &creator},
	}}
	svc := New(repo, nil, nil, nil, files, fakeStoreNoGet{}, "test-bucket")

	_, err := svc.GetScreenshotURL(context.Background(), projectID, annotationID)
	if !errors.Is(err, annotationdom.ErrAnnotationScreenshotMismatch) {
		t.Fatalf("expected ErrAnnotationScreenshotMismatch, got %v", err)
	}
}

func TestGetScreenshotURL_LegitimateScreenshot_Succeeds(t *testing.T) {
	projectID := uuid.New()
	annotationID := uuid.New()
	creator := uuid.New()

	fileID := uuid.New()
	repo := &fakeAnnotationRepo{annotations: map[uuid.UUID]*annotationdom.PageAnnotation{
		annotationID: {ID: annotationID, ProjectID: projectID, CreatedBy: creator, ScreenshotFileID: &fileID},
	}}
	files := &fakeFileFinder{files: map[uuid.UUID]*attachmentdom.File{
		fileID: {ID: fileID, StorageKey: "annotations/" + fileID.String() + "/shot.png", UploadedBy: &creator},
	}}
	svc := New(repo, nil, nil, nil, files, fakeStoreNoGet{}, "test-bucket")

	url, err := svc.GetScreenshotURL(context.Background(), projectID, annotationID)
	if err != nil {
		t.Fatalf("expected a legitimate screenshot to presign successfully, got %v", err)
	}
	if url == "" {
		t.Error("expected a non-empty presigned URL")
	}
}

func TestGetScreenshotURL_UploaderDifferentFromCreator_StillSucceeds(t *testing.T) {
	// annotations.write lets any project member complete a screenshot
	// upload onto any annotation, not just their own (see router.go's
	// "Resolve is a separate permission tier" reasoning for the analogous
	// case) — a screenshot legitimately attached by someone other than the
	// annotation's own creator must still be viewable by the whole project,
	// not just rejected because the uploader and creator differ.
	projectID := uuid.New()
	annotationID := uuid.New()
	creator := uuid.New()
	teammateWhoAttachedIt := uuid.New()

	fileID := uuid.New()
	repo := &fakeAnnotationRepo{annotations: map[uuid.UUID]*annotationdom.PageAnnotation{
		annotationID: {ID: annotationID, ProjectID: projectID, CreatedBy: creator, ScreenshotFileID: &fileID},
	}}
	files := &fakeFileFinder{files: map[uuid.UUID]*attachmentdom.File{
		fileID: {ID: fileID, StorageKey: "annotations/" + fileID.String() + "/shot.png", UploadedBy: &teammateWhoAttachedIt},
	}}
	svc := New(repo, nil, nil, nil, files, fakeStoreNoGet{}, "test-bucket")

	if _, err := svc.GetScreenshotURL(context.Background(), projectID, annotationID); err != nil {
		t.Fatalf("expected a screenshot attached by a teammate (not the annotation's creator) to still be viewable, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// CreateTaskFromAnnotation
// ---------------------------------------------------------------------------

type fakeTaskCreator struct {
	calls int
}

func (c *fakeTaskCreator) CreateTask(_ context.Context, in taskdom.CreateTaskInput) (*taskdom.Task, error) {
	c.calls++
	return &taskdom.Task{ID: uuid.New(), ProjectID: in.ProjectID, Title: in.Title}, nil
}

var _ TaskCreator = (*fakeTaskCreator)(nil)

func TestCreateTaskFromAnnotation_AlreadyHasTask_Rejected(t *testing.T) {
	projectID := uuid.New()
	annotationID := uuid.New()
	existingTaskID := uuid.New()
	repo := &fakeAnnotationRepo{annotations: map[uuid.UUID]*annotationdom.PageAnnotation{
		annotationID: {ID: annotationID, ProjectID: projectID, Body: "fix this", TaskID: &existingTaskID},
	}}
	tasks := &fakeTaskCreator{}
	svc := New(repo, nil, tasks, nil, nil, nil, "test-bucket")

	_, err := svc.CreateTaskFromAnnotation(context.Background(), projectID, annotationID, annotationdom.CreateTaskFromAnnotationInput{ReporterID: uuid.New()})
	if !errors.Is(err, annotationdom.ErrAnnotationAlreadyHasTask) {
		t.Fatalf("expected ErrAnnotationAlreadyHasTask, got %v", err)
	}
	if tasks.calls != 0 {
		t.Errorf("expected no task to be created, got %d calls", tasks.calls)
	}
}

// TestCreateTaskFromAnnotation_ClaimConflict_Rejected is the regression
// test for the duplicate-task bug: if another call (e.g. a client retry
// that's still in flight, or one that already succeeded but hasn't been
// observed yet) holds the ClaimTaskCreation claim, this call must bail out
// with ErrAnnotationTaskCreationInProgress instead of creating a second
// task.
func TestCreateTaskFromAnnotation_ClaimConflict_Rejected(t *testing.T) {
	projectID := uuid.New()
	annotationID := uuid.New()
	repo := &fakeAnnotationRepo{
		annotations: map[uuid.UUID]*annotationdom.PageAnnotation{
			annotationID: {ID: annotationID, ProjectID: projectID, Body: "fix this"},
		},
		claimTaskCreationOverride: func() (bool, error) { return false, nil },
	}
	tasks := &fakeTaskCreator{}
	svc := New(repo, nil, tasks, nil, nil, nil, "test-bucket")

	_, err := svc.CreateTaskFromAnnotation(context.Background(), projectID, annotationID, annotationdom.CreateTaskFromAnnotationInput{ReporterID: uuid.New()})
	if !errors.Is(err, annotationdom.ErrAnnotationTaskCreationInProgress) {
		t.Fatalf("expected ErrAnnotationTaskCreationInProgress, got %v", err)
	}
	if tasks.calls != 0 {
		t.Errorf("expected no task to be created while another attempt holds the claim, got %d calls", tasks.calls)
	}
}

func TestCreateTaskFromAnnotation_Succeeds(t *testing.T) {
	projectID := uuid.New()
	annotationID := uuid.New()
	repo := &fakeAnnotationRepo{annotations: map[uuid.UUID]*annotationdom.PageAnnotation{
		annotationID: {ID: annotationID, ProjectID: projectID, Body: "fix this"},
	}}
	tasks := &fakeTaskCreator{}
	svc := New(repo, nil, tasks, nil, nil, nil, "test-bucket")

	a, err := svc.CreateTaskFromAnnotation(context.Background(), projectID, annotationID, annotationdom.CreateTaskFromAnnotationInput{ReporterID: uuid.New()})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if tasks.calls != 1 {
		t.Errorf("expected exactly one task to be created, got %d calls", tasks.calls)
	}
	if a.TaskID == nil {
		t.Error("expected the returned annotation to have TaskID set")
	}
}

// TestCreateTaskFromAnnotation_TransientSetTaskIDFailure_RecoversWithoutDuplicateTask
// covers the failure window the claim was added to close: SetTaskID
// failing transiently after the task was already created must not result
// in a second task, and setTaskIDWithRetry's in-process retry should
// recover without the caller needing to retry the whole request.
func TestCreateTaskFromAnnotation_TransientSetTaskIDFailure_RecoversWithoutDuplicateTask(t *testing.T) {
	projectID := uuid.New()
	annotationID := uuid.New()
	repo := &fakeAnnotationRepo{
		annotations: map[uuid.UUID]*annotationdom.PageAnnotation{
			annotationID: {ID: annotationID, ProjectID: projectID, Body: "fix this"},
		},
		setTaskIDFailures: setTaskIDCreationLinkRetries - 1, // fails until the last allowed attempt
	}
	tasks := &fakeTaskCreator{}
	svc := New(repo, nil, tasks, nil, nil, nil, "test-bucket")

	a, err := svc.CreateTaskFromAnnotation(context.Background(), projectID, annotationID, annotationdom.CreateTaskFromAnnotationInput{ReporterID: uuid.New()})
	if err != nil {
		t.Fatalf("expected the retry to recover, got %v", err)
	}
	if tasks.calls != 1 {
		t.Errorf("expected exactly one task to be created despite the SetTaskID retries, got %d calls", tasks.calls)
	}
	if a.TaskID == nil {
		t.Error("expected TaskID to end up set once the retry succeeds")
	}
	if repo.setTaskIDCalls != setTaskIDCreationLinkRetries {
		t.Errorf("expected %d SetTaskID attempts, got %d", setTaskIDCreationLinkRetries, repo.setTaskIDCalls)
	}
}

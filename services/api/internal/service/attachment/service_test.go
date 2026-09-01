package attachmentsvc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	"github.com/Paca-AI/api/internal/platform/storage"
)

// fakeTaskChecker always reports that the task belongs to the project.
type fakeTaskChecker struct{}

func (fakeTaskChecker) TaskBelongsToProject(_ context.Context, _, _ uuid.UUID) error {
	return nil
}

var _ attachmentdom.TaskOwnerChecker = fakeTaskChecker{}

// fakeDocChecker always reports that the document belongs to the project.
type fakeDocChecker struct{}

func (fakeDocChecker) DocBelongsToProject(_ context.Context, _, _ uuid.UUID) error {
	return nil
}

var _ attachmentdom.DocOwnerChecker = fakeDocChecker{}

// fakeRepo is a minimal in-memory attachmentdom.Repository for exercising
// service methods without a real database.
type fakeRepo struct {
	files       map[uuid.UUID]*attachmentdom.File
	attachments map[uuid.UUID]*attachmentdom.TaskAttachment
}

func (r *fakeRepo) CreateFile(_ context.Context, f *attachmentdom.File) error {
	r.files[f.ID] = f
	return nil
}
func (r *fakeRepo) FindFileByID(_ context.Context, id uuid.UUID) (*attachmentdom.File, error) {
	f, ok := r.files[id]
	if !ok {
		return nil, errors.New("file not found")
	}
	return f, nil
}
func (r *fakeRepo) UpdateFileStatus(_ context.Context, _ uuid.UUID, _ attachmentdom.UploadStatus, _ *string) error {
	return nil
}
func (r *fakeRepo) DeleteFile(_ context.Context, _ uuid.UUID) error { return nil }

func (r *fakeRepo) ListTaskAttachments(_ context.Context, _ uuid.UUID) ([]*attachmentdom.TaskAttachment, error) {
	return nil, nil
}
func (r *fakeRepo) FindTaskAttachmentByID(_ context.Context, id uuid.UUID) (*attachmentdom.TaskAttachment, error) {
	a, ok := r.attachments[id]
	if !ok {
		return nil, attachmentdom.ErrAttachmentNotFound
	}
	return a, nil
}
func (r *fakeRepo) CreateTaskAttachment(_ context.Context, a *attachmentdom.TaskAttachment) error {
	r.attachments[a.ID] = a
	return nil
}
func (r *fakeRepo) DeleteTaskAttachment(_ context.Context, id uuid.UUID) error {
	delete(r.attachments, id)
	return nil
}

var _ attachmentdom.Repository = (*fakeRepo)(nil)

// fakeStore is a minimal storage.Client whose GetObject mimics the real
// S3Client: it truncates the returned bytes to maxBytes without ever
// returning an error, matching io.LimitReader + io.ReadAll semantics. Every
// other method is unused by these tests and just errors out.
type fakeStore struct {
	objectBytes []byte
}

func (s *fakeStore) PresignPutObject(context.Context, string, string, string, time.Duration) (string, error) {
	return "", errors.New("not implemented")
}
func (s *fakeStore) PresignGetObject(context.Context, string, string, time.Duration, string) (string, error) {
	return "", errors.New("not implemented")
}
func (s *fakeStore) InitiateMultipartUpload(context.Context, string, string, string, int64, int64, time.Duration) (*storage.MultipartUpload, error) {
	return nil, errors.New("not implemented")
}
func (s *fakeStore) CompleteMultipartUpload(context.Context, string, string, string, []storage.CompletedPart) error {
	return errors.New("not implemented")
}
func (s *fakeStore) AbortMultipartUpload(context.Context, string, string, string) error {
	return errors.New("not implemented")
}
func (s *fakeStore) DeleteObject(context.Context, string, string) error {
	return errors.New("not implemented")
}
func (s *fakeStore) EnsureBucket(context.Context, string) error { return nil }
func (s *fakeStore) GetObject(_ context.Context, _, _ string, maxBytes int64) ([]byte, error) {
	data := s.objectBytes
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		data = data[:maxBytes]
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}
func (s *fakeStore) PutObject(context.Context, string, string, string, []byte) error {
	return errors.New("not implemented")
}

var _ storage.Client = (*fakeStore)(nil)

// newTestService wires a Service around an attachment whose File record
// declares declaredFileSize while the object store actually holds
// actualObjectBytes — letting tests simulate a client that under-declared
// file_size at upload time (InitiateUpload/CompleteUpload never re-verify
// the real object size against it).
func newTestService(actualObjectBytes []byte, declaredFileSize int64) (svc *Service, taskID, attachmentID uuid.UUID) {
	fileID := uuid.New()
	attachmentID = uuid.New()
	taskID = uuid.New()

	repo := &fakeRepo{
		files: map[uuid.UUID]*attachmentdom.File{
			fileID: {
				ID:          fileID,
				StorageKey:  "tasks/x/y/file.txt",
				Bucket:      "test-bucket",
				FileName:    "file.txt",
				ContentType: "text/plain",
				FileSize:    declaredFileSize,
			},
		},
		attachments: map[uuid.UUID]*attachmentdom.TaskAttachment{
			attachmentID: {
				ID:     attachmentID,
				TaskID: taskID,
				FileID: fileID,
			},
		},
	}
	store := &fakeStore{objectBytes: actualObjectBytes}
	svc = New(repo, fakeTaskChecker{}, fakeDocChecker{}, store, "test-bucket")
	return svc, taskID, attachmentID
}

func TestGetAttachmentContent_ReturnsBytesWhenWithinLimit(t *testing.T) {
	want := []byte("hello world")
	svc, taskID, attachmentID := newTestService(want, int64(len(want)))

	data, contentType, err := svc.GetAttachmentContent(context.Background(), uuid.New(), taskID, attachmentID)
	if err != nil {
		t.Fatalf("GetAttachmentContent: %v", err)
	}
	if string(data) != string(want) {
		t.Fatalf("got %q, want %q", data, want)
	}
	if contentType != "text/plain" {
		t.Fatalf("got content type %q, want text/plain", contentType)
	}
}

// TestGetAttachmentContent_RejectsUndeclaredOversizedObject covers the
// silent-truncation gap: a client can declare a small file_size at
// upload-initiation time and then PUT more bytes than it claimed, since
// nothing enforces the presigned PUT actually matches. Before this fix,
// GetObject's io.LimitReader would silently truncate the read to
// MaxAttachmentContentSize and the endpoint would serve a 200 with
// corrupted, partial bytes instead of failing. See avatar_service.go for
// the same guard against the same underlying gap.
func TestGetAttachmentContent_RejectsUndeclaredOversizedObject(t *testing.T) {
	actual := make([]byte, attachmentdom.MaxAttachmentContentSize+1024)
	svc, taskID, attachmentID := newTestService(actual, 10) // declared size well under the cap

	data, _, err := svc.GetAttachmentContent(context.Background(), uuid.New(), taskID, attachmentID)
	if !errors.Is(err, attachmentdom.ErrAttachmentContentTooLarge) {
		t.Fatalf("got err %v, want ErrAttachmentContentTooLarge", err)
	}
	if data != nil {
		t.Fatalf("expected nil data on error, got %d bytes", len(data))
	}
}

func TestGetAttachmentContent_RejectsDeclaredOversizedFile(t *testing.T) {
	// The declared file_size alone already exceeds the cap; GetObject
	// should never even be called.
	svc, taskID, attachmentID := newTestService(nil, attachmentdom.MaxAttachmentContentSize+1)

	_, _, err := svc.GetAttachmentContent(context.Background(), uuid.New(), taskID, attachmentID)
	if !errors.Is(err, attachmentdom.ErrAttachmentContentTooLarge) {
		t.Fatalf("got err %v, want ErrAttachmentContentTooLarge", err)
	}
}

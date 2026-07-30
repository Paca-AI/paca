// Package attachmentsvc implements the attachment application service.
package attachmentsvc

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/storage"
)

const (
	// presignedUploadTTL is how long a presigned upload URL remains valid.
	presignedUploadTTL = 1 * time.Hour
	// presignedDownloadTTL is how long a presigned download URL remains valid.
	presignedDownloadTTL = 15 * time.Minute
)

// Service is the concrete implementation of attachmentdom.Service.
type Service struct {
	repo        attachmentdom.Repository
	taskChecker attachmentdom.TaskOwnerChecker
	store       storage.Client
	bucket      string
}

// New returns a configured attachment service.
func New(repo attachmentdom.Repository, taskChecker attachmentdom.TaskOwnerChecker, store storage.Client, bucket string) *Service {
	return &Service{repo: repo, taskChecker: taskChecker, store: store, bucket: bucket}
}

// InitiateUpload creates a pending File record and returns a presigned upload
// session.  For files >= MultipartThreshold a multipart upload is initiated
// with pre-signed URLs for each part; otherwise a single-part presigned PUT
// URL is returned.
func (s *Service) InitiateUpload(ctx context.Context, projectID uuid.UUID, in attachmentdom.InitiateUploadInput) (*attachmentdom.UploadSession, error) {
	if err := s.taskChecker.TaskBelongsToProject(ctx, projectID, in.TaskID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.FileName) == "" {
		return nil, attachmentdom.ErrFileNameEmpty
	}
	if strings.TrimSpace(in.ContentType) == "" {
		return nil, attachmentdom.ErrContentTypeEmpty
	}
	if in.FileSize <= 0 {
		return nil, attachmentdom.ErrFileSizeZero
	}

	fileID := uuid.New()
	// Build a storage key that is unique per file and organised by task.
	safeFileName := sanitizeFileName(in.FileName)
	storageKey := fmt.Sprintf("tasks/%s/%s/%s", in.TaskID.String(), fileID.String(), safeFileName)

	now := time.Now()
	f := &attachmentdom.File{
		ID:           fileID,
		StorageKey:   storageKey,
		Bucket:       s.bucket,
		FileName:     in.FileName,
		ContentType:  in.ContentType,
		FileSize:     in.FileSize,
		UploadStatus: attachmentdom.UploadStatusPending,
		UploadedBy:   &in.UploadedBy,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.repo.CreateFile(ctx, f); err != nil {
		return nil, fmt.Errorf("attachment svc: create file: %w", err)
	}

	session := &attachmentdom.UploadSession{FileID: fileID}

	if in.FileSize >= storage.MultipartThreshold {
		// Multipart upload path.
		mu, err := s.store.InitiateMultipartUpload(ctx, s.bucket, storageKey, in.ContentType, in.FileSize, storage.DefaultPartSize, presignedUploadTTL)
		if err != nil {
			return nil, fmt.Errorf("attachment svc: initiate multipart: %w", err)
		}
		// Persist the S3 upload ID so CompleteUpload can reference it.
		if err := s.repo.UpdateFileStatus(ctx, fileID, attachmentdom.UploadStatusPending, &mu.UploadID); err != nil {
			return nil, fmt.Errorf("attachment svc: save upload id: %w", err)
		}
		session.IsMultipart = true
		session.Multipart = mu
	} else {
		// Single-part upload path.
		uploadURL, err := s.store.PresignPutObject(ctx, s.bucket, storageKey, in.ContentType, presignedUploadTTL)
		if err != nil {
			return nil, fmt.Errorf("attachment svc: presign put: %w", err)
		}
		session.UploadURL = uploadURL
	}

	return session, nil
}

// CompleteUpload marks the file as uploaded and creates the TaskAttachment
// join record.  For multipart uploads the caller must supply the completed
// parts so the service can call CompleteMultipartUpload on the object store.
func (s *Service) CompleteUpload(ctx context.Context, projectID uuid.UUID, in attachmentdom.CompleteUploadInput) (*attachmentdom.TaskAttachment, error) {
	if err := s.taskChecker.TaskBelongsToProject(ctx, projectID, in.TaskID); err != nil {
		return nil, err
	}
	f, err := s.repo.FindFileByID(ctx, in.FileID)
	if err != nil {
		return nil, err
	}
	if f.UploadStatus != attachmentdom.UploadStatusPending {
		return nil, attachmentdom.ErrUploadNotPending
	}

	switch {
	case f.MultipartUploadID != nil && in.UploadID == nil:
		// File was initiated as multipart but caller did not provide an upload ID.
		return nil, attachmentdom.ErrMultipartUploadIDRequired
	case f.MultipartUploadID == nil && in.UploadID != nil:
		// Caller supplied an upload ID but the file is not a multipart upload.
		return nil, attachmentdom.ErrNotMultipartUpload
	case f.MultipartUploadID != nil && in.UploadID != nil && *in.UploadID != *f.MultipartUploadID:
		// Upload IDs present but refer to different sessions.
		return nil, attachmentdom.ErrUploadIDMismatch
	case f.MultipartUploadID != nil && in.UploadID != nil && len(in.Parts) == 0:
		// Multipart upload requires at least one completed part.
		return nil, attachmentdom.ErrMultipartPartsEmpty
	}

	if in.UploadID != nil {
		// Complete the multipart upload with the storage provider.
		parts := make([]storage.CompletedPart, 0, len(in.Parts))
		for _, p := range in.Parts {
			parts = append(parts, storage.CompletedPart{
				PartNumber: p.PartNumber,
				ETag:       p.ETag,
			})
		}
		if err := s.store.CompleteMultipartUpload(ctx, s.bucket, f.StorageKey, *in.UploadID, parts); err != nil {
			return nil, fmt.Errorf("attachment svc: complete multipart: %w", err)
		}
	}

	// Mark the file as uploaded and clear the multipart upload ID.
	if err := s.repo.UpdateFileStatus(ctx, in.FileID, attachmentdom.UploadStatusUploaded, nil); err != nil {
		return nil, fmt.Errorf("attachment svc: update file status: %w", err)
	}

	now := time.Now()
	a := &attachmentdom.TaskAttachment{
		ID:        uuid.New(),
		TaskID:    in.TaskID,
		FileID:    in.FileID,
		CreatedBy: &in.CreatedBy,
		CreatedAt: now,
	}
	if err := s.repo.CreateTaskAttachment(ctx, a); err != nil {
		return nil, fmt.Errorf("attachment svc: create task attachment: %w", err)
	}

	// Populate the file field for the response.
	f.UploadStatus = attachmentdom.UploadStatusUploaded
	a.File = f
	return a, nil
}

// GetDownloadURL returns a presigned GET URL for the given attachment's file.
// When forceDownload is true the URL includes a Content-Disposition: attachment
// header so the browser downloads the file rather than previewing it inline.
// Verifies the attachment belongs to taskID before generating the URL.
func (s *Service) GetDownloadURL(ctx context.Context, projectID, taskID, attachmentID uuid.UUID, ttl time.Duration, forceDownload bool) (string, error) {
	if err := s.taskChecker.TaskBelongsToProject(ctx, projectID, taskID); err != nil {
		return "", err
	}
	a, err := s.repo.FindTaskAttachmentByID(ctx, attachmentID)
	if err != nil {
		return "", err
	}
	if a.TaskID != taskID {
		return "", attachmentdom.ErrAttachmentNotFound
	}

	f, err := s.repo.FindFileByID(ctx, a.FileID)
	if err != nil {
		return "", err
	}

	contentDisposition := ""
	if forceDownload {
		// Strip ASCII control characters (including CR and LF) to prevent header
		// injection, then use mime.FormatMediaType which produces a correctly
		// RFC-quoted or RFC-5987-encoded filename parameter.
		safeName := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, f.FileName)
		if safeName == "" {
			safeName = "file"
		}
		cd := mime.FormatMediaType("attachment", map[string]string{"filename": safeName})
		if cd == "" {
			// Fallback: bare disposition without a filename parameter.
			cd = "attachment"
		}
		contentDisposition = cd
	}

	bucket := f.Bucket
	if bucket == "" {
		bucket = s.bucket
	}

	url, err := s.store.PresignGetObject(ctx, bucket, f.StorageKey, ttl, contentDisposition)
	if err != nil {
		return "", fmt.Errorf("attachment svc: presign get: %w", err)
	}
	return url, nil
}

// ListTaskAttachments returns all confirmed attachments for the given task.
func (s *Service) ListTaskAttachments(ctx context.Context, projectID, taskID uuid.UUID) ([]*attachmentdom.TaskAttachment, error) {
	if err := s.taskChecker.TaskBelongsToProject(ctx, projectID, taskID); err != nil {
		return nil, err
	}
	return s.repo.ListTaskAttachments(ctx, taskID)
}

// DeleteTaskAttachment removes the task→file association only.
// The underlying file record and object-store object are intentionally kept
// so the file can be referenced by other tasks or restored later.
// Verifies the attachment belongs to taskID before deleting.
func (s *Service) DeleteTaskAttachment(ctx context.Context, projectID, taskID, attachmentID uuid.UUID) error {
	if err := s.taskChecker.TaskBelongsToProject(ctx, projectID, taskID); err != nil {
		return err
	}
	a, err := s.repo.FindTaskAttachmentByID(ctx, attachmentID)
	if err != nil {
		return err
	}
	if a.TaskID != taskID {
		return attachmentdom.ErrAttachmentNotFound
	}
	return s.repo.DeleteTaskAttachment(ctx, attachmentID)
}

// sanitizeFileName strips directory components and replaces path-unsafe
// characters to produce a safe object-key segment.
func sanitizeFileName(name string) string {
	// Keep only the base name to prevent path traversal.
	name = path.Base(name)
	// Replace whitespace with underscores.
	name = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." {
		name = "file"
	}
	return name
}

// --- Task ownership checker --------------------------------------------------

type taskOwnerChecker struct {
	repo taskdom.TaskRepository
}

// NewTaskOwnerChecker returns a attachmentdom.TaskOwnerChecker that validates
// task→project ownership via the task repository.
func NewTaskOwnerChecker(repo taskdom.TaskRepository) attachmentdom.TaskOwnerChecker {
	return &taskOwnerChecker{repo: repo}
}

func (c *taskOwnerChecker) TaskBelongsToProject(ctx context.Context, projectID, taskID uuid.UUID) error {
	t, err := c.repo.FindTaskByID(ctx, taskID)
	if err != nil {
		if errors.Is(err, taskdom.ErrTaskNotFound) {
			return attachmentdom.ErrTaskNotInProject
		}
		return err
	}
	if t.ProjectID != projectID {
		return attachmentdom.ErrTaskNotInProject
	}
	return nil
}

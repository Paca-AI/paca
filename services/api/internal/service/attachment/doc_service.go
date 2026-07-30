package attachmentsvc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	"github.com/Paca-AI/api/internal/platform/storage"
)

// InitiateDocUpload creates a pending File record for a document and returns a
// presigned upload session.  The storage key is organised under docs/{docId}/
// to keep document files separate from task files.
func (s *Service) InitiateDocUpload(ctx context.Context, in attachmentdom.DocUploadInput) (*attachmentdom.UploadSession, error) {
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
	safeFileName := sanitizeFileName(in.FileName)
	storageKey := fmt.Sprintf("docs/%s/%s/%s", in.DocID.String(), fileID.String(), safeFileName)

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
		return nil, fmt.Errorf("attachment svc: create doc file: %w", err)
	}

	session := &attachmentdom.UploadSession{FileID: fileID}

	if in.FileSize >= storage.MultipartThreshold {
		mu, err := s.store.InitiateMultipartUpload(ctx, s.bucket, storageKey, in.ContentType, in.FileSize, storage.DefaultPartSize, presignedUploadTTL)
		if err != nil {
			return nil, fmt.Errorf("attachment svc: initiate multipart for doc: %w", err)
		}
		if err := s.repo.UpdateFileStatus(ctx, fileID, attachmentdom.UploadStatusPending, &mu.UploadID); err != nil {
			return nil, fmt.Errorf("attachment svc: save upload id for doc: %w", err)
		}
		session.IsMultipart = true
		session.Multipart = mu
	} else {
		uploadURL, err := s.store.PresignPutObject(ctx, s.bucket, storageKey, in.ContentType, presignedUploadTTL)
		if err != nil {
			return nil, fmt.Errorf("attachment svc: presign put for doc: %w", err)
		}
		session.UploadURL = uploadURL
	}

	return session, nil
}

// CompleteDocUpload marks the file as uploaded and returns the updated file record.
func (s *Service) CompleteDocUpload(ctx context.Context, in attachmentdom.DocCompleteUploadInput) (*attachmentdom.File, error) {
	f, err := s.repo.FindFileByID(ctx, in.FileID)
	if err != nil {
		return nil, err
	}
	if f.UploadStatus != attachmentdom.UploadStatusPending {
		return nil, attachmentdom.ErrUploadNotPending
	}

	switch {
	case f.MultipartUploadID != nil && in.UploadID == nil:
		return nil, attachmentdom.ErrMultipartUploadIDRequired
	case f.MultipartUploadID == nil && in.UploadID != nil:
		return nil, attachmentdom.ErrNotMultipartUpload
	case f.MultipartUploadID != nil && in.UploadID != nil && *in.UploadID != *f.MultipartUploadID:
		return nil, attachmentdom.ErrUploadIDMismatch
	case f.MultipartUploadID != nil && in.UploadID != nil && len(in.Parts) == 0:
		return nil, attachmentdom.ErrMultipartPartsEmpty
	}

	if in.UploadID != nil {
		parts := make([]storage.CompletedPart, 0, len(in.Parts))
		for _, p := range in.Parts {
			parts = append(parts, storage.CompletedPart{
				PartNumber: p.PartNumber,
				ETag:       p.ETag,
			})
		}
		if err := s.store.CompleteMultipartUpload(ctx, s.bucket, f.StorageKey, *in.UploadID, parts); err != nil {
			return nil, fmt.Errorf("attachment svc: complete multipart for doc: %w", err)
		}
	}

	if err := s.repo.UpdateFileStatus(ctx, in.FileID, attachmentdom.UploadStatusUploaded, nil); err != nil {
		return nil, fmt.Errorf("attachment svc: update doc file status: %w", err)
	}

	f.UploadStatus = attachmentdom.UploadStatusUploaded
	return f, nil
}

// GetDocFileDownloadURL returns a presigned GET URL for the given doc file.
// docID is used to verify the file belongs to the document by checking the
// storage key prefix (docs/{docId}/).
func (s *Service) GetDocFileDownloadURL(ctx context.Context, docID uuid.UUID, fileID uuid.UUID, ttl time.Duration) (string, error) {
	f, err := s.repo.FindFileByID(ctx, fileID)
	if err != nil {
		return "", err
	}

	if !docFileKeyHasDocPrefix(f.StorageKey, docID) {
		return "", attachmentdom.ErrDocFileMismatch
	}

	bucket := f.Bucket
	if bucket == "" {
		bucket = s.bucket
	}

	url, err := s.store.PresignGetObject(ctx, bucket, f.StorageKey, ttl, "")
	if err != nil {
		return "", fmt.Errorf("attachment svc: presign get for doc file: %w", err)
	}
	return url, nil
}

// DeleteDocFile removes the file record from the database and deletes the
// object from the object store. docID is used to verify the file belongs to
// the document by checking the storage key prefix (docs/{docId}/).
func (s *Service) DeleteDocFile(ctx context.Context, docID uuid.UUID, fileID uuid.UUID) error {
	f, err := s.repo.FindFileByID(ctx, fileID)
	if err != nil {
		return err
	}

	if !docFileKeyHasDocPrefix(f.StorageKey, docID) {
		return attachmentdom.ErrDocFileMismatch
	}

	bucket := f.Bucket
	if bucket == "" {
		bucket = s.bucket
	}

	if err := s.store.DeleteObject(ctx, bucket, f.StorageKey); err != nil {
		return fmt.Errorf("attachment svc: delete doc file object: %w", err)
	}

	return s.repo.DeleteFile(ctx, fileID)
}

// docFileKeyHasDocPrefix returns true when storageKey starts with
// the expected "docs/{docID}/" prefix, confirming the file belongs to the document.
func docFileKeyHasDocPrefix(storageKey string, docID uuid.UUID) bool {
	return strings.HasPrefix(storageKey, fmt.Sprintf("docs/%s/", docID.String()))
}

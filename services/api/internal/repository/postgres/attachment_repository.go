package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
)

// --- sqlx models -----------------------------------------------------------

type fileRecord struct {
	ID                string    `db:"id"`
	StorageKey        string    `db:"storage_key"`
	Bucket            string    `db:"bucket"`
	FileName          string    `db:"file_name"`
	ContentType       string    `db:"content_type"`
	FileSize          int64     `db:"file_size"`
	UploadStatus      string    `db:"upload_status"`
	MultipartUploadID *string   `db:"multipart_upload_id"`
	UploadedBy        *string   `db:"uploaded_by"`
	CreatedAt         time.Time `db:"created_at"`
	UpdatedAt         time.Time `db:"updated_at"`
}

// taskAttachmentWithFileRow is the flat join result for task attachment + file.
type taskAttachmentWithFileRow struct {
	// task_attachments columns
	ID        string    `db:"id"`
	TaskID    string    `db:"task_id"`
	FileID    string    `db:"file_id"`
	CreatedBy *string   `db:"created_by"`
	CreatedAt time.Time `db:"created_at"`
	// files columns (prefixed)
	FileIDJoin          string    `db:"file_id_join"`
	FileStorageKey      string    `db:"file_storage_key"`
	FileBucket          string    `db:"file_bucket"`
	FileFileName        string    `db:"file_file_name"`
	FileContentType     string    `db:"file_content_type"`
	FileFileSize        int64     `db:"file_file_size"`
	FileUploadStatus    string    `db:"file_upload_status"`
	FileMultipartUpload *string   `db:"file_multipart_upload_id"`
	FileUploadedBy      *string   `db:"file_uploaded_by"`
	FileCreatedAt       time.Time `db:"file_created_at"`
	FileUpdatedAt       time.Time `db:"file_updated_at"`
}

// --- Repository ------------------------------------------------------------

// AttachmentRepository is the sqlx implementation of attachmentdom.Repository.
type AttachmentRepository struct {
	db *sqlx.DB
}

// NewAttachmentRepository returns a new AttachmentRepository.
func NewAttachmentRepository(db *sqlx.DB) *AttachmentRepository {
	return &AttachmentRepository{db: db}
}

// --- File ------------------------------------------------------------------

const fileSelectCols = `id, storage_key, bucket, file_name, content_type, file_size, upload_status, multipart_upload_id, uploaded_by, created_at, updated_at`

// CreateFile inserts a new file metadata record.
func (r *AttachmentRepository) CreateFile(ctx context.Context, f *attachmentdom.File) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO files (id, storage_key, bucket, file_name, content_type, file_size, upload_status, multipart_upload_id, uploaded_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		f.ID.String(), f.StorageKey, f.Bucket, f.FileName, f.ContentType,
		f.FileSize, string(f.UploadStatus), f.MultipartUploadID,
		uuidPtrToStringPtr(f.UploadedBy), f.CreatedAt, f.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("attachment repo: create file: %w", err)
	}
	return nil
}

// FindFileByID returns the file with the given ID.
func (r *AttachmentRepository) FindFileByID(ctx context.Context, id uuid.UUID) (*attachmentdom.File, error) {
	var rec fileRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+fileSelectCols+` FROM files WHERE id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, attachmentdom.ErrFileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("attachment repo: find file: %w", err)
	}
	return fileRecordToEntity(&rec), nil
}

// UpdateFileStatus updates the upload_status and multipart_upload_id of a file.
func (r *AttachmentRepository) UpdateFileStatus(ctx context.Context, id uuid.UUID, status attachmentdom.UploadStatus, multipartUploadID *string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE files SET upload_status = $1, multipart_upload_id = $2, updated_at = $3 WHERE id = $4`,
		string(status), multipartUploadID, time.Now(), id.String(),
	)
	if err != nil {
		return fmt.Errorf("attachment repo: update file status: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return attachmentdom.ErrFileNotFound
	}
	return nil
}

// DeleteFile removes a file record permanently.
func (r *AttachmentRepository) DeleteFile(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM files WHERE id = $1`, id.String())
	if err != nil {
		return fmt.Errorf("attachment repo: delete file: %w", err)
	}
	return nil
}

// --- Task attachments ------------------------------------------------------

const taskAttachmentJoinSQL = `
	SELECT
		ta.id, ta.task_id, ta.file_id, ta.created_by, ta.created_at,
		f.id AS file_id_join, f.storage_key AS file_storage_key, f.bucket AS file_bucket,
		f.file_name AS file_file_name, f.content_type AS file_content_type,
		f.file_size AS file_file_size, f.upload_status AS file_upload_status,
		f.multipart_upload_id AS file_multipart_upload_id, f.uploaded_by AS file_uploaded_by,
		f.created_at AS file_created_at, f.updated_at AS file_updated_at
	FROM task_attachments ta
	JOIN files f ON f.id = ta.file_id`

// ListTaskAttachments returns all task attachments for the given task,
// with the associated file metadata eagerly loaded.
func (r *AttachmentRepository) ListTaskAttachments(ctx context.Context, taskID uuid.UUID) ([]*attachmentdom.TaskAttachment, error) {
	var rows []taskAttachmentWithFileRow
	if err := r.db.SelectContext(ctx, &rows, taskAttachmentJoinSQL+` WHERE ta.task_id = $1 ORDER BY ta.created_at ASC`, taskID.String()); err != nil {
		return nil, fmt.Errorf("attachment repo: list task attachments: %w", err)
	}
	result := make([]*attachmentdom.TaskAttachment, 0, len(rows))
	for _, row := range rows {
		result = append(result, taskAttachmentWithFileRowToEntity(&row))
	}
	return result, nil
}

// FindTaskAttachmentByID returns a single task attachment by ID.
func (r *AttachmentRepository) FindTaskAttachmentByID(ctx context.Context, id uuid.UUID) (*attachmentdom.TaskAttachment, error) {
	var row taskAttachmentWithFileRow
	err := r.db.GetContext(ctx, &row, taskAttachmentJoinSQL+` WHERE ta.id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, attachmentdom.ErrAttachmentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("attachment repo: find task attachment: %w", err)
	}
	return taskAttachmentWithFileRowToEntity(&row), nil
}

// CreateTaskAttachment inserts a new task-attachment join record.
func (r *AttachmentRepository) CreateTaskAttachment(ctx context.Context, a *attachmentdom.TaskAttachment) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO task_attachments (id, task_id, file_id, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		a.ID.String(), a.TaskID.String(), a.FileID.String(),
		uuidPtrToStringPtr(a.CreatedBy), a.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("attachment repo: create task attachment: %w", err)
	}
	return nil
}

// DeleteTaskAttachment removes a task attachment by ID.
func (r *AttachmentRepository) DeleteTaskAttachment(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM task_attachments WHERE id = $1`, id.String())
	if err != nil {
		return fmt.Errorf("attachment repo: delete task attachment: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return attachmentdom.ErrAttachmentNotFound
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

func fileRecordToEntity(r *fileRecord) *attachmentdom.File {
	return &attachmentdom.File{
		ID:                mustParseUUID(r.ID),
		StorageKey:        r.StorageKey,
		Bucket:            r.Bucket,
		FileName:          r.FileName,
		ContentType:       r.ContentType,
		FileSize:          r.FileSize,
		UploadStatus:      attachmentdom.UploadStatus(r.UploadStatus),
		MultipartUploadID: r.MultipartUploadID,
		UploadedBy:        stringPtrToUUIDPtr(r.UploadedBy),
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
	}
}

func taskAttachmentWithFileRowToEntity(row *taskAttachmentWithFileRow) *attachmentdom.TaskAttachment {
	a := &attachmentdom.TaskAttachment{
		ID:        mustParseUUID(row.ID),
		TaskID:    mustParseUUID(row.TaskID),
		FileID:    mustParseUUID(row.FileID),
		CreatedBy: stringPtrToUUIDPtr(row.CreatedBy),
		CreatedAt: row.CreatedAt,
	}
	f := &attachmentdom.File{
		ID:                mustParseUUID(row.FileIDJoin),
		StorageKey:        row.FileStorageKey,
		Bucket:            row.FileBucket,
		FileName:          row.FileFileName,
		ContentType:       row.FileContentType,
		FileSize:          row.FileFileSize,
		UploadStatus:      attachmentdom.UploadStatus(row.FileUploadStatus),
		MultipartUploadID: row.FileMultipartUpload,
		UploadedBy:        stringPtrToUUIDPtr(row.FileUploadedBy),
		CreatedAt:         row.FileCreatedAt,
		UpdatedAt:         row.FileUpdatedAt,
	}
	a.File = f
	return a
}

func uuidPtrToStringPtr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func stringPtrToUUIDPtr(s *string) *uuid.UUID {
	if s == nil {
		return nil
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return nil
	}
	return &id
}

func mustParseUUID(s string) uuid.UUID {
	id, _ := uuid.Parse(s)
	return id
}

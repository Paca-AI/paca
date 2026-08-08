// Package storage provides an object-storage abstraction that is compatible
// with both AWS S3 and MinIO (which implements the S3 API).
package storage

import (
	"context"
	"time"
)

// PresignedPart carries the pre-signed URL and sequential part number for one
// chunk of a multipart upload.
type PresignedPart struct {
	PartNumber int    `json:"part_number"`
	UploadURL  string `json:"upload_url"`
}

// MultipartUpload is returned when a file is large enough to require chunked
// upload (>= MultipartThreshold bytes).
type MultipartUpload struct {
	UploadID string          `json:"upload_id"`
	Parts    []PresignedPart `json:"parts"`
}

// CompletedPart is sent by the client after each part has been uploaded.
type CompletedPart struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
}

// Client is the object-storage operations contract.
type Client interface {
	// PresignPutObject returns a pre-signed URL that the client can use to
	// upload a single object (for files smaller than MultipartThreshold).
	PresignPutObject(ctx context.Context, bucket, key, contentType string, ttl time.Duration) (string, error)

	// PresignGetObject returns a pre-signed URL that allows the bearer to
	// download the named object for the given TTL.
	// Pass a non-empty contentDisposition (e.g. `attachment; filename="file.pdf"`) to
	// override the browser's default behaviour and force a download.
	PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, contentDisposition string) (string, error)

	// InitiateMultipartUpload begins a multipart upload and returns the S3
	// UploadID along with pre-signed PUT URLs for each part.
	// partSize is the size of each part in bytes (minimum 5 MiB).
	// totalSize is used to calculate the number of parts.
	InitiateMultipartUpload(ctx context.Context, bucket, key, contentType string, totalSize int64, partSize int64, ttl time.Duration) (*MultipartUpload, error)

	// CompleteMultipartUpload signals S3/MinIO that all parts have been
	// received and assembles the final object.
	CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) error

	// AbortMultipartUpload cancels an in-progress multipart session and
	// releases any partial parts already stored.
	AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error

	// DeleteObject removes a single object from the bucket.
	DeleteObject(ctx context.Context, bucket, key string) error

	// EnsureBucket creates the bucket if it does not already exist.
	EnsureBucket(ctx context.Context, bucket string) error

	// GetObject downloads an object's full contents. Intended for small
	// objects the server needs to process itself (e.g. re-encoding an
	// uploaded avatar) — not for client-facing downloads, which should use
	// PresignGetObject instead.
	GetObject(ctx context.Context, bucket, key string) ([]byte, error)

	// PutObject uploads data directly from the server (as opposed to a
	// client uploading via a presigned URL from PresignPutObject).
	// Intended for small, server-generated objects (e.g. a resized avatar).
	PutObject(ctx context.Context, bucket, key, contentType string, data []byte) error
}

// MultipartThreshold is the minimum file size (in bytes) at which the service
// switches from a single-part pre-signed PUT to a multipart upload.
const MultipartThreshold = 5 * 1024 * 1024 // 5 MiB

// DefaultPartSize is the chunk size used for each part of a multipart upload.
const DefaultPartSize = 5 * 1024 * 1024 // 5 MiB

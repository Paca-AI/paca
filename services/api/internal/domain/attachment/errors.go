package attachmentdom

import "errors"

// Sentinel errors returned by attachment domain operations.
var (
	ErrFileNotFound       = errors.New("file not found")
	ErrAttachmentNotFound = errors.New("attachment not found")
	ErrUploadNotPending   = errors.New("file upload is not in pending state")
	ErrFileSizeZero       = errors.New("file size must be greater than zero")
	ErrFileNameEmpty      = errors.New("file name must not be empty")
	ErrContentTypeEmpty   = errors.New("content type must not be empty")

	// ErrMultipartUploadIDRequired is returned when a file was initiated as a
	// multipart upload but the caller did not supply an upload_id on complete.
	ErrMultipartUploadIDRequired = errors.New("multipart upload requires an upload_id")
	// ErrNotMultipartUpload is returned when the caller supplies an upload_id
	// but the file was not initiated as a multipart upload.
	ErrNotMultipartUpload = errors.New("file was not initiated as a multipart upload")
	// ErrUploadIDMismatch is returned when the provided upload_id does not
	// match the upload session stored on the file record.
	ErrUploadIDMismatch = errors.New("upload_id does not match the recorded multipart upload session")
	// ErrDocFileMismatch is returned when a file does not belong to the
	// specified document (storage key prefix mismatch).
	ErrDocFileMismatch = errors.New("file does not belong to the specified document")
	// ErrMultipartPartsEmpty is returned when a multipart complete request
	// contains no parts.
	ErrMultipartPartsEmpty = errors.New("multipart upload requires at least one part")
	// ErrTaskNotInProject is returned when the referenced task does not belong
	// to the project specified in the request URL.
	ErrTaskNotInProject = errors.New("task does not belong to the specified project")
	// ErrDocNotInProject is returned when the referenced document does not
	// belong to the project specified in the request URL.
	ErrDocNotInProject = errors.New("document does not belong to the specified project")

	// ErrAttachmentContentTooLarge is returned when GetAttachmentContent is
	// asked to read a file larger than MaxAttachmentContentSize into memory.
	ErrAttachmentContentTooLarge = errors.New("attachment exceeds the maximum size that can be read inline")

	// ErrAvatarTooLarge is returned when an avatar upload exceeds MaxAvatarUploadSize.
	ErrAvatarTooLarge = errors.New("avatar file exceeds the maximum allowed size")
	// ErrAvatarContentTypeInvalid is returned when an avatar upload's content
	// type is not in AvatarContentTypes.
	ErrAvatarContentTypeInvalid = errors.New("avatar content type must be image/png, image/jpeg, image/webp, or image/gif")
	// ErrAvatarDecodeFailed is returned when the uploaded bytes cannot be
	// decoded as an image of one of the accepted content types.
	ErrAvatarDecodeFailed = errors.New("uploaded file is not a valid image")
	// ErrAvatarDimensionsTooLarge is returned when an uploaded image's
	// declared pixel dimensions exceed MaxAvatarDecodeDimension, checked
	// before the full image is decoded into memory.
	ErrAvatarDimensionsTooLarge = errors.New("image dimensions exceed the maximum allowed for an avatar")
	// ErrAvatarOwnerMismatch is returned when a file being completed does not
	// belong to the claimed avatar owner (storage key prefix mismatch).
	ErrAvatarOwnerMismatch = errors.New("file does not belong to the specified avatar owner")
)

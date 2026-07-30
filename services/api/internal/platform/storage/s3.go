package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Config holds the configuration for an S3-compatible object store.
// Both AWS S3 and MinIO are supported.
type S3Config struct {
	Endpoint        string // leave empty for AWS S3; set to MinIO URL for self-hosted
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool // only relevant when Endpoint is set (MinIO)
	ForcePathStyle  bool // set true for MinIO
	// PublicURL is the public-facing base URL that clients (browsers) use to
	// reach the object store through the gateway proxy, e.g.
	// "http://localhost/storage".  When set, all presigned URLs are rewritten
	// to replace the internal endpoint prefix with this value so that clients
	// never receive internal Docker hostnames.
	// Leave empty to return presigned URLs as-is (e.g. direct AWS S3).
	PublicURL string
}

// S3Client is an S3-compatible implementation of Client.
type S3Client struct {
	s3      *s3.Client
	presign *s3.PresignClient
	cfg     S3Config
}

// NewS3Client constructs an S3Client.  When cfg.Endpoint is empty the client
// connects to AWS S3; otherwise it targets the given MinIO (or other
// S3-compatible) endpoint.
func NewS3Client(ctx context.Context, cfg S3Config) (*S3Client, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		),
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}

	var s3Opts []func(*s3.Options)

	if cfg.Endpoint != "" {
		scheme := "http"
		if cfg.UseSSL {
			scheme = "https"
		}
		endpoint := cfg.Endpoint
		// Prefix scheme if not already present.
		if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
			endpoint = scheme + "://" + endpoint
		}

		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = cfg.ForcePathStyle
		})
	}

	s3Client := s3.NewFromConfig(awsCfg, s3Opts...)
	presignClient := s3.NewPresignClient(s3Client)

	return &S3Client{
		s3:      s3Client,
		presign: presignClient,
		cfg:     cfg,
	}, nil
}

// internalEndpoint returns the full scheme+host of the internal endpoint as
// it appears in presigned URLs (e.g. "http://minio:9000").
func (c *S3Client) internalEndpoint() string {
	if c.cfg.Endpoint == "" {
		return ""
	}
	scheme := "http"
	if c.cfg.UseSSL {
		scheme = "https"
	}
	ep := c.cfg.Endpoint
	if strings.HasPrefix(ep, "http://") || strings.HasPrefix(ep, "https://") {
		return ep
	}
	return scheme + "://" + ep
}

// rewriteURL replaces the internal endpoint prefix in a presigned URL with
// the configured PublicURL so browsers can reach the object store through the
// gateway proxy.  Returns the URL unchanged when PublicURL is empty.
func (c *S3Client) rewriteURL(u string) string {
	if c.cfg.PublicURL == "" {
		return u
	}
	internal := c.internalEndpoint()
	if internal == "" {
		return u
	}
	public := strings.TrimRight(c.cfg.PublicURL, "/")
	return strings.Replace(u, internal, public, 1)
}

// PresignPutObject generates a pre-signed PUT URL for a single-part upload.
func (c *S3Client) PresignPutObject(ctx context.Context, bucket, key, contentType string, ttl time.Duration) (string, error) {
	req, err := c.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("storage: presign put %q: %w", key, err)
	}
	return c.rewriteURL(req.URL), nil
}

// PresignGetObject generates a pre-signed GET URL for downloading an object.
// Pass a non-empty contentDisposition to embed a Content-Disposition override
// in the presigned URL (e.g. force a browser download).
func (c *S3Client) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, contentDisposition string) (string, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if contentDisposition != "" {
		input.ResponseContentDisposition = aws.String(contentDisposition)
	}
	req, err := c.presign.PresignGetObject(ctx, input, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("storage: presign get %q: %w", key, err)
	}
	return c.rewriteURL(req.URL), nil
}

// InitiateMultipartUpload begins an S3 multipart upload and returns the
// UploadID plus pre-signed URLs for every part.
func (c *S3Client) InitiateMultipartUpload(ctx context.Context, bucket, key, contentType string, totalSize, partSize int64, ttl time.Duration) (*MultipartUpload, error) {
	createResp, err := c.s3.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: create multipart upload: %w", err)
	}

	uploadID := aws.ToString(createResp.UploadId)

	numParts := int((totalSize + partSize - 1) / partSize)
	parts := make([]PresignedPart, 0, numParts)

	for i := 1; i <= numParts; i++ {
		req, err := c.presign.PresignUploadPart(ctx, &s3.UploadPartInput{
			Bucket:     aws.String(bucket),
			Key:        aws.String(key),
			UploadId:   aws.String(uploadID),
			PartNumber: aws.Int32(int32(i)),
		}, s3.WithPresignExpires(ttl))
		if err != nil {
			// Attempt to clean up on partial failure.
			_ = c.AbortMultipartUpload(ctx, bucket, key, uploadID)
			return nil, fmt.Errorf("storage: presign part %d: %w", i, err)
		}
		parts = append(parts, PresignedPart{PartNumber: i, UploadURL: c.rewriteURL(req.URL)})
	}

	return &MultipartUpload{UploadID: uploadID, Parts: parts}, nil
}

// CompleteMultipartUpload assembles all uploaded parts into a single S3 object.
func (c *S3Client) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) error {
	s3Parts := make([]s3types.CompletedPart, 0, len(parts))
	for _, p := range parts {
		s3Parts = append(s3Parts, s3types.CompletedPart{
			PartNumber: aws.Int32(int32(p.PartNumber)),
			ETag:       aws.String(p.ETag),
		})
	}

	_, err := c.s3.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{
			Parts: s3Parts,
		},
	})
	if err != nil {
		return fmt.Errorf("storage: complete multipart upload: %w", err)
	}
	return nil
}

// AbortMultipartUpload cancels an in-progress multipart upload session.
func (c *S3Client) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	_, err := c.s3.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	if err != nil {
		return fmt.Errorf("storage: abort multipart upload: %w", err)
	}
	return nil
}

// DeleteObject removes a single object from the bucket.
func (c *S3Client) DeleteObject(ctx context.Context, bucket, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("storage: delete object %q: %w", key, err)
	}
	return nil
}

// EnsureBucket creates the bucket if it does not already exist.
func (c *S3Client) EnsureBucket(ctx context.Context, bucket string) error {
	_, err := c.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)})
	if err == nil {
		return nil // already exists
	}

	input := &s3.CreateBucketInput{Bucket: aws.String(bucket)}

	if _, err := c.s3.CreateBucket(ctx, input); err != nil {
		// Another caller may have won a concurrent create race between our
		// HeadBucket check and this call; both outcomes mean the bucket is
		// now present, so they aren't real errors.
		var alreadyOwned *s3types.BucketAlreadyOwnedByYou
		var alreadyExists *s3types.BucketAlreadyExists
		if errors.As(err, &alreadyOwned) || errors.As(err, &alreadyExists) {
			return nil
		}
		return fmt.Errorf("storage: create bucket %q: %w", bucket, err)
	}
	return nil
}

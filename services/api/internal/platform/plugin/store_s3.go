package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

// S3API is the subset of *s3.Client the s3Store uses. It exists so tests can
// substitute an in-memory fake.
type S3API interface {
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

// s3Store keeps plugins in a bucket, under {S3Prefix}/{name}/:
//
//	backend.wasm | plugin.json | migrations/*.sql
//	frontend/... | mcp/... | skills/{skill}/SKILL.md
type s3Store struct {
	cfg StoreConfig
	s3  S3API
}

// newS3Store uses cfg.S3Client when set, otherwise builds a client from the
// default AWS credential chain.
func newS3Store(ctx context.Context, cfg StoreConfig) (*s3Store, error) {
	if strings.TrimSpace(cfg.S3Bucket) == "" {
		return nil, errors.New("plugin store: PLUGINS_STORE=s3 requires a bucket (STORAGE_BUCKET)")
	}
	client := cfg.S3Client
	if client == nil {
		awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.S3Region))
		if err != nil {
			return nil, fmt.Errorf("plugin store: load AWS config: %w", err)
		}
		client = s3.NewFromConfig(awsCfg)
	}
	return &s3Store{cfg: cfg, s3: client}, nil
}

func (s *s3Store) Mode() string { return "s3" }

// Everything lives in one bucket, so there is nothing further to configure.
func (s *s3Store) CheckInstallable() error { return nil }

func (s *s3Store) RequireKind(kind ArtifactKind) error { return validateKind(kind) }

func (s *s3Store) LoadWASM(ctx context.Context, name string) ([]byte, error) {
	return s.get(ctx, s.key(name, "backend.wasm"))
}

func (s *s3Store) LoadPluginJSON(ctx context.Context, name string) ([]byte, error) {
	return s.get(ctx, s.key(name, "plugin.json"))
}

func (s *s3Store) ListMigrations(ctx context.Context, name string) ([]MigrationFile, error) {
	keys, err := s.list(ctx, s.key(name, "migrations")+"/")
	if err != nil {
		return nil, fmt.Errorf("plugin store: list s3 migrations for %q: %w", name, err)
	}
	var files []MigrationFile
	for _, key := range keys { // ListObjectsV2 returns keys in lexicographic order
		if filepath.Ext(key) != ".sql" {
			continue
		}
		data, err := s.get(ctx, key)
		if err != nil {
			return nil, err
		}
		files = append(files, MigrationFile{Name: filepath.Base(key), SQL: string(data)})
	}
	return files, nil
}

func (s *s3Store) ReadSkill(ctx context.Context, pluginName, skill string) ([]byte, error) {
	if err := plugindom.ValidatePluginName(pluginName); err != nil {
		return nil, err
	}
	if err := validateSkillSegment(skill); err != nil {
		return nil, err
	}
	return s.get(ctx, s.key(pluginName, string(KindSkills), skill, "SKILL.md"))
}

func (s *s3Store) OpenAsset(ctx context.Context, kind ArtifactKind, name, relPath string) (*Asset, error) {
	if err := plugindom.ValidatePluginName(name); err != nil {
		return nil, ErrAssetNotFound
	}
	if err := validateKind(kind); err != nil {
		return nil, ErrAssetNotFound
	}
	rel, ok := cleanAssetPath(relPath)
	if !ok {
		return nil, ErrAssetNotFound
	}
	key := s.key(name, string(kind), rel)
	out, err := s.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.S3Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var noKey *s3types.NoSuchKey
		var apiErr smithy.APIError
		if errors.As(err, &noKey) || (errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound")) {
			return nil, ErrAssetNotFound
		}
		return nil, fmt.Errorf("plugin store: s3 get %q: %w", key, err)
	}
	// Prefer the extension-based type so assets are served identically in both
	// modes, whatever the bucket recorded at upload time.
	contentType := assetContentType(rel)
	size := int64(-1)
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	return &Asset{
		Body:        out.Body,
		Size:        size,
		ContentType: contentType,
		ETag:        aws.ToString(out.ETag),
		ModTime:     aws.ToTime(out.LastModified),
	}, nil
}

// PutBackend writes plugin.json last, so a half-finished upload never leaves a
// manifest that points at a missing binary.
func (s *s3Store) PutBackend(ctx context.Context, name string, b BackendBundle) error {
	if err := plugindom.ValidatePluginName(name); err != nil {
		return err
	}
	if b.HasMigrations {
		if err := s.deletePrefix(ctx, s.key(name, "migrations")+"/"); err != nil {
			return fmt.Errorf("clear migrations: %w", err)
		}
		for _, path := range b.Migrations {
			if err := s.putFile(ctx, s.key(name, "migrations", filepath.Base(path)), path); err != nil {
				return fmt.Errorf("upload migration %q: %w", filepath.Base(path), err)
			}
		}
	}
	if b.WASMPath != "" {
		if err := s.putFile(ctx, s.key(name, "backend.wasm"), b.WASMPath); err != nil {
			return fmt.Errorf("upload backend.wasm: %w", err)
		}
		if err := s.putFile(ctx, s.key(name, "plugin.json"), b.ManifestPath); err != nil {
			return fmt.Errorf("upload plugin.json: %w", err)
		}
	}
	return nil
}

func (s *s3Store) PutTree(ctx context.Context, kind ArtifactKind, name, srcDir string) error {
	if err := plugindom.ValidatePluginName(name); err != nil {
		return err
	}
	if err := validateKind(kind); err != nil {
		return err
	}
	prefix := s.key(name, string(kind)) + "/"
	if err := s.deletePrefix(ctx, prefix); err != nil {
		return fmt.Errorf("clear %s bundle: %w", kind, err)
	}
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if err := s.putFile(ctx, prefix+filepath.ToSlash(rel), path); err != nil {
			return fmt.Errorf("upload %s/%s: %w", kind, rel, err)
		}
		return nil
	})
}

func (s *s3Store) DeleteAll(ctx context.Context, name string) error {
	if err := plugindom.ValidatePluginName(name); err != nil {
		return err
	}
	if err := s.deletePrefix(ctx, s.key(name)+"/"); err != nil {
		return fmt.Errorf("remove plugin objects: %w", err)
	}
	return nil
}

// key joins the configured prefix and parts into an object key.
func (s *s3Store) key(parts ...string) string {
	all := make([]string, 0, len(parts)+1)
	if p := strings.Trim(s.cfg.S3Prefix, "/"); p != "" {
		all = append(all, p)
	}
	all = append(all, parts...)
	return strings.Join(all, "/")
}

func (s *s3Store) get(ctx context.Context, key string) ([]byte, error) {
	out, err := s.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.S3Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("plugin store: s3 get %q: %w", key, err)
	}
	defer func() { _ = out.Body.Close() }()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("plugin store: read s3 body %q: %w", key, err)
	}
	return data, nil
}

func (s *s3Store) putFile(ctx context.Context, key, path string) error {
	f, err := os.Open(path) // #nosec G304 — temp dir created by the installer
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	contentType := assetContentType(path)
	_, err = s.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.cfg.S3Bucket),
		Key:         aws.String(key),
		Body:        f,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("plugin store: s3 put %q: %w", key, err)
	}
	return nil
}

// list returns every key under prefix.
func (s *s3Store) list(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	var token *string
	for {
		page, err := s.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(s.cfg.S3Bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}
		if !aws.ToBool(page.IsTruncated) || page.NextContinuationToken == nil {
			return keys, nil
		}
		token = page.NextContinuationToken
	}
}

// deletePrefix removes every object under prefix. prefix must end in "/" so a
// plugin name that is a prefix of another ("a.b" vs "a.bc") never matches it.
func (s *s3Store) deletePrefix(ctx context.Context, prefix string) error {
	if !strings.HasSuffix(prefix, "/") {
		return fmt.Errorf("plugin store: refusing to delete non-directory prefix %q", prefix)
	}
	keys, err := s.list(ctx, prefix)
	if err != nil {
		return err
	}
	// One DeleteObject per key rather than the batch DeleteObjects call: it is
	// the one delete every S3-compatible store implements (Google Cloud
	// Storage's S3 interoperability layer has no multi-object delete), and a
	// plugin is at most a few hundred small files.
	for _, k := range keys {
		if _, err := s.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(s.cfg.S3Bucket),
			Key:    aws.String(k),
		}); err != nil {
			return fmt.Errorf("delete %q: %w", k, err)
		}
	}
	return nil
}

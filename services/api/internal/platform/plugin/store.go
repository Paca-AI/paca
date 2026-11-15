package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"
	"time"
)

// ArtifactKind names one of the three asset bundles a plugin can ship besides
// its backend (WASM + manifest + migrations).
type ArtifactKind string

const (
	// KindFrontend is the micro-frontend bundle, served at /plugins/<name>/.
	KindFrontend ArtifactKind = "frontend"
	// KindMCP is the MCP server bundle, served at /plugins-mcp/<name>/.
	KindMCP ArtifactKind = "mcp"
	// KindSkills is the Agent Skills bundle, served at /plugins-skills/<name>/.
	KindSkills ArtifactKind = "skills"
)

// MigrationFile holds a migration filename and its SQL content.
type MigrationFile struct {
	Name string // e.g. "0001_create_scenarios.sql"
	SQL  string
}

// ErrAssetNotFound is returned by [Store.OpenAsset] when the file does not
// exist (or the path is not one that may be served).
var ErrAssetNotFound = errors.New("plugin asset not found")

// Asset is one served plugin file. The caller must close Body.
type Asset struct {
	Body        io.ReadCloser
	Size        int64 // -1 when unknown
	ContentType string
	ETag        string
	ModTime     time.Time
}

// BackendBundle is the backend half of a plugin install, as files already
// extracted to a temp directory.
type BackendBundle struct {
	// WASMPath and ManifestPath are written as backend.wasm and plugin.json.
	// Leave WASMPath empty for a plugin with no backend.
	WASMPath     string
	ManifestPath string
	// Migrations are the .sql files; they replace any existing migrations
	// when HasMigrations is true.
	Migrations    []string
	HasMigrations bool
}

// Store is where installed plugin artifacts live. There are two
// implementations, chosen once by [NewStore] from PLUGINS_STORE: one backed by
// local folders and one backed by an S3-compatible bucket. Everything else
// (installer, runtime, migration runner, skills) depends only on this
// interface and never asks which backend it is talking to.
type Store interface {
	// Mode is "local" or "s3", for logging.
	Mode() string

	// CheckInstallable fails when the store is not configured well enough to
	// install a plugin's mandatory artifacts.
	CheckInstallable() error
	// RequireKind fails when an optional bundle kind cannot be stored. It lets
	// the installer fail before downloading a bundle it could not keep.
	RequireKind(kind ArtifactKind) error

	// LoadWASM returns the compiled backend binary of a plugin.
	LoadWASM(ctx context.Context, name string) ([]byte, error)
	// LoadPluginJSON returns the plugin.json manifest of a plugin.
	LoadPluginJSON(ctx context.Context, name string) ([]byte, error)
	// ListMigrations returns the plugin's SQL migrations in lexicographic
	// order, or none when it has no migrations.
	ListMigrations(ctx context.Context, name string) ([]MigrationFile, error)
	// ReadSkill returns the SKILL.md of one skill of one plugin. It makes
	// every Store a [SkillReader].
	ReadSkill(ctx context.Context, pluginName, skill string) ([]byte, error)

	// OpenAsset opens one file of a plugin's frontend, MCP or skills bundle for
	// serving over HTTP. relPath is relative to the bundle root and is
	// validated: anything that could leave the bundle yields
	// [ErrAssetNotFound]. Backend files (WASM, manifest, migrations) are not
	// reachable through it.
	OpenAsset(ctx context.Context, kind ArtifactKind, name, relPath string) (*Asset, error)

	// PutBackend installs a plugin's backend files, replacing what was there.
	PutBackend(ctx context.Context, name string, b BackendBundle) error
	// PutTree replaces one asset bundle of a plugin with the contents of srcDir.
	PutTree(ctx context.Context, kind ArtifactKind, name, srcDir string) error
	// DeleteAll removes every installed artifact of a plugin. It does not
	// touch the database.
	DeleteAll(ctx context.Context, name string) error
}

// StoreConfig parameterises [NewStore].
type StoreConfig struct {
	// Store selects the backend: "local" or "s3".
	Store string

	// WASMDir is the root directory for local backend files
	// ({WASMDir}/{name}/backend.wasm, plugin.json, migrations/).
	// FrontendDir, MCPDir and SkillsDir are the local roots for the asset
	// bundles. Only used when Store == "local".
	WASMDir     string
	FrontendDir string
	MCPDir      string
	SkillsDir   string

	// S3Bucket is the bucket name used when Store == "s3".
	S3Bucket string
	// S3Prefix is the key prefix under which plugins are stored. Keys are
	//   {S3Prefix}/{name}/backend.wasm | plugin.json | migrations/*.sql
	//   {S3Prefix}/{name}/frontend/... | mcp/... | skills/{skill}/SKILL.md
	S3Prefix string
	// S3Region is the AWS region for S3 operations.
	S3Region string
	// S3Client, when set, is used instead of building a client from the
	// default AWS credential chain. Bootstrap passes the same client the
	// attachment storage uses so a self-hosted endpoint (RustFS) works.
	S3Client S3API
}

// NewStore returns the Store selected by cfg.Store. An unknown value is an
// error rather than a silent fallback to local disk.
func NewStore(ctx context.Context, cfg StoreConfig) (Store, error) {
	switch cfg.Store {
	case "local":
		return newLocalStore(cfg), nil
	case "s3":
		return newS3Store(ctx, cfg)
	default:
		return nil, fmt.Errorf("plugin store: invalid Store value %q (must be \"local\" or \"s3\")", cfg.Store)
	}
}

func validateSkillSegment(skill string) error {
	if skill == "" || skill == "." || skill == ".." || strings.ContainsAny(skill, `/\`) {
		return fmt.Errorf("invalid skill name %q", skill)
	}
	return nil
}

// cleanAssetPath normalises a request path into a slash-separated path that
// stays inside a bundle, or reports ok=false.
func cleanAssetPath(rel string) (string, bool) {
	if rel == "" || strings.ContainsAny(rel, "\\\x00") {
		return "", false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return "", false
		}
	}
	clean := strings.TrimPrefix(path.Clean("/"+rel), "/")
	if clean == "" {
		return "", false
	}
	return clean, true
}

// assetContentType picks a Content-Type from the file extension. The standard
// library's table lacks a few types plugin bundles use (Markdown skills, WASM,
// source maps), and the gateway serves them with the right type in local mode.
func assetContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".wasm":
		return "application/wasm"
	case ".map", ".json":
		return "application/json"
	}
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func validateKind(kind ArtifactKind) error {
	switch kind {
	case KindFrontend, KindMCP, KindSkills:
		return nil
	}
	return fmt.Errorf("plugin store: unknown artifact kind %q", kind)
}

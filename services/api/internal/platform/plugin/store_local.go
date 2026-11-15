package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

// localStore keeps plugins in folders:
//
//	{WASMDir}/{name}/backend.wasm | plugin.json | migrations/*.sql
//	{FrontendDir}/{name}/...  {MCPDir}/{name}/...  {SkillsDir}/{name}/{skill}/SKILL.md
type localStore struct {
	cfg StoreConfig
}

func newLocalStore(cfg StoreConfig) *localStore { return &localStore{cfg: cfg} }

func (s *localStore) Mode() string { return "local" }

// root returns the configured folder for an asset kind.
func (s *localStore) root(kind ArtifactKind) (string, error) {
	var root, env string
	switch kind {
	case KindFrontend:
		root, env = s.cfg.FrontendDir, "PLUGINS_FRONTEND_DIR"
	case KindMCP:
		root, env = s.cfg.MCPDir, "PLUGINS_MCP_DIR"
	case KindSkills:
		root, env = s.cfg.SkillsDir, "PLUGINS_SKILLS_DIR"
	default:
		return "", validateKind(kind)
	}
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("plugin %s directory is not configured (%s)", kind, env)
	}
	return root, nil
}

func (s *localStore) CheckInstallable() error {
	if strings.TrimSpace(s.cfg.WASMDir) == "" {
		return errors.New("plugin backend directory is not configured")
	}
	if strings.TrimSpace(s.cfg.FrontendDir) == "" {
		return errors.New("plugin frontend directory is not configured")
	}
	return nil
}

func (s *localStore) RequireKind(kind ArtifactKind) error {
	_, err := s.root(kind)
	return err
}

func (s *localStore) LoadWASM(_ context.Context, name string) ([]byte, error) {
	path := filepath.Join(s.cfg.WASMDir, name, "backend.wasm")
	data, err := os.ReadFile(path) // #nosec G304 — path components from trusted config + DB
	if err != nil {
		return nil, fmt.Errorf("plugin store: read %q: %w", path, err)
	}
	return data, nil
}

func (s *localStore) LoadPluginJSON(_ context.Context, name string) ([]byte, error) {
	path := filepath.Join(s.cfg.WASMDir, name, "plugin.json")
	data, err := os.ReadFile(path) // #nosec G304 — trusted config + DB
	if err != nil {
		return nil, fmt.Errorf("plugin store: read plugin.json %q: %w", path, err)
	}
	return data, nil
}

func (s *localStore) ListMigrations(_ context.Context, name string) ([]MigrationFile, error) {
	dir := filepath.Join(s.cfg.WASMDir, name, "migrations")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil // plugin has no migrations
	}
	if err != nil {
		return nil, fmt.Errorf("plugin store: list migrations for %q: %w", name, err)
	}
	var files []MigrationFile
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sql" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		content, err := os.ReadFile(path) // #nosec G304 — trusted config + DB
		if err != nil {
			return nil, fmt.Errorf("plugin store: read migration %q: %w", path, err)
		}
		files = append(files, MigrationFile{Name: e.Name(), SQL: string(content)})
	}
	return files, nil
}

func (s *localStore) ReadSkill(_ context.Context, pluginName, skill string) ([]byte, error) {
	if err := plugindom.ValidatePluginName(pluginName); err != nil {
		return nil, err
	}
	if err := validateSkillSegment(skill); err != nil {
		return nil, err
	}
	root, err := s.root(KindSkills)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(root, pluginName, skill, "SKILL.md")) // #nosec G304 — validated segments
}

func (s *localStore) OpenAsset(_ context.Context, kind ArtifactKind, name, relPath string) (*Asset, error) {
	if err := plugindom.ValidatePluginName(name); err != nil {
		return nil, ErrAssetNotFound
	}
	rel, ok := cleanAssetPath(relPath)
	if !ok {
		return nil, ErrAssetNotFound
	}
	root, err := s.root(kind)
	if err != nil {
		return nil, err
	}
	dir, err := safePluginDir(root, name)
	if err != nil {
		return nil, ErrAssetNotFound
	}
	f, err := os.Open(filepath.Join(dir, filepath.FromSlash(rel))) // #nosec G304 — name and rel validated above
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrAssetNotFound
		}
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		_ = f.Close()
		return nil, ErrAssetNotFound
	}
	return &Asset{
		Body:        f,
		Size:        info.Size(),
		ContentType: assetContentType(rel),
		ETag:        fmt.Sprintf(`W/"%x-%x"`, info.ModTime().UnixNano(), info.Size()),
		ModTime:     info.ModTime(),
	}, nil
}

func (s *localStore) PutBackend(_ context.Context, name string, b BackendBundle) error {
	if err := plugindom.ValidatePluginName(name); err != nil {
		return err
	}
	if b.WASMPath == "" && !b.HasMigrations {
		return nil
	}
	dir, err := safePluginDir(s.cfg.WASMDir, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir backend plugin dir: %w", err)
	}
	if b.WASMPath != "" {
		if err := copyFile(b.WASMPath, filepath.Join(dir, "backend.wasm")); err != nil {
			return fmt.Errorf("write backend.wasm: %w", err)
		}
		if err := copyFile(b.ManifestPath, filepath.Join(dir, "plugin.json")); err != nil {
			return fmt.Errorf("write plugin.json: %w", err)
		}
	}
	if b.HasMigrations {
		dst := filepath.Join(dir, "migrations")
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("clear migrations dir: %w", err)
		}
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return fmt.Errorf("mkdir migrations dir: %w", err)
		}
		for _, path := range b.Migrations {
			if err := copyFile(path, filepath.Join(dst, filepath.Base(path))); err != nil {
				return fmt.Errorf("copy migration %q: %w", filepath.Base(path), err)
			}
		}
	}
	return nil
}

func (s *localStore) PutTree(_ context.Context, kind ArtifactKind, name, srcDir string) error {
	if err := plugindom.ValidatePluginName(name); err != nil {
		return err
	}
	root, err := s.root(kind)
	if err != nil {
		return err
	}
	dst, err := safePluginDir(root, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("mkdir %s root: %w", kind, err)
	}
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("clear %s dir: %w", kind, err)
	}
	if err := copyDir(srcDir, dst); err != nil {
		return fmt.Errorf("copy %s bundle: %w", kind, err)
	}
	return nil
}

// DeleteAll removes the plugin's folder under every configured root; roots
// that are not configured are skipped.
func (s *localStore) DeleteAll(_ context.Context, name string) error {
	if err := plugindom.ValidatePluginName(name); err != nil {
		return err
	}
	var errs []error
	for _, t := range []struct {
		label string
		root  string
	}{
		{"backend", s.cfg.WASMDir},
		{"frontend", s.cfg.FrontendDir},
		{"mcp", s.cfg.MCPDir},
		{"skills", s.cfg.SkillsDir},
	} {
		if strings.TrimSpace(t.root) == "" {
			continue
		}
		dir, err := safePluginDir(t.root, name)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, fmt.Errorf("remove %s dir: %w", t.label, err))
		}
	}
	return errors.Join(errs...)
}

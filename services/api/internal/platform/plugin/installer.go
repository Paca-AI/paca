package plugin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/platform/netguard"
)

// Installer downloads and installs marketplace plugin artifacts into local stores.
type Installer struct {
	backendDir  string
	frontendDir string
	mcpDir      string
	skillsDir   string
	httpClient  *http.Client
	log         *slog.Logger
}

// NewInstaller creates a marketplace installer.
func NewInstaller(backendDir, frontendDir, mcpDir, skillsDir string, httpClient *http.Client, log *slog.Logger) *Installer {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	// Enforce the private/internal IP guard at dial time so a caller-provided
	// client (which typically only sets Timeout) can't silently skip the
	// DNS-rebinding protection applied to the marketplace catalog client. A
	// caller that supplies its own Transport (e.g. a test double) is left
	// untouched.
	if httpClient.Transport == nil {
		httpClient.Transport = netguard.NewSafeHTTPTransport()
	}
	return &Installer{
		backendDir:  backendDir,
		frontendDir: frontendDir,
		mcpDir:      mcpDir,
		skillsDir:   skillsDir,
		httpClient:  httpClient,
		log:         log,
	}
}

// Install downloads, validates, and installs artifacts for one plugin.
// It returns the parsed plugin manifest used for DB registration.
//
// checkManifest is called with the downloaded manifest immediately after it
// is parsed and validated, before any of the plugin's live on-disk artifacts
// are touched (the backend/frontend/migrations/mcp/skills copy steps below
// overwrite or os.RemoveAll the existing installed directories in place, with
// no staging — so a compatibility check run after those steps would be too
// late to prevent them). If checkManifest returns an error, Install returns
// it unwrapped and leaves any already-installed artifacts for this plugin
// name completely untouched.
func (i *Installer) Install(ctx context.Context, item MarketplacePlugin, checkManifest func(plugindom.PluginManifest) error) (plugindom.PluginManifest, error) {
	if strings.TrimSpace(i.backendDir) == "" {
		return plugindom.PluginManifest{}, fmt.Errorf("plugin backend directory is not configured")
	}
	if strings.TrimSpace(i.frontendDir) == "" {
		return plugindom.PluginManifest{}, fmt.Errorf("plugin frontend directory is not configured")
	}

	tmpRoot, err := os.MkdirTemp("", "paca-plugin-install-*")
	if err != nil {
		return plugindom.PluginManifest{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpRoot) }()

	backendExtract := filepath.Join(tmpRoot, "backend")
	frontendExtract := filepath.Join(tmpRoot, "frontend")
	migrationsExtract := filepath.Join(tmpRoot, "migrations")
	manifestExtract := filepath.Join(tmpRoot, "manifest")
	mcpExtract := filepath.Join(tmpRoot, "mcp")
	skillsExtract := filepath.Join(tmpRoot, "skills")

	if err := i.downloadAndExtractTarGz(ctx, item.Artifacts.ManifestTarGzURL, manifestExtract); err != nil {
		return plugindom.PluginManifest{}, fmt.Errorf("download manifest: %w", err)
	}
	if strings.TrimSpace(item.Artifacts.BackendTarGzURL) != "" {
		if err := i.downloadAndExtractTarGz(ctx, item.Artifacts.BackendTarGzURL, backendExtract); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("download backend: %w", err)
		}
	}
	if strings.TrimSpace(item.Artifacts.FrontendTarGzURL) != "" {
		if err := i.downloadAndExtractTarGz(ctx, item.Artifacts.FrontendTarGzURL, frontendExtract); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("download frontend: %w", err)
		}
	}
	if strings.TrimSpace(item.Artifacts.MigrationsTarGzURL) != "" {
		if err := i.downloadAndExtractTarGz(ctx, item.Artifacts.MigrationsTarGzURL, migrationsExtract); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("download migrations: %w", err)
		}
	}

	manifestPath, err := findFirstFile(manifestExtract, func(path string) bool {
		return filepath.Base(path) == "plugin.json"
	})
	if err != nil {
		return plugindom.PluginManifest{}, fmt.Errorf("resolve plugin.json: %w", err)
	}

	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return plugindom.PluginManifest{}, fmt.Errorf("read plugin.json: %w", err)
	}
	var manifest plugindom.PluginManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return plugindom.PluginManifest{}, fmt.Errorf("parse plugin.json: %w", err)
	}
	if manifest.ID != item.Name {
		return plugindom.PluginManifest{}, fmt.Errorf("manifest id %q does not match catalog name %q", manifest.ID, item.Name)
	}
	if checkManifest != nil {
		if err := checkManifest(manifest); err != nil {
			return plugindom.PluginManifest{}, err
		}
	}

	backendPluginDir := filepath.Join(i.backendDir, item.Name)
	frontendPluginDir := filepath.Join(i.frontendDir, item.Name)

	// Backend WASM — optional.
	if strings.TrimSpace(item.Artifacts.BackendTarGzURL) != "" {
		backendWASM, err := findFirstFile(backendExtract, func(path string) bool {
			return strings.HasSuffix(strings.ToLower(filepath.Base(path)), ".wasm")
		})
		if err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("resolve .wasm file: %w", err)
		}
		if err := os.MkdirAll(backendPluginDir, 0o755); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("mkdir backend plugin dir: %w", err)
		}
		if err := copyFile(backendWASM, filepath.Join(backendPluginDir, "backend.wasm")); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("write backend.wasm: %w", err) //nolint:govet
		}
		if err := copyFile(manifestPath, filepath.Join(backendPluginDir, "plugin.json")); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("write plugin.json: %w", err)
		}
	}

	// Migrations — optional.
	if strings.TrimSpace(item.Artifacts.MigrationsTarGzURL) != "" {
		if err := os.MkdirAll(backendPluginDir, 0o755); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("mkdir backend plugin dir: %w", err)
		}
		migrationsDst := filepath.Join(backendPluginDir, "migrations")
		if err := os.RemoveAll(migrationsDst); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("clear migrations dir: %w", err)
		}
		if err := os.MkdirAll(migrationsDst, 0o755); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("mkdir migrations dir: %w", err)
		}
		migrationFiles, err := findAllFiles(migrationsExtract, func(path string) bool {
			return strings.HasSuffix(strings.ToLower(path), ".sql")
		})
		if err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("list migration files: %w", err)
		}
		for _, path := range migrationFiles {
			if err := copyFile(path, filepath.Join(migrationsDst, filepath.Base(path))); err != nil {
				return plugindom.PluginManifest{}, fmt.Errorf("copy migration %q: %w", filepath.Base(path), err)
			}
		}
	}

	// Frontend bundle — optional.
	if strings.TrimSpace(item.Artifacts.FrontendTarGzURL) != "" {
		frontendContentRoot, err := resolveSingleRootDir(frontendExtract)
		if err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("resolve frontend content root: %w", err)
		}
		if err := os.MkdirAll(i.frontendDir, 0o755); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("mkdir frontend root: %w", err)
		}
		if err := os.RemoveAll(frontendPluginDir); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("clear frontend dir: %w", err)
		}
		if err := copyDir(frontendContentRoot, frontendPluginDir); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("copy frontend bundle: %w", err)
		}
	}

	// MCP bundle — optional, installed to a dedicated mcp store served at /plugins-mcp/.
	if strings.TrimSpace(item.Artifacts.MCPTarGzURL) != "" {
		if strings.TrimSpace(i.mcpDir) == "" {
			return plugindom.PluginManifest{}, fmt.Errorf("plugin MCP directory is not configured (PLUGINS_MCP_DIR)")
		}
		if err := i.downloadAndExtractTarGz(ctx, item.Artifacts.MCPTarGzURL, mcpExtract); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("download mcp: %w", err)
		}
		mcpContentRoot, err := resolveSingleRootDir(mcpExtract)
		if err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("resolve mcp content root: %w", err)
		}
		mcpPluginDir := filepath.Join(i.mcpDir, item.Name)
		if err := os.MkdirAll(i.mcpDir, 0o755); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("mkdir mcp root: %w", err)
		}
		if err := os.RemoveAll(mcpPluginDir); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("clear mcp dir: %w", err)
		}
		if err := copyDir(mcpContentRoot, mcpPluginDir); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("copy mcp bundle: %w", err)
		}
	}

	// Skills bundle — optional, installed to a dedicated skills store served at /plugins-skills/.
	if strings.TrimSpace(item.Artifacts.SkillsTarGzURL) != "" {
		if strings.TrimSpace(i.skillsDir) == "" {
			return plugindom.PluginManifest{}, fmt.Errorf("plugin skills directory is not configured (PLUGINS_SKILLS_DIR)")
		}
		if err := i.downloadAndExtractTarGz(ctx, item.Artifacts.SkillsTarGzURL, skillsExtract); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("download skills: %w", err)
		}
		skillsContentRoot, err := resolveSingleRootDir(skillsExtract)
		if err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("resolve skills content root: %w", err)
		}
		skillsPluginDir := filepath.Join(i.skillsDir, item.Name)
		if err := os.MkdirAll(i.skillsDir, 0o755); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("mkdir skills root: %w", err)
		}
		if err := os.RemoveAll(skillsPluginDir); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("clear skills dir: %w", err)
		}
		if err := copyDir(skillsContentRoot, skillsPluginDir); err != nil {
			return plugindom.PluginManifest{}, fmt.Errorf("copy skills bundle: %w", err)
		}
	}

	if i.log != nil {
		i.log.Info("plugin artifacts installed", "name", item.Name, "backend_dir", backendPluginDir, "frontend_dir", frontendPluginDir)
	}

	return manifest, nil
}

// Uninstall removes all installed files for a plugin from the backend,
// frontend, MCP, and skills stores. It does NOT touch the database.
func (i *Installer) Uninstall(name string) error {
	backendPluginDir := filepath.Join(i.backendDir, name)
	frontendPluginDir := filepath.Join(i.frontendDir, name)
	mcpPluginDir := filepath.Join(i.mcpDir, name)
	skillsPluginDir := filepath.Join(i.skillsDir, name)

	var errs []error
	if err := os.RemoveAll(backendPluginDir); err != nil {
		errs = append(errs, fmt.Errorf("remove backend dir: %w", err))
	}
	if err := os.RemoveAll(frontendPluginDir); err != nil {
		errs = append(errs, fmt.Errorf("remove frontend dir: %w", err))
	}
	if strings.TrimSpace(i.mcpDir) != "" {
		if err := os.RemoveAll(mcpPluginDir); err != nil {
			errs = append(errs, fmt.Errorf("remove mcp dir: %w", err))
		}
	}
	if strings.TrimSpace(i.skillsDir) != "" {
		if err := os.RemoveAll(skillsPluginDir); err != nil {
			errs = append(errs, fmt.Errorf("remove skills dir: %w", err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	if i.log != nil {
		i.log.Info("plugin files removed", "name", name)
	}
	return nil
}

func (i *Installer) downloadAndExtractTarGz(ctx context.Context, url, dest string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("artifact URL is empty")
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("mkdir extract destination: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := i.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download artifact: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	// Limit download size to 100MB
	const maxDownloadSize = 100 * 1024 * 1024
	downloadLimitReader := io.LimitReader(resp.Body, maxDownloadSize)

	if err := untarGz(downloadLimitReader, dest, maxDownloadSize); err != nil {
		return fmt.Errorf("extract artifact: %w", err)
	}
	return nil
}

func untarGz(r io.Reader, dest string, maxExtractedSize int64) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	destClean := filepath.Clean(dest)

	const maxFiles = 10000
	var fileCount int
	var totalExtracted int64

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		fileCount++
		if fileCount > maxFiles {
			return fmt.Errorf("archive contains too many files (max %d)", maxFiles)
		}

		name := filepath.Clean(hdr.Name)
		if name == "." || strings.HasPrefix(name, "..") {
			continue
		}
		target := filepath.Join(dest, name)
		targetClean := filepath.Clean(target)
		if !strings.HasPrefix(targetClean, destClean+string(os.PathSeparator)) && targetClean != destClean {
			return fmt.Errorf("illegal archive path %q", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetClean, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			// Check individual file size
			const maxFileSize = 50 * 1024 * 1024 // 50MB per file
			if hdr.Size > maxFileSize {
				return fmt.Errorf("file %q exceeds maximum size of %d bytes", hdr.Name, maxFileSize)
			}

			if err := os.MkdirAll(filepath.Dir(targetClean), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(targetClean, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}

			// Limit bytes written per file
			fileLimitReader := io.LimitReader(tr, hdr.Size)
			written, err := io.Copy(f, fileLimitReader)
			if err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}

			totalExtracted += written
			if totalExtracted > maxExtractedSize {
				return fmt.Errorf("total extracted size exceeds maximum of %d bytes", maxExtractedSize)
			}
		}
	}

	return nil
}

func findFirstFile(root string, predicate func(path string) bool) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if predicate(path) {
			found = path
			return io.EOF
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("file not found in %s", root)
	}
	return found, nil
}

func findAllFiles(root string, predicate func(path string) bool) ([]string, error) {
	items := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if predicate(path) {
			items = append(items, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func resolveSingleRootDir(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		return root, nil
	}
	return filepath.Join(root, entries[0].Name()), nil
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Sync()
}

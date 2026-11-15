package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

// MigrationReport lists what [MigrateLocalToStore] found and copied.
type MigrationReport struct {
	// Plugins is one entry per plugin found in the local folders, sorted by name.
	Plugins []MigratedPlugin
	// Skipped are folder names that are not valid plugin names.
	Skipped []string
}

// MigratedPlugin is the outcome for one plugin.
type MigratedPlugin struct {
	Name string
	// Parts names what was (or, in a dry run, would be) copied: "backend",
	// "migrations", "frontend", "mcp", "skills".
	Parts []string
	Err   error
}

// MigrateLocalToStore copies every plugin installed in the local folders of
// src (WASMDir, FrontendDir, MCPDir, SkillsDir) into dst, typically the S3
// store, so an install can switch PLUGINS_STORE from local to s3 without
// reinstalling each plugin. The local folders are only read, never changed.
//
// It is safe to re-run: each part replaces what dst already holds. Backend
// binaries are read back from dst and compared with the local file, so a
// silent upload failure is reported instead of discovered at plugin load.
// With dryRun nothing is written. A failure on one plugin is recorded on its
// entry and the rest still run; the returned error joins them.
func MigrateLocalToStore(ctx context.Context, src StoreConfig, dst Store, dryRun bool) (MigrationReport, error) {
	var report MigrationReport

	roots := []struct {
		kind ArtifactKind // empty for the backend root
		dir  string
	}{
		{"", src.WASMDir},
		{KindFrontend, src.FrontendDir},
		{KindMCP, src.MCPDir},
		{KindSkills, src.SkillsDir},
	}

	names := map[string]struct{}{}
	for _, r := range roots {
		if strings.TrimSpace(r.dir) == "" {
			continue
		}
		entries, err := os.ReadDir(r.dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return report, fmt.Errorf("read %s: %w", r.dir, err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if plugindom.ValidatePluginName(e.Name()) != nil {
				report.Skipped = append(report.Skipped, e.Name())
				continue
			}
			names[e.Name()] = struct{}{}
		}
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	sort.Strings(report.Skipped)

	var errs []error
	for _, name := range sorted {
		pm := MigratedPlugin{Name: name}
		pm.Parts, pm.Err = migrateOne(ctx, src, dst, name, dryRun)
		if pm.Err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, pm.Err))
		}
		report.Plugins = append(report.Plugins, pm)
	}
	return report, errors.Join(errs...)
}

func migrateOne(ctx context.Context, src StoreConfig, dst Store, name string, dryRun bool) ([]string, error) {
	var parts []string

	// Backend: backend.wasm + plugin.json, and/or migrations/*.sql.
	var bundle BackendBundle
	if dir := backendDir(src, name); dir != "" {
		wasm := filepath.Join(dir, "backend.wasm")
		manifest := filepath.Join(dir, "plugin.json")
		if fileExists(wasm) {
			if !fileExists(manifest) {
				return parts, fmt.Errorf("%s has backend.wasm but no plugin.json", dir)
			}
			bundle.WASMPath, bundle.ManifestPath = wasm, manifest
			parts = append(parts, "backend")
		}
		if migs, err := filepath.Glob(filepath.Join(dir, "migrations", "*.sql")); err == nil && len(migs) > 0 {
			sort.Strings(migs)
			bundle.Migrations, bundle.HasMigrations = migs, true
			parts = append(parts, "migrations")
		}
	}
	if !dryRun && (bundle.WASMPath != "" || bundle.HasMigrations) {
		if err := dst.PutBackend(ctx, name, bundle); err != nil {
			return parts, err
		}
		if bundle.WASMPath != "" {
			if err := verifyWASM(ctx, dst, name, bundle.WASMPath); err != nil {
				return parts, err
			}
		}
	}

	// Asset bundles.
	for _, t := range []struct {
		kind ArtifactKind
		root string
	}{
		{KindFrontend, src.FrontendDir},
		{KindMCP, src.MCPDir},
		{KindSkills, src.SkillsDir},
	} {
		if strings.TrimSpace(t.root) == "" {
			continue
		}
		dir := filepath.Join(t.root, name)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		parts = append(parts, string(t.kind))
		if dryRun {
			continue
		}
		if err := dst.PutTree(ctx, t.kind, name, dir); err != nil {
			return parts, err
		}
	}
	return parts, nil
}

// backendDir returns the plugin's local backend folder, or "" when absent.
func backendDir(src StoreConfig, name string) string {
	if strings.TrimSpace(src.WASMDir) == "" {
		return ""
	}
	dir := filepath.Join(src.WASMDir, name)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func verifyWASM(ctx context.Context, dst Store, name, localPath string) error {
	want, err := os.ReadFile(localPath) // #nosec G304 — path built from the configured folder
	if err != nil {
		return err
	}
	got, err := dst.LoadWASM(ctx, name)
	if err != nil {
		return fmt.Errorf("verify backend.wasm: %w", err)
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("verify backend.wasm: uploaded copy differs from %s", localPath)
	}
	return nil
}

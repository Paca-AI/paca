package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

// buildTarGz packages files (relative path -> content) into an in-memory
// .tar.gz archive, mirroring the marketplace artifact format Install expects.
func buildTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("write tar header for %q: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write tar content for %q: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}

// newTestInstaller wires an Installer against a local httptest server. The
// client is given a non-nil, non-netguard Transport so it can reach the
// loopback test server — see NewInstaller's doc comment on why a caller-
// supplied Transport bypasses the SSRF guard.
func newTestInstaller(t *testing.T, backendDir, frontendDir string) *Installer {
	t.Helper()
	return NewInstaller(backendDir, frontendDir, "", "", &http.Client{Transport: http.DefaultTransport}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// TestInstall_CheckManifestRejection_LeavesExistingArtifactsUntouched is a
// regression test for a bug where an incompatible-host-version rejection
// arrived too late: Install would already have overwritten/removed the
// currently-installed plugin's live backend files (copyFile uses O_TRUNC,
// migrations/frontend/mcp/skills dirs are os.RemoveAll'd) before the caller's
// compatibility check ran. checkManifest must run — and this test asserts it
// runs — before any of that, so a rejected upgrade leaves the previously
// installed plugin exactly as it was.
func TestInstall_CheckManifestRejection_LeavesExistingArtifactsUntouched(t *testing.T) {
	const pluginName = "com.paca.test"

	manifestTarGz := buildTarGz(t, map[string]string{
		"plugin.json": `{"id":"com.paca.test","version":"2.0.0","minCoreVersion":"99.0.0"}`,
	})
	backendTarGz := buildTarGz(t, map[string]string{
		"backend.wasm": "NEW-WASM-BYTES",
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/manifest.tar.gz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(manifestTarGz) })
	mux.HandleFunc("/backend.tar.gz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(backendTarGz) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	backendDir := t.TempDir()
	frontendDir := t.TempDir()

	// Simulate a plugin already installed at the previous version.
	existingPluginDir := filepath.Join(backendDir, pluginName)
	if err := os.MkdirAll(existingPluginDir, 0o755); err != nil {
		t.Fatalf("seed existing plugin dir: %v", err)
	}
	wasmPath := filepath.Join(existingPluginDir, "backend.wasm")
	manifestPath := filepath.Join(existingPluginDir, "plugin.json")
	if err := os.WriteFile(wasmPath, []byte("OLD-WASM-BYTES"), 0o644); err != nil {
		t.Fatalf("seed old backend.wasm: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"id":"com.paca.test","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatalf("seed old plugin.json: %v", err)
	}

	installer := newTestInstaller(t, backendDir, frontendDir)

	errIncompatible := errors.New("host too old")
	item := MarketplacePlugin{
		Name:    pluginName,
		Version: "2.0.0",
		Artifacts: MarketplacePluginArtifact{
			ManifestTarGzURL: srv.URL + "/manifest.tar.gz",
			BackendTarGzURL:  srv.URL + "/backend.tar.gz",
		},
	}

	_, err := installer.Install(context.Background(), item, func(m plugindom.PluginManifest) error {
		if m.ID != pluginName {
			t.Errorf("expected checkManifest to receive the downloaded manifest, got ID %q", m.ID)
		}
		return errIncompatible
	})
	if !errors.Is(err, errIncompatible) {
		t.Fatalf("expected Install to return checkManifest's error unwrapped, got: %v", err)
	}

	gotWASM, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read backend.wasm after rejected install: %v", err)
	}
	if string(gotWASM) != "OLD-WASM-BYTES" {
		t.Errorf("backend.wasm was modified despite checkManifest rejection: got %q", gotWASM)
	}

	gotManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read plugin.json after rejected install: %v", err)
	}
	if string(gotManifest) != `{"id":"com.paca.test","version":"1.0.0"}` {
		t.Errorf("plugin.json was modified despite checkManifest rejection: got %q", gotManifest)
	}
}

// TestInstall_CheckManifestAccepted_WritesArtifacts is the accompanying
// happy-path case: when checkManifest allows the manifest, Install proceeds
// to write the downloaded artifacts as before.
func TestInstall_CheckManifestAccepted_WritesArtifacts(t *testing.T) {
	const pluginName = "com.paca.test"

	manifestTarGz := buildTarGz(t, map[string]string{
		"plugin.json": `{"id":"com.paca.test","version":"2.0.0"}`,
	})
	backendTarGz := buildTarGz(t, map[string]string{
		"backend.wasm": "NEW-WASM-BYTES",
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/manifest.tar.gz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(manifestTarGz) })
	mux.HandleFunc("/backend.tar.gz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(backendTarGz) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	backendDir := t.TempDir()
	frontendDir := t.TempDir()
	installer := newTestInstaller(t, backendDir, frontendDir)

	item := MarketplacePlugin{
		Name:    pluginName,
		Version: "2.0.0",
		Artifacts: MarketplacePluginArtifact{
			ManifestTarGzURL: srv.URL + "/manifest.tar.gz",
			BackendTarGzURL:  srv.URL + "/backend.tar.gz",
		},
	}

	manifest, err := installer.Install(context.Background(), item, func(plugindom.PluginManifest) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest.Version != "2.0.0" {
		t.Errorf("expected returned manifest version %q, got %q", "2.0.0", manifest.Version)
	}

	got, err := os.ReadFile(filepath.Join(backendDir, pluginName, "backend.wasm"))
	if err != nil {
		t.Fatalf("read backend.wasm: %v", err)
	}
	if string(got) != "NEW-WASM-BYTES" {
		t.Errorf("expected backend.wasm to be updated, got %q", got)
	}
}

// TestInstall_NilCheckManifest_StillWrites confirms checkManifest is
// optional — existing/future callers that don't need a gate can pass nil.
func TestInstall_NilCheckManifest_StillWrites(t *testing.T) {
	const pluginName = "com.paca.test"

	manifestTarGz := buildTarGz(t, map[string]string{
		"plugin.json": `{"id":"com.paca.test","version":"1.0.0"}`,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/manifest.tar.gz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(manifestTarGz) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	installer := newTestInstaller(t, t.TempDir(), t.TempDir())

	item := MarketplacePlugin{
		Name:    pluginName,
		Version: "1.0.0",
		Artifacts: MarketplacePluginArtifact{
			ManifestTarGzURL: srv.URL + "/manifest.tar.gz",
		},
	}

	if _, err := installer.Install(context.Background(), item, nil); err != nil {
		t.Fatalf("unexpected error with nil checkManifest: %v", err)
	}
}

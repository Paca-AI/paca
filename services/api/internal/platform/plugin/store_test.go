package plugin

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// fakeS3 is an in-memory S3API: just enough of GetObject, PutObject,
// DeleteObjects and ListObjectsV2 (with a small page size, so pagination is
// exercised) for the plugin Store.
type fakeS3 struct {
	mu       sync.Mutex
	objects  map[string][]byte
	types    map[string]string
	pageSize int
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string][]byte{}, types: map[string]string{}, pageSize: 2}
}

func (f *fakeS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.objects[aws.ToString(in.Key)]
	if !ok {
		return nil, &s3types.NoSuchKey{}
	}
	return &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String(f.types[aws.ToString(in.Key)]),
		ETag:          aws.String(`"etag"`),
	}, nil
}

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	data, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[aws.ToString(in.Key)] = data
	f.types[aws.ToString(in.Key)] = aws.ToString(in.ContentType)
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) DeleteObject(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, aws.ToString(in.Key))
	return &s3.DeleteObjectOutput{}, nil
}

func (f *fakeS3) ListObjectsV2(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for k := range f.objects {
		if strings.HasPrefix(k, aws.ToString(in.Prefix)) && k > aws.ToString(in.ContinuationToken) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := &s3.ListObjectsV2Output{}
	if len(keys) > f.pageSize {
		keys = keys[:f.pageSize]
		out.IsTruncated = aws.Bool(true)
		out.NextContinuationToken = aws.String(keys[len(keys)-1])
	}
	for _, k := range keys {
		out.Contents = append(out.Contents, s3types.Object{Key: aws.String(k)})
	}
	return out, nil
}

func (f *fakeS3) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.objects))
	for k := range f.objects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func newS3TestStore(t *testing.T) (Store, *fakeS3) {
	t.Helper()
	fake := newFakeS3()
	st, err := NewStore(context.Background(), StoreConfig{Store: "s3", S3Bucket: "b", S3Prefix: "plugins", S3Client: fake})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return st, fake
}

func TestNewStore_S3RequiresBucket(t *testing.T) {
	if _, err := NewStore(context.Background(), StoreConfig{Store: "s3", S3Client: newFakeS3()}); err == nil {
		t.Fatal("expected an error for PLUGINS_STORE=s3 without a bucket")
	}
}

// TestInstall_S3_RoundTrip installs a plugin with every artifact kind into the
// bucket, reads it back through the Store, replaces it, and uninstalls it —
// and checks a plugin whose name is a prefix of another is left alone.
func TestInstall_S3_RoundTrip(t *testing.T) {
	const name = "com.paca.test"
	files := func(wasm string) map[string][]byte {
		return map[string][]byte{
			"/manifest.tar.gz": buildTarGz(t, map[string]string{"plugin.json": `{"id":"com.paca.test","version":"1.0.0"}`}),
			"/backend.tar.gz":  buildTarGz(t, map[string]string{"backend.wasm": wasm}),
			"/migrations.tar.gz": buildTarGz(t, map[string]string{
				"0001_a.sql": "CREATE TABLE a();", "0002_b.sql": "CREATE TABLE b();", "0003_c.sql": "CREATE TABLE c();",
			}),
			"/frontend.tar.gz": buildTarGz(t, map[string]string{"assets/remoteEntry.js": "js", "assets/chunk.css": "css", "index.html": "h"}),
			"/mcp.tar.gz":      buildTarGz(t, map[string]string{"mcp.js": "mcp"}),
			"/skills.tar.gz":   buildTarGz(t, map[string]string{"paca-x/SKILL.md": "skill body", "paca-y/SKILL.md": "other"}),
		}
	}
	current := files("WASM-1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(current[r.URL.Path])
	}))
	defer srv.Close()

	st, fake := newS3TestStore(t)
	// A different plugin whose name starts with ours must survive our uninstall.
	fake.objects["plugins/com.paca.test2/backend.wasm"] = []byte("other")

	inst := NewInstaller(st, &http.Client{Transport: http.DefaultTransport}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	item := MarketplacePlugin{Name: name, Version: "1.0.0", Artifacts: MarketplacePluginArtifact{
		ManifestTarGzURL:   srv.URL + "/manifest.tar.gz",
		BackendTarGzURL:    srv.URL + "/backend.tar.gz",
		MigrationsTarGzURL: srv.URL + "/migrations.tar.gz",
		FrontendTarGzURL:   srv.URL + "/frontend.tar.gz",
		MCPTarGzURL:        srv.URL + "/mcp.tar.gz",
		SkillsTarGzURL:     srv.URL + "/skills.tar.gz",
	}}
	ctx := context.Background()
	if _, err := inst.Install(ctx, item, nil); err != nil {
		t.Fatalf("Install: %v", err)
	}

	want := []string{
		"plugins/com.paca.test/backend.wasm",
		"plugins/com.paca.test/frontend/assets/chunk.css",
		"plugins/com.paca.test/frontend/assets/remoteEntry.js",
		"plugins/com.paca.test/frontend/index.html",
		"plugins/com.paca.test/mcp/mcp.js",
		"plugins/com.paca.test/migrations/0001_a.sql",
		"plugins/com.paca.test/migrations/0002_b.sql",
		"plugins/com.paca.test/migrations/0003_c.sql",
		"plugins/com.paca.test/plugin.json",
		"plugins/com.paca.test/skills/paca-x/SKILL.md",
		"plugins/com.paca.test/skills/paca-y/SKILL.md",
		"plugins/com.paca.test2/backend.wasm",
	}
	if got := fake.keys(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("bucket keys after install:\n got %v\nwant %v", got, want)
	}
	if ct := fake.types["plugins/com.paca.test/frontend/assets/remoteEntry.js"]; !strings.Contains(ct, "javascript") {
		t.Errorf("remoteEntry.js content type = %q, want a javascript type", ct)
	}

	if b, err := st.LoadWASM(ctx, name); err != nil || string(b) != "WASM-1" {
		t.Fatalf("LoadWASM = %q, %v", b, err)
	}
	if b, err := st.LoadPluginJSON(ctx, name); err != nil || !strings.Contains(string(b), `"id":"com.paca.test"`) {
		t.Fatalf("LoadPluginJSON = %q, %v", b, err)
	}
	migs, err := st.ListMigrations(ctx, name)
	if err != nil || len(migs) != 3 || migs[0].Name != "0001_a.sql" || migs[2].Name != "0003_c.sql" {
		t.Fatalf("ListMigrations = %+v, %v (3 files across 2-per-page listing)", migs, err)
	}
	if b, err := st.ReadSkill(ctx, name, "paca-x"); err != nil || string(b) != "skill body" {
		t.Fatalf("ReadSkill = %q, %v", b, err)
	}

	// Reinstall with fewer files: stale objects must be removed.
	current = files("WASM-2")
	current["/frontend.tar.gz"] = buildTarGz(t, map[string]string{"assets/remoteEntry.js": "js2", "index.html": "h"})
	current["/migrations.tar.gz"] = buildTarGz(t, map[string]string{"0001_a.sql": "CREATE TABLE a();"})
	if _, err := inst.Install(ctx, item, nil); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if b, _ := st.LoadWASM(ctx, name); string(b) != "WASM-2" {
		t.Errorf("wasm after reinstall = %q", b)
	}
	if _, ok := fake.objects["plugins/com.paca.test/frontend/assets/chunk.css"]; ok {
		t.Error("stale frontend file survived reinstall")
	}
	if migs, _ := st.ListMigrations(ctx, name); len(migs) != 1 {
		t.Errorf("migrations after reinstall = %+v, want 1", migs)
	}

	if err := inst.Uninstall(name); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if got := fake.keys(); len(got) != 1 || got[0] != "plugins/com.paca.test2/backend.wasm" {
		t.Fatalf("bucket keys after uninstall = %v, want only the sibling plugin", got)
	}
}

func TestStore_S3_RejectsUnsafeNames(t *testing.T) {
	st, fake := newS3TestStore(t)
	fake.objects["plugins/victim/backend.wasm"] = []byte("x")
	ctx := context.Background()
	for _, name := range []string{"..", "../victim", "a/b", "", "."} {
		if err := st.DeleteAll(ctx, name); err == nil {
			t.Errorf("DeleteAll(%q) = nil, want error", name)
		}
	}
	if _, err := st.ReadSkill(ctx, "com.paca.x", "../other"); err == nil {
		t.Error("ReadSkill accepted a traversing skill name")
	}
	if len(fake.keys()) != 1 {
		t.Fatalf("bucket was modified: %v", fake.keys())
	}
}

func TestStore_Local_PutTreeDeleteAndSkill(t *testing.T) {
	root := t.TempDir()
	st := newLocalStore(StoreConfig{WASMDir: filepath.Join(root, "b"), FrontendDir: filepath.Join(root, "f"), MCPDir: filepath.Join(root, "m"), SkillsDir: filepath.Join(root, "s")})
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "paca-x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "paca-x", "SKILL.md"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := st.PutTree(ctx, KindSkills, "com.paca.x", src); err != nil {
		t.Fatalf("PutTree: %v", err)
	}
	if b, err := st.ReadSkill(ctx, "com.paca.x", "paca-x"); err != nil || string(b) != "body" {
		t.Fatalf("ReadSkill = %q, %v", b, err)
	}
	if err := st.DeleteAll(ctx, "com.paca.x"); err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	if _, err := st.ReadSkill(ctx, "com.paca.x", "paca-x"); err == nil {
		t.Error("skill still readable after DeleteAll")
	}

	unset := newLocalStore(StoreConfig{})
	if err := unset.PutTree(ctx, KindMCP, "com.paca.x", src); err == nil || !strings.Contains(err.Error(), "PLUGINS_MCP_DIR") {
		t.Errorf("PutTree with unset MCP dir = %v, want a PLUGINS_MCP_DIR error", err)
	}
}

func TestStore_OpenAsset(t *testing.T) {
	read := func(t *testing.T, st Store, kind ArtifactKind, name, rel string) (string, error) {
		t.Helper()
		a, err := st.OpenAsset(context.Background(), kind, name, rel)
		if err != nil {
			return "", err
		}
		defer func() { _ = a.Body.Close() }()
		b, _ := io.ReadAll(a.Body)
		return string(b), nil
	}

	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "assets", "remoteEntry.js"), []byte("js"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("h"), 0o644); err != nil {
		t.Fatal(err)
	}

	s3st, _ := newS3TestStore(t)
	root := t.TempDir()
	local := newLocalStore(StoreConfig{WASMDir: filepath.Join(root, "b"), FrontendDir: filepath.Join(root, "f"), MCPDir: filepath.Join(root, "m"), SkillsDir: filepath.Join(root, "s")})
	secret := filepath.Join(root, "b", "com.paca.x")
	if err := os.MkdirAll(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secret, "backend.wasm"), []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, st := range map[string]Store{"local": local, "s3": s3st} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if err := st.PutTree(ctx, KindFrontend, "com.paca.x", src); err != nil {
				t.Fatalf("PutTree: %v", err)
			}
			if got, err := read(t, st, KindFrontend, "com.paca.x", "assets/remoteEntry.js"); err != nil || got != "js" {
				t.Fatalf("OpenAsset = %q, %v", got, err)
			}
			a, _ := st.OpenAsset(ctx, KindFrontend, "com.paca.x", "assets/remoteEntry.js")
			if !strings.Contains(a.ContentType, "javascript") || a.ETag == "" {
				t.Errorf("metadata = %q %q", a.ContentType, a.ETag)
			}
			_ = a.Body.Close()

			for _, rel := range []string{"missing.js", "", "assets", "../backend.wasm", "assets/../../b/com.paca.x/backend.wasm", `..\\x`, "a\x00b"} {
				if _, err := read(t, st, KindFrontend, "com.paca.x", rel); !errors.Is(err, ErrAssetNotFound) {
					t.Errorf("OpenAsset(%q) err = %v, want ErrAssetNotFound", rel, err)
				}
			}
			if _, err := read(t, st, KindFrontend, "../x", "index.html"); !errors.Is(err, ErrAssetNotFound) {
				t.Errorf("traversing plugin name err = %v", err)
			}
			if _, err := read(t, st, ArtifactKind("backend"), "com.paca.x", "backend.wasm"); err == nil {
				t.Error("backend files must not be openable as assets")
			}
		})
	}
}

func TestMigrateLocalToStore(t *testing.T) {
	root := t.TempDir()
	src := StoreConfig{Store: "local", WASMDir: filepath.Join(root, "b"), FrontendDir: filepath.Join(root, "f"), MCPDir: filepath.Join(root, "m"), SkillsDir: filepath.Join(root, "s")}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Plugin with everything.
	write("b/com.paca.full/backend.wasm", "WASM")
	write("b/com.paca.full/plugin.json", `{"id":"com.paca.full"}`)
	write("b/com.paca.full/migrations/0002_b.sql", "B")
	write("b/com.paca.full/migrations/0001_a.sql", "A")
	write("f/com.paca.full/assets/remoteEntry.js", "js")
	write("m/com.paca.full/mcp.js", "mcp")
	write("s/com.paca.full/paca-x/SKILL.md", "skill")
	// Frontend-only plugin, plus a folder that is not a plugin name.
	write("f/com.paca.ui/assets/remoteEntry.js", "js2")
	write("f/not a plugin/x.js", "x")
	// A broken backend: wasm without a manifest.
	write("b/com.paca.bad/backend.wasm", "W")

	dst, fake := newS3TestStore(t)
	ctx := context.Background()

	// Dry run writes nothing but reports everything.
	rep, err := MigrateLocalToStore(ctx, src, dst, true)
	if err == nil || !strings.Contains(err.Error(), "com.paca.bad") {
		t.Fatalf("dry run err = %v, want the broken plugin reported", err)
	}
	if len(fake.keys()) != 0 {
		t.Fatalf("dry run wrote %v", fake.keys())
	}
	if len(rep.Plugins) != 3 || len(rep.Skipped) != 1 || rep.Skipped[0] != "not a plugin" {
		t.Fatalf("report = %+v", rep)
	}

	// Real run: the broken plugin fails, the others still migrate.
	rep, err = MigrateLocalToStore(ctx, src, dst, false)
	if err == nil {
		t.Fatal("want an error for com.paca.bad")
	}
	for _, p := range rep.Plugins {
		if (p.Name == "com.paca.bad") != (p.Err != nil) {
			t.Errorf("plugin %s err = %v", p.Name, p.Err)
		}
	}
	if b, err := dst.LoadWASM(ctx, "com.paca.full"); err != nil || string(b) != "WASM" {
		t.Fatalf("LoadWASM = %q, %v", b, err)
	}
	if migs, _ := dst.ListMigrations(ctx, "com.paca.full"); len(migs) != 2 || migs[0].Name != "0001_a.sql" {
		t.Fatalf("migrations = %+v", migs)
	}
	if b, err := dst.ReadSkill(ctx, "com.paca.full", "paca-x"); err != nil || string(b) != "skill" {
		t.Fatalf("ReadSkill = %q, %v", b, err)
	}
	if a, err := dst.OpenAsset(ctx, KindFrontend, "com.paca.ui", "assets/remoteEntry.js"); err != nil {
		t.Fatalf("frontend-only plugin missing: %v", err)
	} else {
		_ = a.Body.Close()
	}
	if _, err := dst.LoadWASM(ctx, "com.paca.bad"); err == nil {
		t.Error("broken plugin must not be half-uploaded")
	}
	if _, err := os.Stat(filepath.Join(root, "b", "com.paca.full", "backend.wasm")); err != nil {
		t.Errorf("local source was modified: %v", err)
	}

	// Re-running is safe and idempotent.
	before := fake.keys()
	if _, err := MigrateLocalToStore(ctx, src, dst, false); err == nil {
		t.Fatal("rerun should still report the broken plugin")
	}
	if got := fake.keys(); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Errorf("rerun changed the bucket:\n%v\n%v", before, got)
	}
}

func TestStore_S3_UpgradeKeepsOldFilesUntilNewUploaded(t *testing.T) {
	ctx := context.Background()
	st, fake := newS3TestStore(t)

	v1 := t.TempDir()
	for name, body := range map[string]string{"index.html": "1", "old.js": "old"} {
		if err := os.WriteFile(filepath.Join(v1, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.PutTree(ctx, KindFrontend, "com.paca.x", v1); err != nil {
		t.Fatal(err)
	}

	// Upgrade with a bundle that has an unreadable (dangling) source file:
	// the walk skips non-regular files, so make the upload itself fail by
	// pointing the store at a failing client.
	v2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(v2, "index.html"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	failing := &failingPutS3{S3API: fake}
	bad := &s3Store{cfg: st.(*s3Store).cfg, s3: failing}
	if err := bad.PutTree(ctx, KindFrontend, "com.paca.x", v2); err == nil {
		t.Fatal("expected upload error")
	}
	if _, err := st.OpenAsset(ctx, KindFrontend, "com.paca.x", "old.js"); err != nil {
		t.Errorf("failed upgrade must keep old files: %v", err)
	}

	// A successful upgrade prunes the stale file.
	if err := st.PutTree(ctx, KindFrontend, "com.paca.x", v2); err != nil {
		t.Fatal(err)
	}
	if _, err := st.OpenAsset(ctx, KindFrontend, "com.paca.x", "old.js"); !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("stale file survived upgrade: %v", err)
	}
}

type failingPutS3 struct{ S3API }

func (f *failingPutS3) PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return nil, errors.New("boom")
}

func TestStore_S3_ListMigrationsIgnoresNested(t *testing.T) {
	ctx := context.Background()
	st, fake := newS3TestStore(t)
	fake.objects["plugins/com.paca.x/migrations/0001_a.sql"] = []byte("A")
	fake.objects["plugins/com.paca.x/migrations/sub/0002_b.sql"] = []byte("B")
	got, err := st.ListMigrations(ctx, "com.paca.x")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "0001_a.sql" {
		t.Errorf("migrations = %+v", got)
	}
}

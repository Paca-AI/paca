package plugin

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/platform/cache"
)

const cachePluginName = "test.cache"

var (
	cacheWasmOnce sync.Once
	cacheWasmPath string
	cacheWasmErr  error
)

// buildCacheFixture compiles testdata/cacheplugin the same way
// buildPoisonFixture compiles testdata/poisonplugin — see that function's
// doc comment for why this happens on demand rather than committing a
// prebuilt binary.
func buildCacheFixture(t *testing.T) string {
	t.Helper()
	cacheWasmOnce.Do(func() {
		dir, err := os.MkdirTemp("", "cacheplugin-*")
		if err != nil {
			cacheWasmErr = err
			return
		}
		wd, err := os.Getwd()
		if err != nil {
			cacheWasmErr = err
			return
		}
		out := filepath.Join(dir, "cache.wasm")
		cmd := exec.CommandContext(t.Context(), "go", "build", "-buildmode=c-shared", "-o", out, "./testdata/cacheplugin")
		cmd.Dir = wd
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
		if output, buildErr := cmd.CombinedOutput(); buildErr != nil {
			cacheWasmErr = fmt.Errorf("build cache fixture: %w: %s", buildErr, output)
			return
		}
		cacheWasmPath = out
	})
	if cacheWasmErr != nil {
		t.Fatalf("build cache fixture: %v", cacheWasmErr)
	}
	return cacheWasmPath
}

// loadCachePlugin compiles and loads the cache fixture into a fresh Runtime
// wired to the given HostServices (in particular, HostServices.Cache).
func loadCachePlugin(t *testing.T, services HostServices) *Runtime {
	t.Helper()

	wasmPath := buildCacheFixture(t)
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, cachePluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir fixture plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "backend.wasm"), wasmBytes, 0o644); err != nil {
		t.Fatalf("write fixture wasm: %v", err)
	}

	store := &Store{cfg: StoreConfig{Store: "local", WASMDir: dir}}
	rt := NewRuntime(store, services, DefaultResourceLimits(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	p := plugindom.Plugin{
		Name:    cachePluginName,
		Enabled: true,
		Manifest: plugindom.PluginManifest{
			Backend: &plugindom.BackendManifest{},
		},
	}
	ctx := context.Background()
	if err := rt.Load(ctx, p); err != nil {
		t.Fatalf("load fixture plugin: %v", err)
	}
	t.Cleanup(func() { rt.Unload(ctx, cachePluginName) })
	return rt
}

// callCacheSet writes key/value into the fixture's own linear memory (via
// its malloc export) and calls its CacheSet export, which forwards to
// paca.cache_set.
func callCacheSet(t *testing.T, inst *pluginInstance, key, value string, ttl time.Duration) int64 {
	t.Helper()
	ctx := context.Background()
	keyPtrLen, err := writeToMemory(inst.mod, []byte(key))
	if err != nil {
		t.Fatalf("write key: %v", err)
	}
	valPtrLen, err := writeToMemory(inst.mod, []byte(value))
	if err != nil {
		t.Fatalf("write value: %v", err)
	}
	fn := inst.mod.ExportedFunction("CacheSet")
	results, err := fn.Call(ctx, keyPtrLen[0], keyPtrLen[1], valPtrLen[0], valPtrLen[1], uint64(int32(ttl/time.Second)))
	if err != nil {
		t.Fatalf("call CacheSet: %v", err)
	}
	resetFixture(t, inst)
	return int64(int32(results[0]))
}

// callCacheGet writes key into the fixture's memory, calls its CacheGet
// export, and reads back the resulting value (if any) from fixture memory.
func callCacheGet(t *testing.T, inst *pluginInstance, key string) (string, bool) {
	t.Helper()
	ctx := context.Background()
	keyPtrLen, err := writeToMemory(inst.mod, []byte(key))
	if err != nil {
		t.Fatalf("write key: %v", err)
	}
	fn := inst.mod.ExportedFunction("CacheGet")
	results, err := fn.Call(ctx, keyPtrLen[0], keyPtrLen[1])
	if err != nil {
		t.Fatalf("call CacheGet: %v", err)
	}
	combined := results[0]
	valPtr := uint32(combined >> 32)
	valLen := uint32(combined & 0xFFFFFFFF)
	defer resetFixture(t, inst)
	if valLen == 0 {
		return "", false
	}
	data, err := readFromMemory(inst.mod, uint64(valPtr), uint64(valLen))
	if err != nil {
		t.Fatalf("read value: %v", err)
	}
	return string(data), true
}

// callCacheDelete writes key into the fixture's memory and calls its
// CacheDelete export.
func callCacheDelete(t *testing.T, inst *pluginInstance, key string) int64 {
	t.Helper()
	ctx := context.Background()
	keyPtrLen, err := writeToMemory(inst.mod, []byte(key))
	if err != nil {
		t.Fatalf("write key: %v", err)
	}
	fn := inst.mod.ExportedFunction("CacheDelete")
	results, err := fn.Call(ctx, keyPtrLen[0], keyPtrLen[1])
	if err != nil {
		t.Fatalf("call CacheDelete: %v", err)
	}
	resetFixture(t, inst)
	return int64(int32(results[0]))
}

func resetFixture(t *testing.T, inst *pluginInstance) {
	t.Helper()
	fn := inst.mod.ExportedFunction("ResetAllocator")
	if fn == nil {
		return
	}
	if _, err := fn.Call(context.Background()); err != nil {
		t.Fatalf("reset allocator: %v", err)
	}
}

// TestCacheHostFunctions exercises the full WASM<->host round trip for
// paca.cache_get/cache_set/cache_delete against a real (miniredis-backed)
// cache.Store — not just the SDK's in-memory test double — to prove the new
// host functions actually reach Valkey/Redis with the right key/value/TTL.
//
// All subtests share one loadCachePlugin call (one wazero.CompileModule)
// instead of loading a fresh Runtime per scenario: compiling this fixture
// under `go test -race` costs ~7s each time (wazero's optimizing backend is
// expensive under the race detector), and this package's other WASM-fixture
// tests (testdata/poisonplugin) already spend ~30s of the CI job's 60s
// budget — five separate loads here previously pushed the whole package
// over that budget and timed the CI job out. Each data-bearing subtest uses
// its own key so they stay independent without needing a shared store reset
// between them. The nil-Cache subtest reuses the very same Runtime/instance
// by temporarily clearing rt.services.Cache — Runtime.services is read live
// by the registered host-function closures (see registerCacheFunctions), so
// this in-package field mutation takes effect without any extra compile.
func TestCacheHostFunctions(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := cache.NewStore(client, "paca:")

	rt := loadCachePlugin(t, HostServices{Cache: store})
	inst := instanceFor(rt, cachePluginName)

	t.Run("SetThenGet", func(t *testing.T) {
		if ok := callCacheSet(t, inst, "greeting", "hello", time.Minute); ok != 1 {
			t.Fatalf("CacheSet: expected success, got %d", ok)
		}

		got, hit := callCacheGet(t, inst, "greeting")
		if !hit || got != "hello" {
			t.Fatalf("CacheGet: expected hit with %q, got hit=%v value=%q", "hello", hit, got)
		}

		// The value must actually be namespaced under this plugin's own
		// prefix in the shared store, not some other key — read it back
		// directly via cache.Store using the same prefix
		// registerCacheFunctions applies.
		var direct string
		hit, err := store.Get(context.Background(), cacheKeyPrefix(cachePluginName)+"greeting", &direct)
		if err != nil || !hit || direct != "hello" {
			t.Fatalf("expected cache.Store to hold the namespaced key directly, hit=%v err=%v value=%q", hit, err, direct)
		}
	})

	t.Run("GetMiss", func(t *testing.T) {
		if _, hit := callCacheGet(t, inst, "never-set"); hit {
			t.Fatal("expected a miss for a key that was never set")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		callCacheSet(t, inst, "to-delete", "v", time.Minute)
		if ok := callCacheDelete(t, inst, "to-delete"); ok != 1 {
			t.Fatalf("CacheDelete: expected success, got %d", ok)
		}
		if _, hit := callCacheGet(t, inst, "to-delete"); hit {
			t.Fatal("expected key to be gone after CacheDelete")
		}
	})

	// TTLExpiry proves the ttlSeconds argument actually reaches Redis's
	// EXPIRE mechanism (via cache.Store.Set), using miniredis's FastForward
	// instead of a real sleep. FastForward advances the shared miniredis
	// clock for every key, but this subtest's own key is the only one with
	// a TTL short enough to be affected by a 10s jump (SetThenGet/Delete
	// above used a 1-minute TTL).
	t.Run("TTLExpiry", func(t *testing.T) {
		callCacheSet(t, inst, "expiring", "v", 5*time.Second)
		if _, hit := callCacheGet(t, inst, "expiring"); !hit {
			t.Fatal("expected key to be present before its TTL elapses")
		}

		mr.FastForward(10 * time.Second)
		if _, hit := callCacheGet(t, inst, "expiring"); hit {
			t.Fatal("expected key to be gone after its TTL elapses")
		}
	})

	// NilCacheDegradesGracefully ensures a Runtime with no Cache configured
	// (HostServices.Cache == nil) reports misses and failed writes/deletes
	// instead of panicking — a plugin whose Cache is unavailable should fall
	// back to "always recompute", not crash. Run last and restore the real
	// store afterward in case tests are ever reordered.
	t.Run("NilCacheDegradesGracefully", func(t *testing.T) {
		rt.services.Cache = nil
		t.Cleanup(func() { rt.services.Cache = store })

		if ok := callCacheSet(t, inst, "k", "v", time.Minute); ok != 0 {
			t.Fatalf("expected CacheSet to report failure with no Cache configured, got %d", ok)
		}
		if _, hit := callCacheGet(t, inst, "k"); hit {
			t.Fatal("expected CacheGet to miss with no Cache configured")
		}
		if ok := callCacheDelete(t, inst, "k"); ok != 0 {
			t.Fatalf("expected CacheDelete to report failure with no Cache configured, got %d", ok)
		}
	})

	// NegativeTTLRejected ensures cache_set refuses a negative ttlSeconds
	// (which Redis would otherwise reject as an invalid expire time) up
	// front, rather than forwarding it and surfacing a generic store error.
	t.Run("NegativeTTLRejected", func(t *testing.T) {
		if ok := callCacheSet(t, inst, "negative-ttl", "v", -1*time.Second); ok != 0 {
			t.Fatalf("expected CacheSet to reject a negative TTL, got %d", ok)
		}
		if _, hit := callCacheGet(t, inst, "negative-ttl"); hit {
			t.Fatal("expected no value to have been stored for a rejected negative-TTL set")
		}
	})
}

// TestCacheKeyPrefix_DifferentPluginsDoNotCollide pins the namespacing
// contract: two plugins setting the same logical key must land at different
// Redis keys.
func TestCacheKeyPrefix_DifferentPluginsDoNotCollide(t *testing.T) {
	if cacheKeyPrefix("com.paca.a") == cacheKeyPrefix("com.paca.b") {
		t.Fatal("expected different plugins to get different cache key prefixes")
	}
	if got, want := cacheKeyPrefix("com.paca.dashboard")+"k", "plugin:18:com.paca.dashboard:k"; got != want {
		t.Fatalf("cacheKeyPrefix: got %q, want %q", got, want)
	}
}

// TestCacheKeyPrefix_ColonInPluginNameDoesNotCollide guards against the
// namespacing scheme regressing to a bare "plugin:<name>:" concatenation,
// under which a plugin named "a:b" using key "x" would land at the same
// Redis key as a plugin named "a" using key "b:x". The length-prefixed
// encoding in cacheKeyPrefix makes name/key boundaries unambiguous
// regardless of what characters the plugin name contains.
func TestCacheKeyPrefix_ColonInPluginNameDoesNotCollide(t *testing.T) {
	a := cacheKeyPrefix("a:b") + "x"
	b := cacheKeyPrefix("a") + "b:x"
	if a == b {
		t.Fatalf("expected no collision, but both encode to %q", a)
	}
}

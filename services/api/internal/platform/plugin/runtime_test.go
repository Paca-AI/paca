package plugin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

const testPluginName = "test.poison"

var (
	poisonWasmOnce sync.Once
	poisonWasmPath string
	poisonWasmErr  error
)

// buildPoisonFixture compiles testdata/poisonplugin into a WASI-reactor wasm
// binary the first time it's needed, then reuses the result for the rest of
// the test binary's run. The compiled binary isn't committed to the repo
// (the project's .gitignore excludes *.wasm everywhere), so it's built here
// on demand with the standard Go toolchain: GOOS=wasip1 GOARCH=wasm
// cross-compilation needs nothing beyond the Go distribution already
// required to run these tests.
func buildPoisonFixture(t *testing.T) string {
	t.Helper()
	poisonWasmOnce.Do(func() {
		dir, err := os.MkdirTemp("", "poisonplugin-*")
		if err != nil {
			poisonWasmErr = err
			return
		}
		wd, err := os.Getwd()
		if err != nil {
			poisonWasmErr = err
			return
		}
		out := filepath.Join(dir, "poison.wasm")
		cmd := exec.CommandContext(t.Context(), "go", "build", "-buildmode=c-shared", "-o", out, "./testdata/poisonplugin")
		cmd.Dir = wd
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
		if output, buildErr := cmd.CombinedOutput(); buildErr != nil {
			poisonWasmErr = fmt.Errorf("build poison fixture: %w: %s", buildErr, output)
			return
		}
		poisonWasmPath = out
	})
	if poisonWasmErr != nil {
		t.Fatalf("build poison fixture: %v", poisonWasmErr)
	}
	return poisonWasmPath
}

// loadPoisonPlugin compiles (see buildPoisonFixture) and loads the poison
// fixture into a fresh Runtime. The fixture's malloc export is an
// intentionally unsafe bump allocator -- it advances its cursor with no
// bounds check, the same shape a real plugin SDK allocator has -- so tests
// can deterministically trigger the out-of-bounds write that used to poison
// plugin instances.
func loadPoisonPlugin(t *testing.T, limits ResourceLimits) *Runtime {
	t.Helper()
	return loadPoisonPluginWithLogger(t, limits, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// loadPoisonPluginWithLogger is loadPoisonPlugin with an injectable logger, so
// tests can assert on log output (e.g. the dispatchEvent size-limit warning)
// without scraping stderr.
func loadPoisonPluginWithLogger(t *testing.T, limits ResourceLimits, log *slog.Logger) *Runtime {
	t.Helper()

	wasmPath := buildPoisonFixture(t)
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, testPluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir fixture plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "backend.wasm"), wasmBytes, 0o644); err != nil {
		t.Fatalf("write fixture wasm: %v", err)
	}

	store := &Store{cfg: StoreConfig{Store: "local", WASMDir: dir}}
	rt := NewRuntime(store, HostServices{}, limits, log)

	p := plugindom.Plugin{
		Name:    testPluginName,
		Enabled: true,
		Manifest: plugindom.PluginManifest{
			Backend: &plugindom.BackendManifest{},
		},
	}
	ctx := context.Background()
	if err := rt.Load(ctx, p); err != nil {
		t.Fatalf("load fixture plugin: %v", err)
	}
	t.Cleanup(func() { rt.Unload(ctx, testPluginName) })
	return rt
}

// instanceFor looks up the loaded poison-plugin instance directly, for tests
// that need to call dispatchEvent (an unexported method) without going
// through EmitEvent's topic-subscription filtering.
func instanceFor(rt *Runtime, name string) *pluginInstance {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.plugins[name]
}

// TestHandleRequest_OversizedPayload_RejectedWithoutTouchingPlugin pins the
// fast path: when a payload exceeds ResourceLimits.MaxRequestBodyBytes,
// HandleRequest must reject it before ever calling the plugin's malloc
// export. A normal request right after must still succeed, proving the
// rejected call left no trace in the plugin's allocator state.
func TestHandleRequest_OversizedPayload_RejectedWithoutTouchingPlugin(t *testing.T) {
	limits := DefaultResourceLimits()
	limits.MaxRequestBodyBytes = 1024 // far smaller than the fixture's actual memory
	rt := loadPoisonPlugin(t, limits)
	ctx := context.Background()

	oversized := make([]byte, 2048)
	if _, err := rt.HandleRequest(ctx, testPluginName, oversized); err == nil {
		t.Fatal("expected oversized request to be rejected")
	}

	if _, err := rt.HandleRequest(ctx, testPluginName, []byte("hello")); err != nil {
		t.Fatalf("expected normal request to succeed after rejection, got: %v", err)
	}
}

// TestHandleRequest_AllocatorFailure_RecoveredByReset reproduces the
// reported bug directly: with the pre-check disabled, an oversized payload
// reaches wazero's own memory-bounds check inside writeToMemory. Before the
// fix, HandleRequest returned that error without ever calling
// ResetAllocator, so the plugin's bump-allocator cursor stayed corrupted and
// every later call -- including tiny, well-formed ones -- failed the same
// way forever. This test fails on the old code and passes on the fix.
func TestHandleRequest_AllocatorFailure_RecoveredByReset(t *testing.T) {
	limits := DefaultResourceLimits()
	limits.MaxRequestBodyBytes = 0 // disabled, so the write reaches wazero's bounds check
	rt := loadPoisonPlugin(t, limits)
	ctx := context.Background()

	huge := make([]byte, 200*1024*1024) // far beyond the module's actual linear memory
	if _, err := rt.HandleRequest(ctx, testPluginName, huge); err == nil {
		t.Fatal("expected the oversized write to fail at the wazero memory-bounds check")
	}

	for i := 0; i < 3; i++ {
		if _, err := rt.HandleRequest(ctx, testPluginName, []byte("hi")); err != nil {
			t.Fatalf("call %d after the oversized request failed -- instance still poisoned: %v", i, err)
		}
	}
}

// TestDispatchEvent_OversizedPayload_RejectedWithoutTouchingPlugin mirrors
// TestHandleRequest_OversizedPayload_RejectedWithoutTouchingPlugin for the
// event-dispatch path: dispatchEvent must reject a payload over
// ResourceLimits.MaxRequestBodyBytes before ever calling the plugin's malloc
// export, rather than relying solely on the unconditional allocator reset to
// recover afterward. A normal call right after must still succeed.
func TestDispatchEvent_OversizedPayload_RejectedWithoutTouchingPlugin(t *testing.T) {
	limits := DefaultResourceLimits()
	limits.MaxRequestBodyBytes = 1024 // far smaller than the fixture's actual memory

	var logBuf bytes.Buffer
	rt := loadPoisonPluginWithLogger(t, limits, slog.New(slog.NewTextHandler(&logBuf, nil)))
	inst := instanceFor(rt, testPluginName)
	ctx := context.Background()

	oversized := make([]byte, 2048)
	rt.dispatchEvent(ctx, inst, "some.topic", oversized)

	if !strings.Contains(logBuf.String(), "exceeds size limit") {
		t.Fatalf("expected a size-limit warning to be logged, got: %s", logBuf.String())
	}
	if strings.Contains(logBuf.String(), "write event payload") {
		t.Fatalf("dispatchEvent should reject before ever attempting to write into plugin memory, got: %s", logBuf.String())
	}

	if _, err := rt.HandleRequest(ctx, testPluginName, []byte("hello")); err != nil {
		t.Fatalf("expected normal request to succeed after oversized event, got: %v", err)
	}
}

// callerPlugin builds a plugindom.Plugin for use as execQuery/execStatement's
// caller argument, with an optional RequestedSensitiveFields declaration.
func callerPlugin(name string, requested ...string) plugindom.Plugin {
	return plugindom.Plugin{
		Name: name,
		Manifest: plugindom.PluginManifest{
			Backend: &plugindom.BackendManifest{
				RequestedSensitiveFields: requested,
			},
		},
	}
}

// TestSensitiveColumnsForQuery_ForeignPluginSchema pins the detection logic
// for cross-plugin schema access: a caller's SQL text referencing another
// loaded plugin's schema (by its distinctive plugin_data_<name> name) should
// pull in that plugin's declared sensitive columns for the specific table
// referenced, while SQL that only touches the caller's own schema, an
// unrelated table in that schema, or no foreign schema at all should not.
func TestSensitiveColumnsForQuery_ForeignPluginSchema(t *testing.T) {
	rt := &Runtime{
		plugins: map[string]*pluginInstance{
			"com.paca.a": {plugin: plugindom.Plugin{
				Name: "com.paca.a",
				Manifest: plugindom.PluginManifest{
					Backend: &plugindom.BackendManifest{},
				},
			}},
			"com.paca.b": {plugin: plugindom.Plugin{
				Name: "com.paca.b",
				Manifest: plugindom.PluginManifest{
					Backend: &plugindom.BackendManifest{
						SensitiveFields: map[string][]string{
							"customers": {"Email", "phone"},
							"orders":    {"internal_notes"},
						},
					},
				},
			}},
		},
	}

	t.Run("references foreign schema unquoted", func(t *testing.T) {
		sql := "SELECT * FROM plugin_data_com_paca_b.customers"
		got := rt.sensitiveColumnsForQuery(callerPlugin("com.paca.a"), sql)
		want := map[string]struct{}{"email": {}, "phone": {}}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for k := range want {
			if _, ok := got[k]; !ok {
				t.Fatalf("missing column %q in %v", k, got)
			}
		}
	})

	t.Run("references foreign schema quoted", func(t *testing.T) {
		sql := `SELECT * FROM "plugin_data_com_paca_b".customers`
		got := rt.sensitiveColumnsForQuery(callerPlugin("com.paca.a"), sql)
		if _, ok := got["email"]; !ok {
			t.Fatalf("expected quoted schema reference to be detected, got %v", got)
		}
	})

	t.Run("only pulls columns for the table actually referenced", func(t *testing.T) {
		sql := "SELECT * FROM plugin_data_com_paca_b.orders"
		got := rt.sensitiveColumnsForQuery(callerPlugin("com.paca.a"), sql)
		if _, ok := got["internal_notes"]; !ok {
			t.Fatalf("expected orders.internal_notes redacted, got %v", got)
		}
		if _, ok := got["email"]; ok {
			t.Fatalf("customers.email should not be pulled in when only orders is referenced, got %v", got)
		}
	})

	t.Run("caller's own schema is never redacted", func(t *testing.T) {
		sql := "SELECT * FROM plugin_data_com_paca_b.customers"
		got := rt.sensitiveColumnsForQuery(callerPlugin("com.paca.b"), sql)
		if len(got) != 0 {
			t.Fatalf("expected no redaction when the owning plugin queries its own schema, got %v", got)
		}
	})

	t.Run("query touching only own schema or public", func(t *testing.T) {
		sql := "SELECT * FROM my_table JOIN public.tasks ON true"
		got := rt.sensitiveColumnsForQuery(callerPlugin("com.paca.a"), sql)
		if len(got) != 0 {
			t.Fatalf("expected no redaction for a query with no foreign schema reference, got %v", got)
		}
	})

	t.Run("requested access exempts specific columns only", func(t *testing.T) {
		sql := "SELECT * FROM plugin_data_com_paca_b.customers"
		caller := callerPlugin("com.paca.a", "com.paca.b:customers.email")
		got := rt.sensitiveColumnsForQuery(caller, sql)
		if _, ok := got["email"]; ok {
			t.Fatalf("expected email exempted by RequestedSensitiveFields, got %v", got)
		}
		if _, ok := got["phone"]; !ok {
			t.Fatalf("expected phone to remain redacted (not requested), got %v", got)
		}
	})
}

// TestSensitiveColumnsForQuery_CoreTable pins core-table protection: an
// unqualified reference to a core sensitive table (reachable via every
// plugin's search_path through `public`) is redacted for every plugin
// unless it explicitly requests that field.
func TestSensitiveColumnsForQuery_CoreTable(t *testing.T) {
	rt := &Runtime{plugins: map[string]*pluginInstance{}}

	t.Run("unqualified core table is redacted by default", func(t *testing.T) {
		sql := "SELECT id, password_hash FROM users WHERE id = $1"
		got := rt.sensitiveColumnsForQuery(callerPlugin("com.paca.dashboard"), sql)
		if _, ok := got["password_hash"]; !ok {
			t.Fatalf("expected users.password_hash redacted, got %v", got)
		}
	})

	t.Run("explicit public-qualified core table is redacted", func(t *testing.T) {
		sql := "SELECT encrypted_value FROM public.agent_environment_variables WHERE agent_id = $1"
		got := rt.sensitiveColumnsForQuery(callerPlugin("com.paca.other"), sql)
		if _, ok := got["encrypted_value"]; !ok {
			t.Fatalf("expected agent_environment_variables.encrypted_value redacted by default, got %v", got)
		}
	})

	t.Run("requested access exempts a core field for the requesting plugin", func(t *testing.T) {
		sql := "SELECT encrypted_value FROM agent_environment_variables WHERE agent_id = $1"
		caller := callerPlugin("com.paca.admin-tools", "agent_environment_variables.encrypted_value")
		got := rt.sensitiveColumnsForQuery(caller, sql)
		if len(got) != 0 {
			t.Fatalf("expected no redaction once requested, got %v", got)
		}
	})

	t.Run("non-sensitive core table is untouched", func(t *testing.T) {
		sql := "SELECT id, title FROM tasks WHERE project_id = $1"
		got := rt.sensitiveColumnsForQuery(callerPlugin("com.paca.dashboard"), sql)
		if len(got) != 0 {
			t.Fatalf("expected no redaction for a table with no declared sensitive columns, got %v", got)
		}
	})
}

// TestCheckWriteAllowed pins write-side protection: INSERT/UPDATE/DELETE
// against a sensitive table (core or another plugin's) is rejected outright
// unless caller has requested access to it, while writes to the caller's
// own schema or to tables with no declared sensitive columns are untouched.
func TestCheckWriteAllowed(t *testing.T) {
	rt := &Runtime{
		plugins: map[string]*pluginInstance{
			"com.paca.b": {plugin: plugindom.Plugin{
				Name: "com.paca.b",
				Manifest: plugindom.PluginManifest{
					Backend: &plugindom.BackendManifest{
						SensitiveFields: map[string][]string{
							"customers": {"email"},
						},
					},
				},
			}},
		},
	}

	cases := []struct {
		name      string
		caller    plugindom.Plugin
		sql       string
		wantError bool
	}{
		{"insert into core users table blocked", callerPlugin("com.paca.a"), "INSERT INTO users (username, password_hash) VALUES ($1, $2)", true},
		{"update core users table blocked", callerPlugin("com.paca.a"), "UPDATE users SET password_hash = $1 WHERE id = $2", true},
		{"delete from core users table blocked", callerPlugin("com.paca.a"), "DELETE FROM users WHERE id = $1", true},
		{"requested access allows the write", callerPlugin("com.paca.admin-tools", "agent_environment_variables.encrypted_value"), "INSERT INTO agent_environment_variables (agent_id, key, encrypted_value) VALUES ($1, $2, $3)", false},
		{"write to foreign plugin's sensitive table blocked", callerPlugin("com.paca.a"), "UPDATE plugin_data_com_paca_b.customers SET email = $1 WHERE id = $2", true},
		{"owner writing its own schema is unaffected", callerPlugin("com.paca.b"), "UPDATE plugin_data_com_paca_b.customers SET email = $1 WHERE id = $2", false},
		{"write to own schema table is unaffected", callerPlugin("com.paca.a"), "INSERT INTO my_table (col) VALUES ($1)", false},
		{"write to non-sensitive core table is unaffected", callerPlugin("com.paca.a"), "UPDATE tasks SET title = $1 WHERE id = $2", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := rt.checkWriteAllowed(tc.caller, tc.sql, "paca.db_exec")
			if tc.wantError && err == nil {
				t.Fatalf("expected write to be blocked, got no error")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("expected write to be allowed, got error: %v", err)
			}
		})
	}
}

// TestRedactColumns replaces the declared sensitive columns' values with
// "***" across every row, matching column names case-insensitively, and
// leaves everything else untouched.
func TestRedactColumns(t *testing.T) {
	cols := []string{"id", "Email", "title"}
	rows := [][]any{
		{1, "a@example.com", "first"},
		{2, "b@example.com", "second"},
	}
	sensitive := map[string]struct{}{"email": {}}

	redactColumns(cols, rows, sensitive)

	for i, row := range rows {
		if row[1] != "***" {
			t.Fatalf("row %d: expected email column redacted, got %v", i, row[1])
		}
	}
	if rows[0][0] != 1 || rows[0][2] != "first" {
		t.Fatalf("expected non-sensitive columns untouched, got %v", rows[0])
	}
	if rows[1][0] != 2 || rows[1][2] != "second" {
		t.Fatalf("expected non-sensitive columns untouched, got %v", rows[1])
	}
}

// TestRedactColumns_NoMatch is a no-op when no column name matches.
func TestRedactColumns_NoMatch(t *testing.T) {
	cols := []string{"id", "title"}
	rows := [][]any{{1, "first"}}
	redactColumns(cols, rows, map[string]struct{}{"email": {}})
	if rows[0][0] != 1 || rows[0][1] != "first" {
		t.Fatalf("expected rows untouched when no column matches, got %v", rows[0])
	}
}

// TestHandleRequest_Concurrent_DoesNotCorruptSharedInstance pins the
// lock-ordering fix: writeToMemory (which calls the plugin's malloc export)
// must happen while holding the per-instance lock, not before acquiring it,
// since wazero module calls are not safe to interleave. Run with -race.
func TestHandleRequest_Concurrent_DoesNotCorruptSharedInstance(t *testing.T) {
	rt := loadPoisonPlugin(t, DefaultResourceLimits())
	ctx := context.Background()

	const workers = 20
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := rt.HandleRequest(ctx, testPluginName, []byte("concurrent"))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent HandleRequest failed: %v", err)
		}
	}
}

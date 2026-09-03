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

const legacyMallocPluginName = "test.legacymalloc"

var (
	legacyMallocWasmOnce sync.Once
	legacyMallocWasmPath string
	legacyMallocWasmErr  error
)

// buildLegacyMallocFixture compiles testdata/legacymallocplugin, which
// exports only "malloc" -- never "paca_malloc" -- the same shape as every
// plugin binary built against the plugin-sdk-go version before the
// allocator export was renamed.
func buildLegacyMallocFixture(t *testing.T) string {
	t.Helper()
	legacyMallocWasmOnce.Do(func() {
		dir, err := os.MkdirTemp("", "legacymallocplugin-*")
		if err != nil {
			legacyMallocWasmErr = err
			return
		}
		wd, err := os.Getwd()
		if err != nil {
			legacyMallocWasmErr = err
			return
		}
		out := filepath.Join(dir, "legacymalloc.wasm")
		cmd := exec.CommandContext(t.Context(), "go", "build", "-buildmode=c-shared", "-o", out, "./testdata/legacymallocplugin")
		cmd.Dir = wd
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
		if output, buildErr := cmd.CombinedOutput(); buildErr != nil {
			legacyMallocWasmErr = fmt.Errorf("build legacymalloc fixture: %w: %s", buildErr, output)
			return
		}
		legacyMallocWasmPath = out
	})
	if legacyMallocWasmErr != nil {
		t.Fatalf("build legacymalloc fixture: %v", legacyMallocWasmErr)
	}
	return legacyMallocWasmPath
}

// loadLegacyMallocPlugin compiles (see buildLegacyMallocFixture) and loads
// the legacymalloc fixture into a fresh Runtime.
func loadLegacyMallocPlugin(t *testing.T, limits ResourceLimits) *Runtime {
	t.Helper()

	wasmPath := buildLegacyMallocFixture(t)
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, legacyMallocPluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir fixture plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "backend.wasm"), wasmBytes, 0o644); err != nil {
		t.Fatalf("write fixture wasm: %v", err)
	}

	store := &Store{cfg: StoreConfig{Store: "local", WASMDir: dir}}
	rt := NewRuntime(store, HostServices{}, limits, slog.New(slog.NewTextHandler(io.Discard, nil)))

	p := plugindom.Plugin{
		Name:    legacyMallocPluginName,
		Enabled: true,
		Manifest: plugindom.PluginManifest{
			Backend: &plugindom.BackendManifest{},
		},
	}
	ctx := context.Background()
	if err := rt.Load(ctx, p); err != nil {
		t.Fatalf("load fixture plugin: %v", err)
	}
	t.Cleanup(func() { rt.Unload(ctx, legacyMallocPluginName) })
	return rt
}

// TestHandleRequest_LegacyMallocExport_FallbackStillWorks pins
// writeToMemory's backward-compatibility fallback: a plugin binary compiled
// against the pre-rename SDK exports only "malloc", never "paca_malloc".
// The host must still be able to write the request payload into such a
// plugin's memory and call it successfully -- if the fallback ever
// regresses, every already-deployed plugin of this shape breaks the moment
// the host redeploys, silently, since callers of writeToMemory often ignore
// its error.
func TestHandleRequest_LegacyMallocExport_FallbackStillWorks(t *testing.T) {
	rt := loadLegacyMallocPlugin(t, DefaultResourceLimits())
	ctx := context.Background()

	if _, err := rt.HandleRequest(ctx, legacyMallocPluginName, []byte("hello")); err != nil {
		t.Fatalf("expected request to a legacy-malloc-only plugin to succeed via the fallback, got: %v", err)
	}
}

// TestMaxHTTPBodyBytes covers the three cases callers of MaxHTTPBodyBytes
// must distinguish: the limit disabled entirely (0, "no limit"), the known
// envelope overhead alone already exceeding the limit (-1, "reject
// outright, not even an empty body fits"), and the normal case where the
// base64 expansion factor is applied to whatever room remains after
// subtracting the caller-measured overhead.
func TestMaxHTTPBodyBytes(t *testing.T) {
	tests := []struct {
		name     string
		maxBytes int64
		overhead int64
		want     int64
	}{
		{name: "disabled limit means no limit", maxBytes: 0, overhead: 500, want: 0},
		{name: "negative limit also means no limit", maxBytes: -1, overhead: 500, want: 0},
		{name: "overhead equal to limit leaves no room", maxBytes: 1000, overhead: 1000, want: -1},
		{name: "overhead exceeding limit leaves no room", maxBytes: 1000, overhead: 1500, want: -1},
		{name: "remaining room is reduced by the base64 factor", maxBytes: 1000, overhead: 200, want: 600}, // (1000-200)*3/4
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &Runtime{limits: ResourceLimits{MaxRequestBodyBytes: tt.maxBytes}}
			if got := rt.MaxHTTPBodyBytes(tt.overhead); got != tt.want {
				t.Fatalf("MaxHTTPBodyBytes(%d) with MaxRequestBodyBytes=%d = %d, want %d", tt.overhead, tt.maxBytes, got, tt.want)
			}
		})
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

// TestSensitiveColumnsForQuery_CommentBypass pins comment-stripping: since
// PostgreSQL treats a comment as equivalent to whitespace during
// tokenization, "FROM/*x*/table" parses identically to "FROM table" and
// must not be able to slip past the FROM/JOIN detection regex just because
// there's no literal whitespace character between the keyword and the
// table name.
func TestSensitiveColumnsForQuery_CommentBypass(t *testing.T) {
	rt := &Runtime{plugins: map[string]*pluginInstance{}}

	t.Run("block comment between FROM and table name is still detected", func(t *testing.T) {
		sql := "SELECT * FROM/*x*/users"
		got := rt.sensitiveColumnsForQuery(callerPlugin("com.paca.dashboard"), sql)
		if _, ok := got["password_hash"]; !ok {
			t.Fatalf("expected users.password_hash redacted despite comment obfuscation, got %v", got)
		}
	})

	t.Run("line comment before FROM is still detected", func(t *testing.T) {
		sql := "SELECT * -- x\nFROM users"
		got := rt.sensitiveColumnsForQuery(callerPlugin("com.paca.dashboard"), sql)
		if _, ok := got["password_hash"]; !ok {
			t.Fatalf("expected users.password_hash redacted despite comment obfuscation, got %v", got)
		}
	})
}

// TestSensitiveColumnsForQuery_SyntaxVariants pins detection of standard
// Postgres syntax that the original pattern set missed: old-style
// comma-joins, the "TABLE name" shorthand, and result-column aliasing that
// would otherwise let the aliased name slip past redaction untouched.
func TestSensitiveColumnsForQuery_SyntaxVariants(t *testing.T) {
	rt := &Runtime{plugins: map[string]*pluginInstance{}}

	t.Run("comma-joined table beyond the first is detected", func(t *testing.T) {
		sql := "SELECT password_hash FROM tasks, users WHERE tasks.user_id = users.id"
		got := rt.sensitiveColumnsForQuery(callerPlugin("com.paca.dashboard"), sql)
		if _, ok := got["password_hash"]; !ok {
			t.Fatalf("expected users.password_hash redacted for a comma-joined table, got %v", got)
		}
	})

	t.Run("TABLE shorthand nested in a FROM clause is detected", func(t *testing.T) {
		sql := "SELECT password_hash FROM (TABLE users) u"
		got := rt.sensitiveColumnsForQuery(callerPlugin("com.paca.dashboard"), sql)
		if _, ok := got["password_hash"]; !ok {
			t.Fatalf("expected users.password_hash redacted via TABLE shorthand, got %v", got)
		}
	})

	t.Run("result column alias is redacted under its own name", func(t *testing.T) {
		sql := "SELECT password_hash AS ph FROM users"
		got := rt.sensitiveColumnsForQuery(callerPlugin("com.paca.dashboard"), sql)
		if _, ok := got["ph"]; !ok {
			t.Fatalf("expected alias %q to be redacted, got %v", "ph", got)
		}
	})

	t.Run("table-qualified column alias is redacted under its own name", func(t *testing.T) {
		sql := "SELECT u.password_hash AS ph FROM users u"
		got := rt.sensitiveColumnsForQuery(callerPlugin("com.paca.dashboard"), sql)
		if _, ok := got["ph"]; !ok {
			t.Fatalf("expected alias %q to be redacted, got %v", "ph", got)
		}
	})

	t.Run("unrelated columns do not spuriously gain aliases", func(t *testing.T) {
		sql := "SELECT id, username FROM users"
		got := rt.sensitiveColumnsForQuery(callerPlugin("com.paca.dashboard"), sql)
		if _, ok := got["id"]; ok {
			t.Fatalf("did not expect id redacted, got %v", got)
		}
		if _, ok := got["username"]; ok {
			t.Fatalf("did not expect username redacted, got %v", got)
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

		// Regression cases: a pattern-based guard is only as strong as what
		// it recognizes as a write. These previously bypassed detection
		// entirely (checkWriteAllowed silently allowed them).
		{"leading line comment before INSERT no longer bypasses detection", callerPlugin("com.paca.a"), "-- x\nINSERT INTO users (username, password_hash) VALUES ($1, $2)", true},
		{"block comment between INTO and table no longer bypasses detection", callerPlugin("com.paca.a"), "INSERT INTO/*x*/users (username, password_hash) VALUES ($1, $2)", true},
		{"INSERT...SELECT exfiltrating from a sensitive table is blocked", callerPlugin("com.paca.a"), "INSERT INTO my_table (stolen) SELECT password_hash FROM users", true},
		{"UPDATE...FROM exfiltrating from a sensitive table is blocked", callerPlugin("com.paca.a"), "UPDATE my_table SET note = u.password_hash FROM users u WHERE u.id = my_table.uid", true},
		{"DELETE...USING referencing a sensitive table is blocked", callerPlugin("com.paca.a"), "DELETE FROM my_table USING users WHERE my_table.uid = users.id", true},
		{"requesting one sensitive column does not exempt a table's other sensitive columns", callerPlugin("com.paca.a", "agents.llm_api_key_secret"), "UPDATE agents SET acp_bridge_token_hash = $1 WHERE id = $2", true},
		{"requesting every sensitive column of a table does exempt it", callerPlugin("com.paca.a", "agents.llm_api_key_secret", "agents.acp_bridge_token_hash"), "UPDATE agents SET acp_bridge_token_hash = $1 WHERE id = $2", false},
		{"DELETE FROM ONLY a sensitive table is blocked", callerPlugin("com.paca.a"), "DELETE FROM ONLY users WHERE id = $1", true},
		{"UPDATE ONLY a sensitive table is blocked", callerPlugin("com.paca.a"), "UPDATE ONLY users SET password_hash = $1 WHERE id = $2", true},
		{"comma-joined sensitive table beyond the first is blocked", callerPlugin("com.paca.a"), "INSERT INTO my_table (stolen) SELECT password_hash FROM tasks, users", true},
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

// TestCheckWriteAllowed_AlwaysBlockedTables pins the GHSA-g6mx-8g92-w9v5 fix:
// identity/authorization tables are rejected for every plugin write
// unconditionally, unlike coreSensitiveFields tables which a plugin can
// unlock by declaring RequestedSensitiveFields. Before this test's cases
// were enforced, a plugin with zero manifest declarations could run
// "UPDATE project_members SET project_role_id = <owner-role> WHERE
// user_id = <attacker>" (or the equivalent against global_roles,
// project_roles, or password_set_tokens) and grant itself admin on every
// project or every tenant on the instance — the escalation step of the
// advisory's PoC — and a plugin could unlock unrestricted writes to the
// entire users or api_keys table merely by declaring that table's one
// coreSensitiveFields column as requested.
func TestCheckWriteAllowed_AlwaysBlockedTables(t *testing.T) {
	rt := &Runtime{plugins: map[string]*pluginInstance{}}

	cases := []struct {
		name   string
		caller plugindom.Plugin
		sql    string
	}{
		{"project_members: grant self a project role, no manifest declarations", callerPlugin("com.paca.evil"), "UPDATE project_members SET project_role_id = $1 WHERE user_id = $2"},
		{"project_members: insert self as a member", callerPlugin("com.paca.evil"), "INSERT INTO project_members (project_id, user_id, project_role_id) VALUES ($1, $2, $3)"},
		{"project_roles: rewrite a role's permissions to superadmin", callerPlugin("com.paca.evil"), `UPDATE project_roles SET permissions = '{"*": true}' WHERE id = $1`},
		{"global_roles: rewrite the baseline USER role to superadmin", callerPlugin("com.paca.evil"), `UPDATE global_roles SET permissions = '{"*": true}' WHERE name = 'USER'`},
		{"users: escalate own global role_id, no manifest declarations", callerPlugin("com.paca.evil"), "UPDATE users SET role_id = $1 WHERE id = $2"},
		{"users: escalate own global role_id even with password_hash requested", callerPlugin("com.paca.evil", "users.password_hash"), "UPDATE users SET role_id = $1 WHERE id = $2"},
		{"api_keys: forge a key even with key_hash requested", callerPlugin("com.paca.evil", "api_keys.key_hash"), "INSERT INTO api_keys (user_id, key_hash) VALUES ($1, $2)"},
		{"password_set_tokens: plant a token to take over an account", callerPlugin("com.paca.evil"), "INSERT INTO password_set_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)"},
		{"explicit public schema qualification is blocked the same way", callerPlugin("com.paca.evil"), "UPDATE public.project_members SET project_role_id = $1 WHERE user_id = $2"},
		{"delete wiping every project membership is blocked", callerPlugin("com.paca.evil"), "DELETE FROM project_members WHERE true"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := rt.checkWriteAllowed(tc.caller, tc.sql, "paca.db_exec"); err == nil {
				t.Fatalf("expected write to be unconditionally blocked, got no error")
			}
		})
	}

	t.Run("a plugin's own identically-named table is unaffected", func(t *testing.T) {
		caller := callerPlugin("com.paca.evil")
		sql := "UPDATE " + schemaName(caller.Name) + ".users SET role_id = $1 WHERE id = $2"
		if err := rt.checkWriteAllowed(caller, sql, "paca.db_exec"); err != nil {
			t.Fatalf("expected write to the plugin's own schema-qualified table to be unaffected, got error: %v", err)
		}
	})
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

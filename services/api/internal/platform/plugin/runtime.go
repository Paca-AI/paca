package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/google/uuid"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// ResourceLimits controls per-plugin execution constraints.
type ResourceLimits struct {
	// MaxCallDuration is the maximum time allowed for a single plugin function
	// call.  Defaults to 5 seconds.
	MaxCallDuration time.Duration
	// MaxMemoryPages is the maximum number of 64-KiB WASM linear-memory pages
	// a plugin module may allocate.  0 means "use wazero default".
	MaxMemoryPages uint32
	// MaxRequestBodyBytes is the maximum size of a payload that the host will
	// attempt to write into a plugin's linear memory: an inbound HTTP request
	// body (as handed to HandleRequest, after JSON envelope encoding) or an
	// event payload (as handed to dispatchEvent).  Plugin allocators are
	// simple bump allocators with no bounds checking of their own; handing
	// them a payload larger than the module's memory advances their internal
	// cursor past the end of memory before the host's write fails, which
	// would otherwise leave the plugin instance permanently unable to serve
	// any further request.  0 means "no limit".
	MaxRequestBodyBytes int64
}

// DefaultResourceLimits returns conservative defaults for plugin execution.
func DefaultResourceLimits() ResourceLimits {
	return ResourceLimits{
		MaxCallDuration:     5 * time.Second,
		MaxMemoryPages:      1024,             // 64 MiB
		MaxRequestBodyBytes: 10 * 1024 * 1024, // 10 MiB
	}
}

// HostServices provides concrete implementations of the host-side services
// that the WASM host-function bridge delegates to.
type HostServices struct {
	// DB is the underlying *sql.DB for plugin-scoped queries.
	DB *sql.DB
	// Log is the structured logger for plugin-emitted log messages.
	Log *slog.Logger
	// Publisher exposes event emission to plugins.
	Publisher EventPublisher
	// Config contains host-side config values exposed to plugins via
	// paca.config_get when explicitly allowlisted in plugin manifest.
	Config map[string]string
	// HTTPClient is used by the paca.http_request host function.
	HTTPClient *http.Client
	// AllowedOutboundDomains is the allowlist for paca.http_request outbound
	// calls.  When empty, all outbound HTTP is blocked.
	AllowedOutboundDomains []string
	// Authorizer resolves effective permissions (built-in and plugin-declared
	// custom permissions alike, since both are stored in the same permission
	// map) for the paca.permission_check host function. May be nil, in which
	// case permission_check always returns false.
	Authorizer *authz.Authorizer
}

// EventPublisher abstracts the messaging.Publisher to avoid a circular import.
type EventPublisher interface {
	Publish(ctx context.Context, channel string, payload any) error
	Append(ctx context.Context, stream, eventType string, payload any) error
}

// pluginInstance wraps a compiled wazero module for a single installed plugin.
type pluginInstance struct {
	plugin plugindom.Plugin
	mod    api.Module
	rt     wazero.Runtime
	mu     sync.Mutex // serialises calls into the WASM module
}

// Runtime manages the lifecycle of all installed plugin WASM modules.
type Runtime struct {
	store    *Store
	limits   ResourceLimits
	services HostServices
	log      *slog.Logger

	mu      sync.RWMutex
	plugins map[string]*pluginInstance // keyed by plugin.Name
}

// Keep fetch response cap aligned with existing plugin artifact per-file limit.
const maxFetchResponseBodySize = 50 * 1024 * 1024 // 50 MiB

var allowedFetchMethods = map[string]struct{}{
	http.MethodGet:     {},
	http.MethodPost:    {},
	http.MethodPut:     {},
	http.MethodPatch:   {},
	http.MethodDelete:  {},
	http.MethodHead:    {},
	http.MethodOptions: {},
}

var disallowedFetchHeaders = map[string]struct{}{
	"connection":          {},
	"proxy-connection":    {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"host":                {},
	"content-length":      {},
}

// NewRuntime creates a Runtime wired to the given store and host services.
func NewRuntime(store *Store, services HostServices, limits ResourceLimits, log *slog.Logger) *Runtime {
	return &Runtime{
		store:    store,
		limits:   limits,
		services: services,
		log:      log,
		plugins:  make(map[string]*pluginInstance),
	}
}

// MaxRequestBodyBytes returns the configured request-payload size limit so
// HTTP transport code can reject oversized bodies before they ever reach a
// plugin's WASM memory.  0 means "no limit".
func (r *Runtime) MaxRequestBodyBytes() int64 {
	return r.limits.MaxRequestBodyBytes
}

// LoadAll instantiates wazero modules for every enabled plugin in the list.
// It is called once on startup after plugin records are loaded from the DB.
func (r *Runtime) LoadAll(ctx context.Context, plugins []*plugindom.Plugin) error {
	for _, p := range plugins {
		if !p.Enabled {
			continue
		}
		if err := r.Load(ctx, *p); err != nil {
			r.log.Error("plugin: failed to load", "name", p.Name, "error", err)
			// Non-fatal: log and continue loading other plugins.
		}
	}
	return nil
}

// Load compiles and instantiates a single plugin module.
// If a module with the same name is already loaded it is unloaded first.
func (r *Runtime) Load(ctx context.Context, p plugindom.Plugin) error {
	wasmBytes, err := r.store.LoadWASM(ctx, p.Name)
	if err != nil {
		return fmt.Errorf("runtime load %q: %w", p.Name, err)
	}

	// Build a fresh wazero runtime for this plugin with memory limits.
	rtCfg := wazero.NewRuntimeConfig()
	if r.limits.MaxMemoryPages > 0 {
		rtCfg = rtCfg.WithMemoryLimitPages(r.limits.MaxMemoryPages)
	}
	wasmRT := wazero.NewRuntimeWithConfig(ctx, rtCfg)

	// Instantiate WASI to support common I/O syscalls used by SDK helpers.
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, wasmRT); err != nil {
		_ = wasmRT.Close(ctx)
		return fmt.Errorf("runtime load %q: wasi: %w", p.Name, err)
	}

	// Register the paca host module with all host function bridges.
	if err := r.registerHostModule(ctx, wasmRT, p); err != nil {
		_ = wasmRT.Close(ctx)
		return fmt.Errorf("runtime load %q: host module: %w", p.Name, err)
	}

	// Compile + instantiate the plugin module.
	compiled, err := wasmRT.CompileModule(ctx, wasmBytes)
	if err != nil {
		_ = wasmRT.Close(ctx)
		return fmt.Errorf("runtime load %q: compile: %w", p.Name, err)
	}
	// For WASI reactor builds, _initialize must be called before exported functions
	mod, err := wasmRT.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(p.Name).WithStartFunctions("_initialize"))
	if err != nil {
		_ = wasmRT.Close(ctx)
		return fmt.Errorf("runtime load %q: instantiate: %w", p.Name, err)
	}

	// Call Init if exported.
	if fn := mod.ExportedFunction("Init"); fn != nil {
		callCtx, cancel := context.WithTimeout(ctx, r.limits.MaxCallDuration)
		results, callErr := fn.Call(callCtx)
		cancel()
		if callErr != nil {
			_ = mod.Close(ctx)
			_ = wasmRT.Close(ctx)
			return fmt.Errorf("runtime load %q: Init: %w", p.Name, callErr)
		}
		if len(results) > 0 && results[0] != 0 {
			_ = mod.Close(ctx)
			_ = wasmRT.Close(ctx)
			return fmt.Errorf("runtime load %q: Init returned status %d", p.Name, results[0])
		}
	}

	inst := &pluginInstance{plugin: p, mod: mod, rt: wasmRT}

	r.mu.Lock()
	if existing, ok := r.plugins[p.Name]; ok {
		r.unloadLocked(ctx, existing)
	}
	r.plugins[p.Name] = inst
	r.mu.Unlock()

	r.log.Info("plugin loaded", "name", p.Name, "version", p.Version)
	return nil
}

// Unload shuts down and removes the plugin with the given name.
func (r *Runtime) Unload(ctx context.Context, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if inst, ok := r.plugins[name]; ok {
		r.unloadLocked(ctx, inst)
		delete(r.plugins, name)
	}
}

func (r *Runtime) unloadLocked(ctx context.Context, inst *pluginInstance) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if fn := inst.mod.ExportedFunction("Shutdown"); fn != nil {
		shutCtx, cancel := context.WithTimeout(ctx, r.limits.MaxCallDuration)
		_, _ = fn.Call(shutCtx)
		cancel()
	}
	_ = inst.mod.Close(ctx)
	_ = inst.rt.Close(ctx)
}

// HandleRequest dispatches an HTTP request payload to the named plugin's
// HandleRequest export, returning the serialised response bytes.
func (r *Runtime) HandleRequest(ctx context.Context, pluginName string, reqPayload []byte) ([]byte, error) {
	r.mu.RLock()
	inst, ok := r.plugins[pluginName]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("plugin %q not loaded", pluginName)
	}

	if maxBytes := r.limits.MaxRequestBodyBytes; maxBytes > 0 && int64(len(reqPayload)) > maxBytes {
		return nil, fmt.Errorf("plugin %q: request payload of %d bytes exceeds limit of %d bytes", pluginName, len(reqPayload), maxBytes)
	}

	fn := inst.mod.ExportedFunction("HandleRequest")
	if fn == nil {
		return nil, fmt.Errorf("plugin %q: HandleRequest not exported", pluginName)
	}

	// Hold the per-instance lock for the entire interaction, including the
	// initial write. wazero module calls are not safe to interleave: two
	// concurrent malloc calls into the same instance can race on the
	// plugin's bump-allocator cursor the same way an oversized payload does
	// below.
	inst.mu.Lock()
	defer inst.mu.Unlock()

	// Always give the plugin's allocator a chance to reset, even if a step
	// below fails. A plugin's malloc export is a bump allocator with no
	// bounds checking: it advances its internal cursor unconditionally, so a
	// write that the host rejects as out-of-bounds still leaves that cursor
	// corrupted. Without this reset, every later call computes addresses
	// from the corrupted cursor and fails too, permanently "poisoning" the
	// instance after a single oversized request.
	defer func() {
		resetFn := inst.mod.ExportedFunction("ResetAllocator")
		if resetFn == nil {
			return
		}
		resetCtx, cancel := context.WithTimeout(context.Background(), r.limits.MaxCallDuration)
		_, _ = resetFn.Call(resetCtx) // Best-effort; ignore errors
		cancel()
	}()

	// Write the request payload into the plugin's linear memory.
	ptrLen, err := writeToMemory(inst.mod, reqPayload)
	if err != nil {
		return nil, fmt.Errorf("plugin %q: write request: %w", pluginName, err)
	}

	callCtx, cancel := context.WithTimeout(ctx, r.limits.MaxCallDuration)
	results, callErr := fn.Call(callCtx, ptrLen[0], ptrLen[1])
	cancel()

	if callErr != nil {
		return nil, fmt.Errorf("plugin %q: HandleRequest: %w", pluginName, callErr)
	}
	if len(results) < 1 {
		return nil, fmt.Errorf("plugin %q: HandleRequest returned wrong number of values", pluginName)
	}

	combined := results[0]
	outPtr := uint64(combined) >> 32
	outLen := uint64(combined) & 0xFFFFFFFF
	resp, readErr := readFromMemory(inst.mod, outPtr, outLen)
	if readErr != nil {
		return nil, readErr
	}

	return resp, nil
}

// EmitEvent serialises the event payload and dispatches it to every loaded
// plugin that has subscribed to the topic.
func (r *Runtime) EmitEvent(ctx context.Context, topic string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		r.log.Error("plugin: marshal event payload", "topic", topic, "error", err)
		return
	}

	r.mu.RLock()
	instances := make([]*pluginInstance, 0, len(r.plugins))
	for _, inst := range r.plugins {
		for _, sub := range inst.plugin.Manifest.Backend.EventSubscriptions {
			if sub == topic {
				instances = append(instances, inst)
				break
			}
		}
	}
	r.mu.RUnlock()

	for _, inst := range instances {
		r.dispatchEvent(ctx, inst, topic, data)
	}
}

// dispatchEvent invokes a single plugin instance's HandleEvent export. It
// holds the instance lock for the full write+call+reset cycle, mirroring
// HandleRequest: concurrent calls into the same module must not interleave
// malloc invocations, and the allocator must be reset on every exit path so
// a write the host rejects as out-of-bounds cannot leave the plugin's bump
// allocator cursor permanently corrupted.
func (r *Runtime) dispatchEvent(ctx context.Context, inst *pluginInstance, topic string, data []byte) {
	fn := inst.mod.ExportedFunction("HandleEvent")
	if fn == nil {
		return
	}

	if maxBytes := r.limits.MaxRequestBodyBytes; maxBytes > 0 && int64(len(data)) > maxBytes {
		r.log.Warn("plugin: event payload exceeds size limit, dropping",
			"name", inst.plugin.Name, "topic", topic, "size", len(data), "limit", maxBytes)
		return
	}

	inst.mu.Lock()
	defer inst.mu.Unlock()
	defer func() {
		resetFn := inst.mod.ExportedFunction("ResetAllocator")
		if resetFn == nil {
			return
		}
		resetCtx, cancel := context.WithTimeout(context.Background(), r.limits.MaxCallDuration)
		_, _ = resetFn.Call(resetCtx) // Best-effort; ignore errors
		cancel()
	}()

	ptrLen, err := writeToMemory(inst.mod, data)
	if err != nil {
		r.log.Error("plugin: write event payload", "name", inst.plugin.Name, "error", err)
		return
	}
	topicPtrLen, err := writeToMemory(inst.mod, []byte(topic))
	if err != nil {
		r.log.Error("plugin: write topic", "name", inst.plugin.Name, "error", err)
		return
	}

	callCtx, cancel := context.WithTimeout(ctx, r.limits.MaxCallDuration)
	_, _ = fn.Call(callCtx, topicPtrLen[0], topicPtrLen[1], ptrLen[0], ptrLen[1])
	cancel()
}

// PluginRoutes returns the Gin-compatible route definitions for the named plugin.
// Returns nil when the plugin is not loaded or has no backend routes.
func (r *Runtime) PluginRoutes(name string) []plugindom.PluginRoute {
	r.mu.RLock()
	defer r.mu.RUnlock()
	inst, ok := r.plugins[name]
	if !ok || inst.plugin.Manifest.Backend == nil {
		return nil
	}
	return inst.plugin.Manifest.Backend.Routes
}

// LoadedNames returns the names of all currently loaded plugins.
func (r *Runtime) LoadedNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	return names
}

// -------------------------------------------------------------------------
// Host module — paca namespace
// -------------------------------------------------------------------------

// registerHostModule builds the "paca" host module that exports all host
// functions available to plugin WASM modules.
func (r *Runtime) registerHostModule(ctx context.Context, rt wazero.Runtime, p plugindom.Plugin) error {
	builder := rt.NewHostModuleBuilder("paca")

	// --- DB host functions (PLUG-BE-04) ------------------------------------
	r.registerDBFunctions(builder, p)

	// --- Core read-only functions (PLUG-BE-05) -----------------------------
	r.registerCoreFunctions(builder, p)

	// --- HTTP host functions (PLUG-BE-06) ----------------------------------
	r.registerHTTPFunctions(builder, p)

	// --- Outbound fetch host function (PLUG-BE-08) -------------------------
	r.registerFetchFunction(builder, p)

	// --- Event and utility functions (PLUG-BE-07) --------------------------
	r.registerEventFunctions(builder, p)

	_, err := builder.Instantiate(ctx)
	return err
}

// -------------------------------------------------------------------------
// PLUG-BE-04: DB host functions
// -------------------------------------------------------------------------

// dbQueryResult is the JSON shape returned to the plugin for query results.
type dbQueryResult struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

// registerDBFunctions adds paca.db_query, paca.db_exec, paca.db_tx_begin,
// paca.db_tx_commit, paca.db_tx_rollback, paca.storage_get, paca.storage_set,
// paca.storage_delete to the host module builder.
//
// Project-scope isolation is enforced on all queries by prefixing the table
// search path with the plugin's schema.  Plugins must declare a `project_id`
// parameter in their queries; the host validates it matches the authorised
// project before execution.
func (r *Runtime) registerDBFunctions(b wazero.HostModuleBuilder, p plugindom.Plugin) {
	schema := schemaName(p.Name)

	// paca.db_query(sqlPtr, sqlLen, paramsPtr, paramsLen, resultPtrPtr, resultLenPtr)
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			sqlStr, err := readString(m, stack[0], stack[1])
			if err != nil {
				r.log.Error("paca.db_query: read sql", "plugin", p.Name, "error", err)
				return
			}
			paramsJSON, err := readString(m, stack[2], stack[3])
			if err != nil {
				r.log.Error("paca.db_query: read params", "plugin", p.Name, "error", err)
				return
			}

			result, err := r.execQuery(ctx, p, schema, sqlStr, paramsJSON)
			if err != nil {
				r.log.Error("paca.db_query: exec", "plugin", p.Name, "error", err)
				return
			}
			resultPtrLen := writeJSONResult(m, result)
			m.Memory().WriteUint32Le(uint32(stack[4]), uint32(resultPtrLen[0]))
			m.Memory().WriteUint32Le(uint32(stack[5]), uint32(resultPtrLen[1]))
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64},
			nil).
		Export("db_query")

	// paca.db_query2(sqlPtr, sqlLen, paramsPtr, paramsLen, resultPtrPtr, resultLenPtr, errPtrPtr, errLenPtr)
	// Same as db_query but also reports execution errors back to the plugin.
	// db_query's signature has no error channel, so a query that fails to
	// execute (e.g. references a column that doesn't exist) silently looks
	// like a zero-row success to the plugin — see the `return` with no
	// output write in the error branch above. Exposed as a separate export
	// rather than changing db_query in place: WASM import/export signatures
	// are matched exactly at instantiation time, so widening db_query would
	// break every already-compiled plugin binary that still imports the old
	// 6-arg form. New/rebuilt plugins should call this one instead.
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			sqlStr, err := readString(m, stack[0], stack[1])
			if err != nil {
				r.log.Error("paca.db_query2: read sql", "plugin", p.Name, "error", err)
				return
			}
			paramsJSON, err := readString(m, stack[2], stack[3])
			if err != nil {
				r.log.Error("paca.db_query2: read params", "plugin", p.Name, "error", err)
				return
			}

			result, err := r.execQuery(ctx, p, schema, sqlStr, paramsJSON)
			if err != nil {
				errPtrLen, writeErr := writeToMemory(m, []byte(err.Error()))
				if writeErr != nil {
					r.log.Error("paca.db_query2: write error", "plugin", p.Name, "error", writeErr)
					errPtrLen = []uint64{0, 0}
				}
				m.Memory().WriteUint32Le(uint32(stack[4]), 0)
				m.Memory().WriteUint32Le(uint32(stack[5]), 0)
				m.Memory().WriteUint32Le(uint32(stack[6]), uint32(errPtrLen[0]))
				m.Memory().WriteUint32Le(uint32(stack[7]), uint32(errPtrLen[1]))
				return
			}
			resultPtrLen := writeJSONResult(m, result)
			m.Memory().WriteUint32Le(uint32(stack[4]), uint32(resultPtrLen[0]))
			m.Memory().WriteUint32Le(uint32(stack[5]), uint32(resultPtrLen[1]))
			m.Memory().WriteUint32Le(uint32(stack[6]), 0)
			m.Memory().WriteUint32Le(uint32(stack[7]), 0)
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64},
			nil).
		Export("db_query2")

	// paca.db_exec(sqlPtr, sqlLen, paramsPtr, paramsLen, rowsAffectedPtr, errPtrPtr, errLenPtr)
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			sqlStr, err := readString(m, stack[0], stack[1])
			if err != nil {
				return
			}
			paramsJSON, err := readString(m, stack[2], stack[3])
			if err != nil {
				return
			}

			rows, err := r.execStatement(ctx, p, schema, sqlStr, paramsJSON)
			if err != nil {
				errBytes := []byte(err.Error())
				ptrLen, _ := writeToMemory(m, errBytes)
				m.Memory().WriteUint64Le(uint32(stack[4]), 0)
				m.Memory().WriteUint32Le(uint32(stack[5]), uint32(ptrLen[0]))
				m.Memory().WriteUint32Le(uint32(stack[6]), uint32(ptrLen[1]))
				return
			}
			m.Memory().WriteUint64Le(uint32(stack[4]), uint64(rows))
			m.Memory().WriteUint32Le(uint32(stack[5]), 0)
			m.Memory().WriteUint32Le(uint32(stack[6]), 0)
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64},
			nil).
		Export("db_exec")

	// paca.storage_get(keyPtr, keyLen, valuePtrPtr, valueLenPtr)
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			key, err := readString(m, stack[0], stack[1])
			if err != nil {
				return
			}

			var value string
			row := r.services.DB.QueryRowContext(ctx,
				`SELECT value FROM `+schema+`.plugin_kv WHERE key = $1`, key)
			if err := row.Scan(&value); err != nil {
				if err == sql.ErrNoRows {
					m.Memory().WriteUint32Le(uint32(stack[2]), 0)
					m.Memory().WriteUint32Le(uint32(stack[3]), 0)
					return
				}
				return
			}
			ptrLen, _ := writeToMemory(m, []byte(value))
			m.Memory().WriteUint32Le(uint32(stack[2]), uint32(ptrLen[0]))
			m.Memory().WriteUint32Le(uint32(stack[3]), uint32(ptrLen[1]))
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64},
			nil).
		Export("storage_get")

	// paca.storage_set(keyPtr, keyLen, valuePtr, valueLen) -> (ok i32)
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			key, _ := readString(m, stack[0], stack[1])
			value, _ := readString(m, stack[2], stack[3])
			_, err := r.services.DB.ExecContext(ctx,
				`INSERT INTO `+schema+`.plugin_kv (key, value) VALUES ($1, $2)
				 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, value)
			if err != nil {
				r.log.Error("paca.storage_set", "plugin", p.Name, "error", err)
				stack[0] = 0
				return
			}
			stack[0] = 1
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64},
			[]api.ValueType{api.ValueTypeI32}).
		Export("storage_set")

	// paca.storage_delete(keyPtr, keyLen) -> (ok i32)
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			key, _ := readString(m, stack[0], stack[1])
			_, err := r.services.DB.ExecContext(ctx,
				`DELETE FROM `+schema+`.plugin_kv WHERE key = $1`, key)
			if err != nil {
				r.log.Error("paca.storage_delete", "plugin", p.Name, "error", err)
				stack[0] = 0
				return
			}
			stack[0] = 1
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64},
			[]api.ValueType{api.ValueTypeI32}).
		Export("storage_delete")
}

// execQuery runs a SELECT statement scoped to the plugin schema and returns a
// dbQueryResult JSON-encoded result. caller is the requesting plugin, used to
// redact sensitive columns (core, or declared by other plugins) the query
// reaches, and to gate DML-with-RETURNING writes to sensitive tables (see
// sensitiveColumnsForQuery and checkWriteAllowed).
func (r *Runtime) execQuery(ctx context.Context, caller plugindom.Plugin, schema, sqlStr, paramsJSON string) (*dbQueryResult, error) {
	// Allow SELECT and DML statements that use RETURNING.
	trimmed := strings.TrimSpace(strings.ToUpper(sqlStr))
	isDML := strings.HasPrefix(trimmed, "INSERT") || strings.HasPrefix(trimmed, "UPDATE") || strings.HasPrefix(trimmed, "DELETE")
	if !strings.HasPrefix(trimmed, "SELECT") && (!isDML || !strings.Contains(trimmed, "RETURNING")) {
		return nil, fmt.Errorf("paca.db_query: only SELECT and DML with RETURNING statements are allowed")
	}
	if isDML {
		if err := r.checkWriteAllowed(caller, sqlStr, "paca.db_query"); err != nil {
			return nil, err
		}
	}

	var queryParams []any
	if paramsJSON != "" && paramsJSON != "null" {
		if err := json.Unmarshal([]byte(paramsJSON), &queryParams); err != nil {
			return nil, fmt.Errorf("paca.db_query: parse params: %w", err)
		}
	}

	tx, err := r.services.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("paca.db_query: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path TO "+schema+",public"); err != nil {
		return nil, fmt.Errorf("paca.db_query: set search_path: %w", err)
	}

	rows, err := tx.QueryContext(ctx, sqlStr, queryParams...)
	if err != nil {
		return nil, fmt.Errorf("paca.db_query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	// The driver returns text/json/jsonb columns as []byte; encoding/json would
	// otherwise base64-encode those when marshaling the result across the WASM
	// boundary, corrupting the value for the plugin. Convert those columns to
	// string so plugins receive the raw text. Genuinely binary columns (bytea)
	// are left as []byte so they keep going through the safe base64 path.
	isTextColumn := make([]bool, len(colTypes))
	for i, ct := range colTypes {
		isTextColumn[i] = strings.ToUpper(ct.DatabaseTypeName()) != "BYTEA"
	}
	result := &dbQueryResult{Columns: cols}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok && isTextColumn[i] {
				vals[i] = string(b)
			}
		}
		result.Rows = append(result.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("paca.db_query: commit: %w", err)
	}

	if sensitive := r.sensitiveColumnsForQuery(caller, sqlStr); len(sensitive) > 0 {
		redactColumns(result.Columns, result.Rows, sensitive)
	}

	return result, nil
}

// coreSensitiveFields lists platform database columns that
// db_query/db_query2/db_exec must always treat as sensitive, regardless of
// which schema a plugin's search_path resolves them through. Nobody "owns"
// this data the way a plugin owns its own schema (declared via
// BackendManifest.SensitiveFields) — a plugin may only read or write it if
// it explicitly lists the field in its own manifest's
// RequestedSensitiveFields, which must be surfaced to admins in the plugin
// marketplace before install so the request is visible, not silently
// granted.
//
// github_integrations/github_repositories used to live here too (they
// predate the per-plugin schema model and lived in core `public`), but
// migrations/000007_remove_github_tables.sql dropped them from public and
// paca-plugin-github/backend/migrations/0001_create_github_tables.sql
// recreates them under its own plugin_data_com_paca_github schema — so
// they're now an ordinary plugin-owned-schema case, protected via that
// plugin's own BackendManifest.SensitiveFields instead of this registry.
var coreSensitiveFields = map[string][]string{
	"users":                       {"password_hash"},
	"api_keys":                    {"key_hash"},
	"agents":                      {"llm_api_key_secret", "acp_bridge_token_hash"},
	"agent_mcp_servers":           {"env"},
	"agent_environment_variables": {"encrypted_value"},
}

// fromJoinTableRe extracts (schema, table) pairs referenced after FROM/JOIN
// in a query's SQL text. Like paca-plugin-dashboard's own query guard, this
// is a lightweight pattern match rather than a full SQL parser: adequate as
// a defense-in-depth signal over plugin-authored SQL, not a guarantee for
// arbitrarily obfuscated queries.
var fromJoinTableRe = regexp.MustCompile(`(?i)\b(?:from|join)\s+"?([A-Za-z_][A-Za-z0-9_]*)"?(?:\s*\.\s*"?([A-Za-z_][A-Za-z0-9_]*)"?)?`)

// writeTargetRe extracts the (schema, table) pair a single INSERT/UPDATE/
// DELETE statement writes to.
var writeTargetRe = regexp.MustCompile(`(?i)^\s*(?:insert\s+into|update|delete\s+from)\s+"?([A-Za-z_][A-Za-z0-9_]*)"?(?:\s*\.\s*"?([A-Za-z_][A-Za-z0-9_]*)"?)?`)

// tableRef is a (possibly schema-qualified) table reference extracted from
// SQL text. Schema is empty when the reference is unqualified.
type tableRef struct {
	schema string
	table  string
}

func referencedTables(sqlStr string) []tableRef {
	matches := fromJoinTableRe.FindAllStringSubmatch(sqlStr, -1)
	refs := make([]tableRef, 0, len(matches))
	for _, m := range matches {
		if m[2] != "" {
			refs = append(refs, tableRef{schema: strings.ToLower(m[1]), table: strings.ToLower(m[2])})
		} else {
			refs = append(refs, tableRef{table: strings.ToLower(m[1])})
		}
	}
	return refs
}

func writeTargetTable(sqlStr string) (tableRef, bool) {
	m := writeTargetRe.FindStringSubmatch(sqlStr)
	if m == nil {
		return tableRef{}, false
	}
	if m[2] != "" {
		return tableRef{schema: strings.ToLower(m[1]), table: strings.ToLower(m[2])}, true
	}
	return tableRef{table: strings.ToLower(m[1])}, true
}

// sensitiveTableColumns returns the sensitive columns declared for a
// resolved (schema, table) reference and, when the table belongs to another
// plugin's own schema rather than being a core table, that plugin's name
// (owner is "" for core fields, since nobody owns those). callerSchema is
// the calling plugin's own schema: a reference into it is never sensitive,
// since a plugin always has full access to its own data.
//
// An unqualified table name (schema == "") can only ever resolve to the
// caller's own schema or the core `public` schema (search_path never
// reaches another plugin's schema without explicit qualification), so it's
// checked against the core registry directly. This means a plugin that
// happens to name one of its own tables identically to a core table (e.g.
// "users") would have that column over-redacted/write-blocked too — a
// fail-safe false positive, not a false negative.
func (r *Runtime) sensitiveTableColumns(callerSchema string, ref tableRef) (owner string, cols []string) {
	if ref.schema != "" && strings.EqualFold(ref.schema, callerSchema) {
		return "", nil
	}
	if ref.schema == "" || strings.EqualFold(ref.schema, "public") {
		return "", coreSensitiveFields[ref.table]
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	for name, inst := range r.plugins {
		if !strings.EqualFold(schemaName(name), ref.schema) {
			continue
		}
		if inst.plugin.Manifest.Backend == nil {
			return name, nil
		}
		return name, inst.plugin.Manifest.Backend.SensitiveFields[ref.table]
	}
	return "", nil
}

// isRequestedSensitiveField reports whether caller's own manifest declares
// wanting access to table.column, where owner is "" for a core field or the
// owning plugin's ID for a field declared sensitive in that plugin's own
// SensitiveFields.
func isRequestedSensitiveField(caller plugindom.Plugin, owner, table, col string) bool {
	if caller.Manifest.Backend == nil {
		return false
	}
	var key string
	if owner == "" {
		key = table + "." + col
	} else {
		key = owner + ":" + table + "." + col
	}
	for _, f := range caller.Manifest.Backend.RequestedSensitiveFields {
		if strings.EqualFold(f, key) {
			return true
		}
	}
	return false
}

// sensitiveColumnsForQuery returns the set of lowercased column names that
// must be redacted in the result of sqlStr run by caller: for every
// FROM/JOIN table the query references, any sensitive column declared for
// it (core, or another plugin's own SensitiveFields) that caller hasn't
// explicitly requested access to via its own RequestedSensitiveFields.
func (r *Runtime) sensitiveColumnsForQuery(caller plugindom.Plugin, sqlStr string) map[string]struct{} {
	callerSchema := schemaName(caller.Name)
	sensitive := make(map[string]struct{})
	for _, ref := range referencedTables(sqlStr) {
		owner, cols := r.sensitiveTableColumns(callerSchema, ref)
		for _, col := range cols {
			if isRequestedSensitiveField(caller, owner, ref.table, col) {
				continue
			}
			sensitive[strings.ToLower(col)] = struct{}{}
		}
	}
	return sensitive
}

// checkWriteAllowed rejects an INSERT/UPDATE/DELETE statement outright when
// its target table has sensitive columns declared (core, or another
// plugin's own SensitiveFields) that caller hasn't requested access to.
// Unlike reads, sensitivity can't be masked away column-by-column for a
// write, so the whole statement is blocked before it ever reaches Postgres.
// A table with no declared sensitive columns is unaffected (out of scope,
// same boundary as read redaction). funcName is used only in the returned
// error to name the host function that rejected the call.
func (r *Runtime) checkWriteAllowed(caller plugindom.Plugin, sqlStr, funcName string) error {
	ref, ok := writeTargetTable(sqlStr)
	if !ok {
		return nil
	}
	callerSchema := schemaName(caller.Name)
	owner, cols := r.sensitiveTableColumns(callerSchema, ref)
	if len(cols) == 0 {
		return nil
	}
	for _, col := range cols {
		if isRequestedSensitiveField(caller, owner, ref.table, col) {
			return nil
		}
	}
	return fmt.Errorf("%s: table %q has sensitive fields this plugin has not requested access to (declare requestedSensitiveFields in plugin.json)", funcName, ref.table)
}

// redactColumns overwrites every row's value with "***" at each column index
// whose lowercased name is in sensitive.
func redactColumns(cols []string, rows [][]any, sensitive map[string]struct{}) {
	for i, col := range cols {
		if _, ok := sensitive[strings.ToLower(col)]; !ok {
			continue
		}
		for _, row := range rows {
			row[i] = "***"
		}
	}
}

// execStatement runs a non-SELECT DML statement scoped to the plugin schema.
func (r *Runtime) execStatement(ctx context.Context, caller plugindom.Plugin, schema, sqlStr, paramsJSON string) (int64, error) {
	trimmed := strings.TrimSpace(strings.ToUpper(sqlStr))
	for _, banned := range []string{"DROP", "TRUNCATE", "ALTER", "CREATE", "GRANT", "REVOKE"} {
		if strings.HasPrefix(trimmed, banned) {
			return 0, fmt.Errorf("paca.db_exec: DDL/DCL statements are not allowed")
		}
	}
	if err := r.checkWriteAllowed(caller, sqlStr, "paca.db_exec"); err != nil {
		return 0, err
	}

	var queryParams []any
	if paramsJSON != "" && paramsJSON != "null" {
		if err := json.Unmarshal([]byte(paramsJSON), &queryParams); err != nil {
			return 0, fmt.Errorf("paca.db_exec: parse params: %w", err)
		}
	}

	tx, err := r.services.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("paca.db_exec: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path TO "+schema+",public"); err != nil {
		return 0, fmt.Errorf("paca.db_exec: set search_path: %w", err)
	}

	res, err := tx.ExecContext(ctx, sqlStr, queryParams...)
	if err != nil {
		return 0, fmt.Errorf("paca.db_exec: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("paca.db_exec: commit: %w", err)
	}
	return rowsAffected, nil
}

// -------------------------------------------------------------------------
// PLUG-BE-05: Core read-only functions
// -------------------------------------------------------------------------

// loadTaskAssigneeIDs batch-loads task_assignees for taskIDs, returning a map
// keyed by task ID (tasks with no assignees are simply absent from the map).
func (r *Runtime) loadTaskAssigneeIDs(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	result := make(map[uuid.UUID][]uuid.UUID, len(taskIDs))
	if len(taskIDs) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(taskIDs))
	args := make([]any, len(taskIDs))
	for i, id := range taskIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	rows, err := r.services.DB.QueryContext(ctx,
		`SELECT task_id, member_id FROM task_assignees WHERE task_id IN (`+strings.Join(placeholders, ",")+`) ORDER BY assigned_at ASC`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("load task assignees: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var taskID, memberID uuid.UUID
		if err := rows.Scan(&taskID, &memberID); err != nil {
			return nil, fmt.Errorf("load task assignees: scan: %w", err)
		}
		result[taskID] = append(result[taskID], memberID)
	}
	return result, rows.Err()
}

// registerCoreFunctions adds paca.tasks_list, paca.task_get,
// paca.project_get, paca.members_list to the host module builder.
// All results are scoped to the authorised project extracted from the
// request context value set by the Gin auth middleware.
func (r *Runtime) registerCoreFunctions(b wazero.HostModuleBuilder, _ plugindom.Plugin) {
	// paca.tasks_list(projectIdPtr, projectIdLen) -> (jsonPtr, jsonLen)
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			projectIDStr, _ := readString(m, stack[0], stack[1])
			projectID, err := uuid.Parse(projectIDStr)
			if err != nil {
				copy(stack, writeErrorResult(m, fmt.Errorf("paca.tasks_list: invalid project_id: %w", err)))
				return
			}

			rows, err := r.services.DB.QueryContext(ctx,
				`SELECT id, title, status_id, task_number FROM tasks
				 WHERE project_id = $1 AND deleted_at IS NULL
				 ORDER BY task_number DESC LIMIT 100`, projectID)
			if err != nil {
				copy(stack, writeErrorResult(m, err))
				return
			}

			type taskRow struct {
				id       uuid.UUID
				title    string
				statusID *uuid.UUID
				num      int
			}
			var taskRows []taskRow
			for rows.Next() {
				var tr taskRow
				if err := rows.Scan(&tr.id, &tr.title, &tr.statusID, &tr.num); err != nil {
					_ = rows.Close()
					copy(stack, writeErrorResult(m, err))
					return
				}
				taskRows = append(taskRows, tr)
			}
			_ = rows.Close()
			if err := rows.Err(); err != nil {
				copy(stack, writeErrorResult(m, err))
				return
			}

			taskIDs := make([]uuid.UUID, len(taskRows))
			for i, tr := range taskRows {
				taskIDs[i] = tr.id
			}
			assigneeIDs, err := r.loadTaskAssigneeIDs(ctx, taskIDs)
			if err != nil {
				copy(stack, writeErrorResult(m, err))
				return
			}

			var tasks []map[string]any
			for _, tr := range taskRows {
				tasks = append(tasks, map[string]any{
					"id": tr.id, "title": tr.title, "status_id": tr.statusID,
					"assignee_ids": assigneeIDs[tr.id], "task_number": tr.num,
				})
			}
			copy(stack, writeJSONResult(m, tasks))
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64},
			[]api.ValueType{api.ValueTypeI64, api.ValueTypeI64}).
		Export("tasks_list")

	// paca.task_get(taskIdPtr, taskIdLen) -> (jsonPtr, jsonLen)
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			taskIDStr, _ := readString(m, stack[0], stack[1])
			taskID, err := uuid.Parse(taskIDStr)
			if err != nil {
				copy(stack, writeErrorResult(m, err))
				return
			}

			row := r.services.DB.QueryRowContext(ctx,
				`SELECT id, project_id, title, status_id, task_number
				 FROM tasks WHERE id = $1 AND deleted_at IS NULL`, taskID)
			var (
				id, projectID uuid.UUID
				title         string
				statusID      *uuid.UUID
				num           int
			)
			if err := row.Scan(&id, &projectID, &title, &statusID, &num); err != nil {
				copy(stack, writeErrorResult(m, err))
				return
			}
			assigneeIDs, err := r.loadTaskAssigneeIDs(ctx, []uuid.UUID{id})
			if err != nil {
				copy(stack, writeErrorResult(m, err))
				return
			}
			copy(stack, writeJSONResult(m, map[string]any{
				"id": id, "project_id": projectID, "title": title,
				"status_id": statusID, "assignee_ids": assigneeIDs[id], "task_number": num,
			}))
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64},
			[]api.ValueType{api.ValueTypeI64, api.ValueTypeI64}).
		Export("task_get")

	// paca.project_get(projectIdPtr, projectIdLen) -> (jsonPtr, jsonLen)
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			projectIDStr, _ := readString(m, stack[0], stack[1])
			projectID, err := uuid.Parse(projectIDStr)
			if err != nil {
				copy(stack, writeErrorResult(m, err))
				return
			}

			row := r.services.DB.QueryRowContext(ctx,
				`SELECT id, name, description, task_id_prefix FROM projects WHERE id = $1`, projectID)
			var id uuid.UUID
			var name, description, prefix string
			if err := row.Scan(&id, &name, &description, &prefix); err != nil {
				copy(stack, writeErrorResult(m, err))
				return
			}
			copy(stack, writeJSONResult(m, map[string]any{
				"id": id, "name": name, "description": description, "task_id_prefix": prefix,
			}))
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64},
			[]api.ValueType{api.ValueTypeI64, api.ValueTypeI64}).
		Export("project_get")

	// paca.members_list(projectIdPtr, projectIdLen) -> (jsonPtr, jsonLen)
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			projectIDStr, _ := readString(m, stack[0], stack[1])
			projectID, err := uuid.Parse(projectIDStr)
			if err != nil {
				copy(stack, writeErrorResult(m, err))
				return
			}

			rows, err := r.services.DB.QueryContext(ctx,
				`SELECT pm.id, u.username, u.full_name, pr.role_name
				 FROM project_members pm
				 JOIN users u ON u.id = pm.user_id
				 JOIN project_roles pr ON pr.id = pm.project_role_id
		WHERE pm.project_id = $1`, projectID)
			if err != nil {
				copy(stack, writeErrorResult(m, err))
				return
			}
			defer func() { _ = rows.Close() }()

			var members []map[string]any
			for rows.Next() {
				var id uuid.UUID
				var username, fullName, roleName string
				if err := rows.Scan(&id, &username, &fullName, &roleName); err != nil {
					copy(stack, writeErrorResult(m, err))
					return
				}
				members = append(members, map[string]any{
					"id": id, "username": username, "full_name": fullName, "role_name": roleName,
				})
			}
			copy(stack, writeJSONResult(m, members))
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64},
			[]api.ValueType{api.ValueTypeI64, api.ValueTypeI64}).
		Export("members_list")
}

// -------------------------------------------------------------------------
// PLUG-BE-06: HTTP host functions
// -------------------------------------------------------------------------

// pluginRequestKey is the context key used to pass the inbound HTTP request
// payload from the Gin handler to the host function bridge.
type pluginRequestKey struct{}

// WithPluginRequest attaches the serialised HTTP request payload to a context
// so that the host functions paca.http_request_body and
// paca.http_request_headers can retrieve it.
func WithPluginRequest(ctx context.Context, payload *HTTPRequest) context.Context {
	return context.WithValue(ctx, pluginRequestKey{}, payload)
}

// HTTPRequest is the serialised inbound request passed to
// HandleRequest and exposed via the HTTP host functions.
type HTTPRequest struct {
	Method     string            `json:"method"`
	Path       string            `json:"path"`
	Query      map[string]string `json:"query"`
	ProjectID  string            `json:"project_id"`
	CallerID   string            `json:"caller_id"`
	UserID     string            `json:"user_id"`
	CallerRole string            `json:"caller_role"`
	Headers    map[string]string `json:"headers"`
	Body       []byte            `json:"body"`
}

func (r *Runtime) registerHTTPFunctions(b wazero.HostModuleBuilder, _ plugindom.Plugin) {
	// paca.http_request_body() -> (bodyPtr, bodyLen)
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			req, _ := ctx.Value(pluginRequestKey{}).(*HTTPRequest)
			if req == nil {
				stack[0], stack[1] = 0, 0
				return
			}
			ptrLen, _ := writeToMemory(m, req.Body)
			copy(stack, ptrLen)
		}), nil, []api.ValueType{api.ValueTypeI64, api.ValueTypeI64}).
		Export("http_request_body")

	// paca.http_request_headers() -> (jsonPtr, jsonLen)
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			req, _ := ctx.Value(pluginRequestKey{}).(*HTTPRequest)
			if req == nil {
				stack[0], stack[1] = 0, 0
				return
			}
			copy(stack, writeJSONResult(m, req.Headers))
		}), nil, []api.ValueType{api.ValueTypeI64, api.ValueTypeI64}).
		Export("http_request_headers")

	// paca.http_caller_identity() -> (jsonPtr, jsonLen)
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			req, _ := ctx.Value(pluginRequestKey{}).(*HTTPRequest)
			if req == nil {
				stack[0], stack[1] = 0, 0
				return
			}
			copy(stack, writeJSONResult(m, map[string]string{
				"caller_id":   req.CallerID,
				"user_id":     req.UserID,
				"caller_role": req.CallerRole,
				"project_id":  req.ProjectID,
			}))
		}), nil, []api.ValueType{api.ValueTypeI64, api.ValueTypeI64}).
		Export("http_caller_identity")

	// paca.http_respond(statusCode i32, bodyPtr, bodyLen) — no-op; response is
	// returned from HandleRequest by the SDK wrapper.
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(_ context.Context, _ api.Module, _ []uint64) {
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI64, api.ValueTypeI64}, nil).
		Export("http_respond")

	// paca.permission_check(permissionPtr, permissionLen) -> (ok i32)
	//
	// Checks whether the current caller (from the request context set by
	// WithPluginRequest) holds the given permission key, evaluated against the
	// same effective permission set as requirePermissions route middleware:
	// built-in permissions, LegacyPermissionsForRole, and any plugin-declared
	// custom permission granted to the caller's project/global role. Scope
	// (project vs global) is inferred from whether the request carries a
	// project_id: project-scoped requests check project-role permissions,
	// others check global-role permissions only.
	//
	// This lets plugin backend code enforce finer-grained authorization
	// than the single all-or-nothing requirePermissions route gate allows —
	// e.g. "is caller the record's author OR does caller hold
	// time_logging.manage_all" — without a second host round-trip per check.
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			permission, _ := readString(m, stack[0], stack[1])
			req, _ := ctx.Value(pluginRequestKey{}).(*HTTPRequest)
			if req == nil || permission == "" || r.services.Authorizer == nil {
				stack[0] = 0
				return
			}

			userID, err := uuid.Parse(req.UserID)
			if err != nil {
				stack[0] = 0
				return
			}

			var projectID *uuid.UUID
			if req.ProjectID != "" {
				pid, err := uuid.Parse(req.ProjectID)
				if err != nil {
					stack[0] = 0
					return
				}
				projectID = &pid
			}

			granted, err := r.services.Authorizer.HasPermissions(ctx, userID, projectID, req.CallerRole, authz.Permission(permission))
			if err != nil || !granted {
				stack[0] = 0
				return
			}
			stack[0] = 1
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64}, []api.ValueType{api.ValueTypeI32}).
		Export("permission_check")
}

// -------------------------------------------------------------------------
// PLUG-BE-07: Event and utility functions
// -------------------------------------------------------------------------

func (r *Runtime) registerEventFunctions(b wazero.HostModuleBuilder, p plugindom.Plugin) {
	// paca.event_emit(topicPtr, topicLen, payloadPtr, payloadLen) -> (ok i32)
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			topic, _ := readString(m, stack[0], stack[1])
			payload, _ := readString(m, stack[2], stack[3])

			if r.services.Publisher != nil {
				var v any
				_ = json.Unmarshal([]byte(payload), &v)
				if v == nil {
					v = map[string]any{}
				}
				if err := r.services.Publisher.Publish(ctx, "paca.events", map[string]any{
					"type":    topic,
					"source":  p.Name,
					"payload": v,
				}); err != nil {
					r.log.Error("paca.event_emit", "plugin", p.Name, "error", err)
					stack[0] = 0
					return
				}
			}
			stack[0] = 1
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64},
			[]api.ValueType{api.ValueTypeI32}).
		Export("event_emit")

	// paca.event_subscribe — no-op; subscriptions are declared in plugin.json.
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			stack[0] = 1
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64},
			[]api.ValueType{api.ValueTypeI32}).
		Export("event_subscribe")

	// paca.activity_record(payloadPtr i64, payloadLen i64) -> ok i32
	// Appends a task-activity event to paca.task_activities stream so the
	// ActivityConsumer worker can persist it to PostgreSQL.
	// Payload JSON shape:
	//   {"task_id":"uuid","activity_type":"task.checklist.created","content":{...}}
	// actor_id and project_id are derived from the request context to prevent
	// spoofing; plugin-supplied values for those fields are ignored.
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			raw, _ := readString(m, stack[0], stack[1])
			var inp struct {
				TaskID       string `json:"task_id"`
				ActivityType string `json:"activity_type"`
				Content      any    `json:"content"`
			}
			if err := json.Unmarshal([]byte(raw), &inp); err != nil || inp.TaskID == "" || inp.ActivityType == "" {
				r.log.Warn("paca.activity_record: invalid payload", "plugin", p.Name)
				stack[0] = 0
				return
			}

			// Validate task_id is a well-formed UUID.
			taskID, err := uuid.Parse(inp.TaskID)
			if err != nil {
				r.log.Warn("paca.activity_record: invalid task_id", "plugin", p.Name, "task_id", inp.TaskID)
				stack[0] = 0
				return
			}

			// Derive actor_id and project_id from the authenticated request
			// context.  These must not be trusted from the plugin payload to
			// prevent actor impersonation or cross-project writes.
			var actorID, projectIDStr string
			if req, ok := ctx.Value(pluginRequestKey{}).(*HTTPRequest); ok {
				actorID = req.UserID
				projectIDStr = req.ProjectID
			}

			if projectIDStr == "" {
				r.log.Warn("paca.activity_record: missing project context", "plugin", p.Name)
				stack[0] = 0
				return
			}
			projectID, err := uuid.Parse(projectIDStr)
			if err != nil {
				r.log.Warn("paca.activity_record: invalid project_id in context", "plugin", p.Name)
				stack[0] = 0
				return
			}

			// Require a non-empty actor_id so every activity has an
			// attributable author; an empty UserID indicates the request
			// is unauthenticated or the claim is missing.
			if actorID == "" {
				r.log.Warn("paca.activity_record: missing actor in context", "plugin", p.Name)
				stack[0] = 0
				return
			}

			// Verify the task belongs to the project derived from the request
			// context before writing to the activity stream.
			if r.services.DB == nil {
				r.log.Warn("paca.activity_record: DB not available", "plugin", p.Name)
				stack[0] = 0
				return
			}
			var exists bool
			if err := r.services.DB.QueryRowContext(ctx,
				`SELECT EXISTS(SELECT 1 FROM tasks WHERE id = $1 AND project_id = $2 AND deleted_at IS NULL)`,
				taskID, projectID).Scan(&exists); err != nil {
				r.log.Error("paca.activity_record: DB query failed",
					"plugin", p.Name, "task_id", taskID, "project_id", projectID, "error", err)
				stack[0] = 0
				return
			}
			if !exists {
				r.log.Warn("paca.activity_record: task not found in project",
					"plugin", p.Name, "task_id", taskID, "project_id", projectID)
				stack[0] = 0
				return
			}

			contentBytes, _ := json.Marshal(inp.Content)
			now := time.Now().UTC()
			activityID := uuid.New().String()
			payload := map[string]any{
				"id":            activityID,
				"task_id":       taskID.String(),
				"project_id":    projectID.String(),
				"activity_type": inp.ActivityType,
				"content":       string(contentBytes),
				"created_at":    now.Format(time.RFC3339Nano),
				"updated_at":    now.Format(time.RFC3339Nano),
			}
			payload["actor_id"] = actorID
			if r.services.Publisher != nil {
				_ = r.services.Publisher.Append(ctx, events.StreamTaskActivities, inp.ActivityType, payload)
				_ = r.services.Publisher.Publish(ctx, events.ChannelRealtime, map[string]any{
					"type":    inp.ActivityType,
					"payload": payload,
				})
			}
			stack[0] = 1
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64},
			[]api.ValueType{api.ValueTypeI32}).
		Export("activity_record")

	// paca.log(level i32, msgPtr, msgLen)
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(_ context.Context, m api.Module, stack []uint64) {
			level := int(stack[0])
			msg, _ := readString(m, stack[1], stack[2])
			switch level {
			case 0:
				r.log.Debug(msg, "plugin", p.Name)
			case 1:
				r.log.Info(msg, "plugin", p.Name)
			case 2:
				r.log.Warn(msg, "plugin", p.Name)
			default:
				r.log.Error(msg, "plugin", p.Name)
			}
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI64, api.ValueTypeI64}, nil).
		Export("log")

	// paca.config_get(keyPtr, keyLen, valuePtrPtr, valueLenPtr)
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(_ context.Context, m api.Module, stack []uint64) {
			key, err := readString(m, stack[0], stack[1])
			if err != nil || key == "" {
				m.Memory().WriteUint32Le(uint32(stack[2]), 0)
				m.Memory().WriteUint32Le(uint32(stack[3]), 0)
				return
			}
			if !isAllowedConfigKey(key, p.Manifest.Backend) {
				m.Memory().WriteUint32Le(uint32(stack[2]), 0)
				m.Memory().WriteUint32Le(uint32(stack[3]), 0)
				return
			}
			val, ok := r.services.Config[key]
			if !ok || val == "" {
				m.Memory().WriteUint32Le(uint32(stack[2]), 0)
				m.Memory().WriteUint32Le(uint32(stack[3]), 0)
				return
			}
			ptrLen, werr := writeToMemory(m, []byte(val))
			if werr != nil {
				m.Memory().WriteUint32Le(uint32(stack[2]), 0)
				m.Memory().WriteUint32Le(uint32(stack[3]), 0)
				return
			}
			m.Memory().WriteUint32Le(uint32(stack[2]), uint32(ptrLen[0]))
			m.Memory().WriteUint32Le(uint32(stack[3]), uint32(ptrLen[1]))
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64},
			nil).
		Export("config_get")
}

func isAllowedConfigKey(key string, backend *plugindom.BackendManifest) bool {
	if backend == nil || len(backend.AllowedConfigKeys) == 0 {
		return false
	}
	for _, allowed := range backend.AllowedConfigKeys {
		if allowed == key {
			return true
		}
	}
	return false
}

// -------------------------------------------------------------------------
// Outbound fetch host function (PLUG-BE-08)
// -------------------------------------------------------------------------

// registerFetchFunction registers the paca.fetch host function that allows
// plugins to make outbound HTTP requests to domains listed in their manifest.
func (r *Runtime) registerFetchFunction(b wazero.HostModuleBuilder, p plugindom.Plugin) {
	// paca.fetch(reqPtr, reqLen, resPtrPtr, resLenPtr)
	//   reqPtr/reqLen   – JSON-encoded fetchHostRequest in WASM memory
	//   resPtrPtr       – pointer to uint32 that receives response JSON ptr
	//   resLenPtr       – pointer to uint32 that receives response JSON len
	//
	// Response JSON: {"status":200,"body":"..."} on success, or
	//                {"status":0,"error":"<msg>"}   on transport error.
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			writeBack := func(ptrLen []uint64) {
				m.Memory().WriteUint32Le(uint32(stack[2]), uint32(ptrLen[0]))
				m.Memory().WriteUint32Le(uint32(stack[3]), uint32(ptrLen[1]))
			}
			writeErr := func(msg string) {
				type errResp struct {
					Status int    `json:"status"`
					Error  string `json:"error"`
				}
				writeBack(writeJSONResult(m, errResp{Status: 0, Error: msg}))
			}

			reqBytes, err := readFromMemory(m, stack[0], stack[1])
			if err != nil {
				writeErr("fetch: read request: " + err.Error())
				return
			}

			var req struct {
				Method  string            `json:"method"`
				URL     string            `json:"url"`
				Headers map[string]string `json:"headers"`
				Body    string            `json:"body"`
			}
			if err := json.Unmarshal(reqBytes, &req); err != nil {
				writeErr("fetch: decode request: " + err.Error())
				return
			}

			// Validate the target domain against the plugin's allowlist.
			if !isAllowedFetchDomain(ctx, req.URL, p.Manifest.Backend.AllowedOutboundDomains) {
				writeErr("fetch: domain not permitted by plugin manifest")
				return
			}

			// Execute the request via the shared HTTP client.
			httpClient := r.services.HTTPClient
			if httpClient == nil {
				httpClient = &http.Client{Timeout: 30 * time.Second}
			}

			var bodyReader io.Reader
			if req.Body != "" {
				bodyReader = strings.NewReader(req.Body)
			}

			method, ok := normalizeFetchMethod(req.Method)
			if !ok {
				writeErr("fetch: unsupported method")
				return
			}

			httpReq, err := http.NewRequestWithContext(ctx, method, req.URL, bodyReader)
			if err != nil {
				writeErr("fetch: build request: " + err.Error())
				return
			}
			for k, v := range req.Headers {
				if !isAllowedFetchHeader(k) {
					continue
				}
				httpReq.Header.Set(k, v)
			}

			resp, err := httpClient.Do(httpReq)
			if err != nil {
				writeErr("fetch: " + err.Error())
				return
			}
			defer func() {
				_ = resp.Body.Close()
			}()

			// Read one extra byte so we can detect and reject oversized payloads.
			respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchResponseBodySize+1))
			if err != nil {
				writeErr("fetch: read response body: " + err.Error())
				return
			}
			if len(respBody) > maxFetchResponseBodySize {
				writeErr(fmt.Sprintf("fetch: response body exceeds limit of %d bytes", maxFetchResponseBodySize))
				return
			}

			type successResp struct {
				Status  int               `json:"status"`
				Body    string            `json:"body"`
				Headers map[string]string `json:"headers"`
			}
			// Flatten response headers to first-value map.
			hdrs := make(map[string]string, len(resp.Header))
			for k, vv := range resp.Header {
				if len(vv) > 0 {
					hdrs[k] = vv[0]
				}
			}
			writeBack(writeJSONResult(m, successResp{
				Status:  resp.StatusCode,
				Body:    string(respBody),
				Headers: hdrs,
			}))
		}), []api.ValueType{api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64, api.ValueTypeI64},
			nil).
		Export("fetch")
}

// isAllowedFetchDomain reports whether rawURL's host is in the allowlist.
// An empty allowlist means no outbound requests are permitted. A literal "*"
// entry allows any HTTPS host (used by plugins like webhook integrations that
// must call user-supplied URLs); the scheme, hostname, and private/internal IP
// checks below still apply in that case.
func isAllowedFetchDomain(ctx context.Context, rawURL string, allowed []string) bool {
	if len(allowed) == 0 {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	if !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}

	host := parsed.Hostname() // strips port
	if host == "" {
		return false
	}

	hostAllowed := false
	for _, a := range allowed {
		a = strings.TrimSpace(a)
		if a == "*" || strings.EqualFold(host, a) {
			hostAllowed = true
			break
		}
	}
	if !hostAllowed {
		return false
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return false
	}

	for _, ipAddr := range ips {
		if isPrivateOrInternalIP(ipAddr.IP) {
			return false
		}
	}

	return true
}

func normalizeFetchMethod(raw string) (string, bool) {
	method := strings.ToUpper(strings.TrimSpace(raw))
	if method == "" {
		method = http.MethodGet
	}
	_, ok := allowedFetchMethods[method]
	return method, ok
}

func isAllowedFetchHeader(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return false
	}
	_, blocked := disallowedFetchHeaders[normalized]
	return !blocked
}

// -------------------------------------------------------------------------
// Memory helpers
// -------------------------------------------------------------------------

// writeToMemory allocates space in the WASM module's linear memory and writes
// data into it.  Returns [ptr, len] as uint64 values.
func writeToMemory(m api.Module, data []byte) ([]uint64, error) {
	if len(data) == 0 {
		return []uint64{0, 0}, nil
	}
	malloc := m.ExportedFunction("malloc")
	if malloc == nil {
		return nil, fmt.Errorf("plugin: malloc not exported")
	}
	results, err := malloc.Call(context.Background(), uint64(len(data)))
	if err != nil || len(results) == 0 {
		return nil, fmt.Errorf("plugin: malloc failed: %w", err)
	}
	ptr := results[0]
	if !m.Memory().Write(uint32(ptr), data) {
		return nil, fmt.Errorf("plugin: memory write out of bounds")
	}
	return []uint64{ptr, uint64(len(data))}, nil
}

// readFromMemory reads len bytes from the module's linear memory at ptr.
func readFromMemory(m api.Module, ptr, length uint64) ([]byte, error) {
	if length == 0 {
		return nil, nil
	}
	data, ok := m.Memory().Read(uint32(ptr), uint32(length))
	if !ok {
		return nil, fmt.Errorf("plugin: memory read out of bounds (ptr=%d, len=%d)", ptr, length)
	}
	out := make([]byte, length)
	copy(out, data)
	return out, nil
}

// readString reads a UTF-8 string from WASM linear memory.
func readString(m api.Module, ptr, length uint64) (string, error) {
	b, err := readFromMemory(m, ptr, length)
	return string(b), err
}

// writeJSONResult marshals v to JSON and writes it into WASM memory, returning
// the [ptr, len] pair expected by host function return conventions.
func writeJSONResult(m api.Module, v any) []uint64 {
	data, err := json.Marshal(v)
	if err != nil {
		return writeErrorResult(m, err)
	}
	ptrLen, err := writeToMemory(m, data)
	if err != nil {
		return []uint64{0, 0}
	}
	return ptrLen
}

// writeErrorResult writes an error JSON object into WASM memory.
func writeErrorResult(m api.Module, err error) []uint64 {
	data, _ := json.Marshal(map[string]string{"error": err.Error()})
	ptrLen, _ := writeToMemory(m, data)
	return ptrLen
}

// -------------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------------

// schemaName converts a reverse-DNS plugin name to a valid PostgreSQL schema
// name by replacing dots with underscores and prepending "plugin_data_".
// e.g. "com.paca.checklist" → "plugin_data_com_paca_checklist"
func schemaName(pluginName string) string {
	safe := strings.NewReplacer(".", "_", "-", "_").Replace(pluginName)
	return "plugin_data_" + safe
}

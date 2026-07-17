//go:build wasip1

// Stub backend for the frontend-only com.galaxy.analytics plugin (ADR-038
// P3.4, v1) — same pattern as com.galaxy.sdd (ADR-038 T6).
//
// Why this file exists at all: the Paca plugin runtime unconditionally loads
// {PLUGINS_WASM_DIR}/<plugin-id>/backend.wasm for every ENABLED plugin
// (services/api/internal/platform/plugin/runtime.go, Runtime.Load →
// Store.LoadWASM), and the install/enable HTTP handlers fail the request when
// that load errors. There is no frontend-only escape hatch in the host, so a
// frontend-only plugin still has to ship *a* loadable module.
//
// This module is deliberately inert:
//   - exports none of the ABI entry points the runtime calls conditionally
//     (Init / HandleRequest / HandleEvent / Shutdown are all looked up with a
//     nil-guard, so their absence is fine),
//   - registers no routes and subscribes to no events. Pair this with a
//     NON-NULL "backend": {} object in plugin.json: Runtime.EmitEvent walks
//     Manifest.Backend.EventSubscriptions without a nil check, so a manifest
//     that omits "backend" entirely would nil-panic the API on the first
//     domain event once the plugin is loaded.
//
// The analytics views do NOT need a backend: they call the same-origin
// /api/v1 REST endpoints from the browser with the caller's own session
// cookie, so the plugin holds no credentials and adds no server-side surface.
//
// Build (same command scripts/install-local-plugin.sh runs):
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o backend.wasm .
//
// c-shared produces a WASI reactor: wazero calls the exported _initialize on
// instantiation (Go runtime setup) and nothing else ever runs. If Go is not
// available, ../build.sh falls back to an 8-byte empty WASM module
// ("\x00asm\x01\x00\x00\x00"), which wazero also accepts — missing start
// functions are skipped (wazero runtime.go: `if start == nil { continue }`).
//
// No secrets, no permissions, no SDK dependency — keep it that way. Real
// backend behaviour (e.g. persisted burndown snapshots) belongs to v2.
package main

func main() {}

// Command echoplugin is a small WASI-reactor fixture used by the plugin
// runtime E2E tests to exercise a real wazero-loaded module through the full
// HTTP stack, instead of mocking the runtime.
//
// It serves a handful of hardcoded routes (matched on the incoming request's
// method+path) so tests can verify request/response plumbing end to end:
//
//	GET  .../hello   -> 200 {"message":"hello from plugin"}
//	GET  .../whoami  -> 200 {"caller_id":..,"user_id":..,"caller_role":..,"project_id":..}
//	GET  .../query   -> 200, the request's query params echoed back as a JSON object
//	POST .../echo    -> 200, body echoed back unchanged
//	anything else    -> 404 {"error":"not found"}
//
// Its malloc export reuses the same intentionally unsafe bump-allocator
// shape as services/api/internal/platform/plugin/testdata/poisonplugin: it
// advances its cursor with no bounds check, so a request larger than the
// module's actual memory still "succeeds" at the allocator level before the
// host's write fails. That lets the large-request E2E tests reproduce the
// allocator-poisoning bug (and its fix) through the real HTTP path, not just
// at the Runtime unit-test layer.
//
// Rebuild after editing with:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o ../echo.wasm .
package main

import (
	"encoding/json"
	"strings"
	"unsafe"
)

// arena is the plugin's entire "heap". Real plugin SDKs size this much
// larger; it's kept small here so tests can deliberately exceed it without
// needing a multi-hundred-MB request.
var arena [4 * 1024 * 1024]byte

var offset uint32

func arenaBase() uint32 {
	return uint32(uintptr(unsafe.Pointer(&arena[0])))
}

// malloc returns the current cursor and advances it by size with no check
// against the arena's actual capacity. See the package doc above.
//
//go:wasmexport paca_malloc
func malloc(size uint32) uint32 {
	ptr := arenaBase() + offset
	offset += size
	return ptr
}

// ResetAllocator restores the cursor, mirroring the per-call reset a real
// plugin SDK performs so its arena can be reused by the next request.
//
//go:wasmexport ResetAllocator
func resetAllocator() {
	offset = 0
}

func bytesAt(ptr, length uint32) []byte {
	if length == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), int(length))
}

// hostRequest mirrors the JSON shape the host writes -- see
// internal/platform/plugin/runtime.go's HTTPRequest struct.
type hostRequest struct {
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

// hostResponse mirrors the JSON shape the host expects back -- see the
// pluginResp struct in plugin_handler.go's ProxyRequest.
type hostResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    []byte            `json:"body"`
}

// HandleRequest ignores allocator failures by construction: it is only ever
// invoked after the host has already written a valid, in-bounds request
// payload, so reading reqPtr/reqLen here is always safe. The allocator's
// unsafe behavior only matters for the *next* call's malloc, not this one.
//
//go:wasmexport HandleRequest
func handleRequest(reqPtr uint32, reqLen uint32) uint64 {
	var req hostRequest
	_ = json.Unmarshal(bytesAt(reqPtr, reqLen), &req)

	resp := route(req)

	respBytes, err := json.Marshal(resp)
	if err != nil {
		respBytes = []byte(`{"status":500,"body":null}`)
	}

	respPtr := malloc(uint32(len(respBytes)))
	copy(bytesAt(respPtr, uint32(len(respBytes))), respBytes)
	return (uint64(respPtr) << 32) | uint64(len(respBytes))
}

// route matches on the last path segment rather than the whole path: the
// host passes the sub-path captured by its "/plugins/{pluginId}/*" wildcard,
// which arrives without a leading slash (e.g. "hello", not "/hello").
// Trimming defensively handles either convention.
func route(req hostRequest) hostResponse {
	path := strings.TrimPrefix(req.Path, "/")

	switch {
	case path == "hello" && req.Method == "GET":
		body, _ := json.Marshal(map[string]string{"message": "hello from plugin"})
		return hostResponse{Status: 200, Headers: jsonHeaders(), Body: body}

	case path == "whoami" && req.Method == "GET":
		body, _ := json.Marshal(map[string]string{
			"caller_id":   req.CallerID,
			"user_id":     req.UserID,
			"caller_role": req.CallerRole,
			"project_id":  req.ProjectID,
		})
		return hostResponse{Status: 200, Headers: jsonHeaders(), Body: body}

	case path == "echo" && req.Method == "POST":
		return hostResponse{Status: 200, Headers: jsonHeaders(), Body: req.Body}

	case path == "query" && req.Method == "GET":
		q := req.Query
		if q == nil {
			q = map[string]string{}
		}
		body, _ := json.Marshal(q)
		return hostResponse{Status: 200, Headers: jsonHeaders(), Body: body}

	default:
		body, _ := json.Marshal(map[string]string{"error": "not found"})
		return hostResponse{Status: 404, Headers: jsonHeaders(), Body: body}
	}
}

func jsonHeaders() map[string]string {
	return map[string]string{"Content-Type": "application/json"}
}

func main() {}

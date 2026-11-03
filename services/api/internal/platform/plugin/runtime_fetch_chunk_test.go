package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

const (
	fetchPluginName = "test.fetch"
	// fetchArenaSize mirrors arenaSize in testdata/fetchplugin/main.go.
	fetchArenaSize = 64 * 1024
	// fetchTestHost is a public IP literal so isAllowedFetchDomain passes
	// without a DNS lookup.
	fetchTestHost = "8.8.8.8"
)

var (
	fetchWasmOnce sync.Once
	fetchWasmPath string
	fetchWasmErr  error
)

// buildFetchFixture compiles testdata/fetchplugin on demand; see
// buildPoisonFixture for why the binary isn't committed.
func buildFetchFixture(t *testing.T) string {
	t.Helper()
	fetchWasmOnce.Do(func() {
		dir, err := os.MkdirTemp("", "fetchplugin-*")
		if err != nil {
			fetchWasmErr = err
			return
		}
		wd, err := os.Getwd()
		if err != nil {
			fetchWasmErr = err
			return
		}
		out := filepath.Join(dir, "fetch.wasm")
		cmd := exec.CommandContext(t.Context(), "go", "build", "-buildmode=c-shared", "-o", out, "./testdata/fetchplugin")
		cmd.Dir = wd
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
		if output, buildErr := cmd.CombinedOutput(); buildErr != nil {
			fetchWasmErr = fmt.Errorf("build fetch fixture: %w: %s", buildErr, output)
			return
		}
		fetchWasmPath = out
	})
	if fetchWasmErr != nil {
		t.Fatalf("build fetch fixture: %v", fetchWasmErr)
	}
	return fetchWasmPath
}

// roundTripFunc lets a test serve canned HTTP responses without a socket.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// loadFetchPlugin loads the fetch fixture into a Runtime whose HTTP client
// answers every request with a body of bodySize bytes.
func loadFetchPlugin(t *testing.T, bodySize *int) *pluginInstance {
	t.Helper()
	wasmBytes, err := os.ReadFile(buildFetchFixture(t))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, fetchPluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "backend.wasm"), wasmBytes, 0o644); err != nil {
		t.Fatalf("write wasm: %v", err)
	}

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(patternBody(*bodySize))),
			Header:     http.Header{},
			Request:    r,
		}, nil
	})}
	store := &Store{cfg: StoreConfig{Store: "local", WASMDir: dir}}
	rt := NewRuntime(store, HostServices{HTTPClient: client}, DefaultResourceLimits(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	p := plugindom.Plugin{
		Name:    fetchPluginName,
		Enabled: true,
		Manifest: plugindom.PluginManifest{
			Backend: &plugindom.BackendManifest{AllowedOutboundDomains: []string{fetchTestHost}},
		},
	}
	ctx := context.Background()
	if err := rt.Load(ctx, p); err != nil {
		t.Fatalf("load fixture plugin: %v", err)
	}
	t.Cleanup(func() { rt.Unload(ctx, fetchPluginName) })
	return instanceFor(rt, fetchPluginName)
}

// patternBody is n bytes of position-dependent ASCII, so reassembly tests
// notice dropped, duplicated or reordered chunks.
func patternBody(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return string(b)
}

type fetchEnvelope struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
	Error  string `json:"error"`
	Code   string `json:"code"`

	BodyHandle string `json:"body_handle"`
	BodySize   int    `json:"body_size"`
	Chunk      []byte `json:"chunk"`
	Done       bool   `json:"done"`
}

// callFetch sends one fetch request through the fixture and returns the
// decoded envelope plus its length. A zero length means the host delivered
// nothing -- the symptom in the bug report.
func callFetch(t *testing.T, inst *pluginInstance) (env fetchEnvelope, reqLen, resLen uint32) {
	t.Helper()
	return callFetchReq(t, inst, map[string]any{"method": "GET", "url": "https://" + fetchTestHost + "/x"})
}

// callFetchReq is callFetch with an arbitrary request object.
func callFetchReq(t *testing.T, inst *pluginInstance, req map[string]any) (env fetchEnvelope, reqLen, resLen uint32) {
	t.Helper()
	reqJSON, _ := json.Marshal(req)
	ptrLen, err := writeToMemory(inst.mod, reqJSON)
	if err != nil {
		t.Fatalf("write request: %v", err)
	}
	results, err := inst.mod.ExportedFunction("Fetch").Call(context.Background(), ptrLen[0], ptrLen[1])
	if err != nil {
		t.Fatalf("call Fetch: %v", err)
	}
	resPtr, resLen := uint32(results[0]>>32), uint32(results[0]&0xFFFFFFFF)
	if resLen > 0 {
		data, ok := inst.mod.Memory().Read(resPtr, resLen)
		if !ok {
			t.Fatalf("read envelope out of bounds")
		}
		if err := json.Unmarshal(data, &env); err != nil {
			t.Fatalf("decode envelope %q: %v", data[:min(len(data), 200)], err)
		}
	}
	resetFixture(t, inst)
	return env, uint32(len(reqJSON)), resLen
}

// TestFetch_ResponseArenaBoundary pins the exact edge from the bug report:
// an envelope that fits in the space left after the request succeeds, and one
// byte more is reported as response_too_large -- never as an empty response.
func TestFetch_ResponseArenaBoundary(t *testing.T) {
	bodySize := 0
	inst := loadFetchPlugin(t, &bodySize)

	// Calibrate the envelope overhead (status + headers JSON) with an empty body.
	_, reqLen, emptyLen := callFetch(t, inst)
	if emptyLen == 0 {
		t.Fatal("control fetch returned an empty response")
	}
	capacity := fetchArenaSize - int(reqLen)
	fitBody := capacity - int(emptyLen)

	bodySize = fitBody
	env, _, resLen := callFetch(t, inst)
	if env.Error != "" || resLen != uint32(capacity) || len(env.Body) != fitBody {
		t.Fatalf("envelope at capacity: err=%q resLen=%d (want %d) body=%d", env.Error, resLen, capacity, len(env.Body))
	}

	bodySize = fitBody + 1
	env, _, resLen = callFetch(t, inst)
	if resLen == 0 {
		t.Fatal("oversized envelope produced an empty response; want a response_too_large error")
	}
	if !strings.Contains(env.Error, `"chunked": true`) || strings.Contains(env.Error, "smaller page size") {
		t.Fatalf("a fetch error must point at chunked reads, not at a smaller page: %q", env.Error)
	}
	if env.Code != errCodeResponseTooLarge || !strings.Contains(env.Error, "does not fit") {
		t.Fatalf("oversized envelope: code=%q error=%q", env.Code, env.Error)
	}
	if want := fmt.Sprint(capacity + 1); !strings.Contains(env.Error, want) {
		t.Fatalf("error %q should report the %s-byte envelope size", env.Error, want)
	}
}

// TestFetch_OversizedResponse_AllocatorStillUsable checks that a response far
// larger than the arena yields the size-limit error and leaves the plugin
// able to serve the next, small fetch.
func TestFetch_OversizedResponse_AllocatorStillUsable(t *testing.T) {
	bodySize := 5 * fetchArenaSize
	inst := loadFetchPlugin(t, &bodySize)

	env, _, resLen := callFetch(t, inst)
	if resLen == 0 || env.Code != errCodeResponseTooLarge {
		t.Fatalf("want response_too_large, got resLen=%d code=%q error=%q", resLen, env.Code, env.Error)
	}

	bodySize = 10
	env, _, resLen = callFetch(t, inst)
	if resLen == 0 || env.Error != "" || env.Body != patternBody(10) {
		t.Fatalf("follow-up fetch failed: resLen=%d error=%q", resLen, env.Error)
	}
}

// TestFetch_PriorAllocationsReduceCapacity covers the "prior host allocations
// consume arena space" half of the report: the same body that fits on a fresh
// arena is rejected once earlier allocations are still live.
func TestFetch_PriorAllocationsReduceCapacity(t *testing.T) {
	bodySize := 0
	inst := loadFetchPlugin(t, &bodySize)
	_, reqLen, emptyLen := callFetch(t, inst)
	bodySize = fetchArenaSize - int(reqLen) - int(emptyLen) - 100 // fits fresh

	if _, err := writeToMemory(inst.mod, make([]byte, 1000)); err != nil { // prior allocation, not reset
		t.Fatalf("prior allocation: %v", err)
	}
	env, _, resLen := callFetch(t, inst)
	if resLen == 0 || env.Code != errCodeResponseTooLarge {
		t.Fatalf("want response_too_large with a partly used arena, got resLen=%d code=%q", resLen, env.Code)
	}
}

// fetchAllChunks drains a body_handle in limit-byte pieces and returns the
// reassembled body.
func fetchAllChunks(t *testing.T, inst *pluginInstance, handle string, size, limit int) string {
	t.Helper()
	var got []byte
	for offset := 0; ; {
		env, _, resLen := callFetchReq(t, inst, map[string]any{"body_handle": handle, "offset": offset, "limit": limit})
		if resLen == 0 || env.Error != "" {
			t.Fatalf("chunk at offset %d: resLen=%d error=%q", offset, resLen, env.Error)
		}
		if env.BodySize != size {
			t.Fatalf("body_size = %d, want %d", env.BodySize, size)
		}
		got = append(got, env.Chunk...)
		offset += len(env.Chunk)
		if env.Done {
			return string(got)
		}
	}
}

// TestFetch_Chunked_ReadsBodyLargerThanArena is the end-to-end contract: a
// body several times the arena arrives intact when the request opts in, and
// the handle is released after the last chunk.
func TestFetch_Chunked_ReadsBodyLargerThanArena(t *testing.T) {
	bodySize := 5*fetchArenaSize + 123
	inst := loadFetchPlugin(t, &bodySize)

	env, _, resLen := callFetchReq(t, inst, map[string]any{"method": "GET", "url": "https://" + fetchTestHost + "/x", "chunked": true})
	if resLen == 0 || env.Error != "" || env.BodyHandle == "" || env.BodySize != bodySize || env.Status != 200 {
		t.Fatalf("chunked meta: resLen=%d status=%d handle=%q size=%d error=%q", resLen, env.Status, env.BodyHandle, env.BodySize, env.Error)
	}
	if env.Body != "" {
		t.Fatalf("meta envelope should not carry the body, got %d bytes", len(env.Body))
	}

	// 16 KiB raw -> ~22 KiB base64: well inside the 64 KiB arena.
	if got := fetchAllChunks(t, inst, env.BodyHandle, bodySize, 16*1024); got != patternBody(bodySize) {
		t.Fatalf("reassembled body differs (len %d, want %d)", len(got), bodySize)
	}

	again, _, _ := callFetchReq(t, inst, map[string]any{"body_handle": env.BodyHandle, "offset": 0, "limit": 10})
	if again.Error == "" {
		t.Fatal("handle should be released after the final chunk")
	}
}

// TestFetch_Chunked_SmallBodyStaysInline keeps the common case one round trip
// and byte-for-byte compatible with a non-chunked request.
func TestFetch_Chunked_SmallBodyStaysInline(t *testing.T) {
	bodySize := 100
	inst := loadFetchPlugin(t, &bodySize)
	env, _, _ := callFetchReq(t, inst, map[string]any{"method": "GET", "url": "https://" + fetchTestHost + "/x", "chunked": true})
	if env.BodyHandle != "" || env.Body != patternBody(100) {
		t.Fatalf("small chunked fetch should be inline: handle=%q body=%d bytes", env.BodyHandle, len(env.Body))
	}
}

// TestFetch_NotChunked_OversizedStillExplicitError guards old SDKs, which do
// not send "chunked": they keep getting response_too_large.
func TestFetch_NotChunked_OversizedStillExplicitError(t *testing.T) {
	bodySize := 3 * fetchArenaSize
	inst := loadFetchPlugin(t, &bodySize)
	env, _, _ := callFetch(t, inst)
	if env.Code != errCodeResponseTooLarge || env.BodyHandle != "" {
		t.Fatalf("code=%q handle=%q", env.Code, env.BodyHandle)
	}
}

// TestFetch_Chunk_OversizedLimitIsRetryable: a chunk that does not fit the
// arena reports response_too_large but keeps the handle, so a smaller limit
// succeeds -- including when it is the final chunk.
func TestFetch_Chunk_OversizedLimitIsRetryable(t *testing.T) {
	bodySize := 4 * fetchArenaSize
	inst := loadFetchPlugin(t, &bodySize)
	meta, _, _ := callFetchReq(t, inst, map[string]any{"method": "GET", "url": "https://" + fetchTestHost + "/x", "chunked": true})

	env, _, resLen := callFetchReq(t, inst, map[string]any{"body_handle": meta.BodyHandle, "offset": 0, "limit": bodySize})
	if resLen == 0 || env.Code != errCodeResponseTooLarge {
		t.Fatalf("oversized chunk: resLen=%d code=%q", resLen, env.Code)
	}
	if !strings.Contains(env.Error, "smaller limit") {
		t.Fatalf("a chunk error must tell the caller to ask for less: %q", env.Error)
	}
	if got := fetchAllChunks(t, inst, meta.BodyHandle, bodySize, 16*1024); got != patternBody(bodySize) {
		t.Fatal("body differs after retry with a smaller limit")
	}
}

func TestFetchChunkStore(t *testing.T) {
	now := time.Unix(0, 0)
	s := newFetchChunkStore()
	s.now = func() time.Time { return now }

	a := s.put([]byte("aaaa"))
	if _, _, _, err := s.read(a, 5, 1); err == nil {
		t.Error("offset past end should error")
	}
	if _, _, _, err := s.read("nope", 0, 1); err == nil {
		t.Error("unknown handle should error")
	}

	// Eviction: the oldest handle goes when the cap is exceeded.
	b := s.put([]byte("bbbb"))
	c := s.put([]byte("cccc"))
	if _, _, _, err := s.read(a, 0, 1); err == nil {
		t.Error("oldest handle should have been evicted")
	}
	if _, _, _, err := s.read(b, 0, 1); err != nil {
		t.Errorf("second handle should survive: %v", err)
	}

	// TTL: unread bodies expire.
	now = now.Add(fetchChunkTTL + time.Second)
	if _, _, _, err := s.read(c, 0, 1); err == nil {
		t.Error("expired handle should error")
	}
}

// The hint depends on what the caller can change: a page size for a query, a
// chunked read for a fetch, a smaller limit for a chunk.
func TestResponseTooLargeMessage_NamesTheRemedyForEachOperation(t *testing.T) {
	cases := map[string]struct{ got, want, notWant string }{
		"db_query":     {responseTooLargeMessage("db_query", 10, remedyLessData), "smaller page size", `"chunked"`},
		"settings_get": {responseTooLargeMessage("settings_get", 10, remedyLessData), "smaller page size", `"chunked"`},
		"fetch":        {responseTooLargeMessage("fetch", 10, remedyChunked), `"chunked": true`, "smaller page size"},
		"fetch chunk":  {responseTooLargeMessage("fetch", 10, remedySmallerChunk), "smaller limit", "smaller page size"},
		"fetch header": {responseTooLargeMessage("fetch", 10, remedyHeaders), "headers alone", "smaller page size"},
	}
	for name, c := range cases {
		if !strings.Contains(c.got, "does not fit") || !strings.Contains(c.got, "10 bytes") {
			t.Errorf("%s: %q", name, c.got)
		}
		if !strings.Contains(c.got, c.want) || strings.Contains(c.got, c.notWant) {
			t.Errorf("%s: want %q and not %q in %q", name, c.want, c.notWant, c.got)
		}
	}
}

// Command poisonplugin is a minimal WASI-reactor fixture used by
// runtime_test.go to exercise Runtime.HandleRequest against a real wazero
// module instead of mocks.
//
// Its malloc export deliberately reproduces the allocator shape that a real
// plugin SDK uses: a bump allocator that advances its cursor unconditionally,
// with no bounds check against the module's actual memory size. That is what
// lets an oversized request "poison" a plugin instance — see runtime.go's
// HandleRequest doc comment for the full mechanism. This fixture lets the
// test trigger that failure deterministically without depending on the real
// (externally hosted) plugin-sdk-go allocator implementation.
//
// Rebuild after editing with:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared \
//	  -o ../poison.wasm .
package main

// offset is the bump allocator's cursor. Starting it away from 0 mirrors a
// real plugin reserving low memory for its own static data.
var offset uint32 = 4096

// malloc returns the current cursor and advances it by size, with no check
// against the module's actual memory size. The host (Runtime.HandleRequest)
// is responsible for rejecting writes that would land out of bounds; this
// fixture intentionally does not protect itself, since the bug under test is
// about the host's behavior when that happens.
//
//go:wasmexport paca_malloc
func malloc(size uint32) uint32 {
	ptr := offset
	offset += size
	return ptr
}

// ResetAllocator restores the cursor, simulating the per-call reset a real
// plugin SDK performs so its arena can be reused by the next request.
//
//go:wasmexport ResetAllocator
func resetAllocator() {
	offset = 4096
}

// HandleRequest ignores the request payload and reports an empty response
// (outPtr=0, outLen=0), which the host reads without touching memory. The
// test only cares whether this call succeeds, not what it returns.
//
//go:wasmexport HandleRequest
func handleRequest(ptr uint32, length uint32) uint64 {
	return 0
}

// HandleEvent mirrors HandleRequest for the event-dispatch path.
//
//go:wasmexport HandleEvent
func handleEvent(topicPtr uint32, topicLen uint32, payloadPtr uint32, payloadLen uint32) {
}

func main() {}

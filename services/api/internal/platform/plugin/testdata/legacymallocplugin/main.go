// Command legacymallocplugin is a minimal WASI-reactor fixture used by
// runtime_test.go to pin writeToMemory's backward-compatibility fallback: it
// exports only "malloc" (the pre-rename name), never "paca_malloc", the same
// shape as every plugin binary compiled against the plugin-sdk-go version
// before the allocator export was renamed. If the fallback in writeToMemory
// ever regresses, every already-deployed plugin like this one stops working
// the moment the host is redeployed — this fixture makes that failure show
// up in CI instead of in production.
//
// Rebuild after editing with:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared \
//	  -o ../legacymalloc.wasm .
package main

import "unsafe"

// mallocBuf backs a simple bounds-checked bump allocator -- unlike
// poisonplugin, this fixture isn't testing the host's out-of-bounds
// recovery, so it protects itself normally the way a real SDK allocator
// does.
var mallocBuf [4096]byte
var mallocOffset uint32
var mallocBase uint32

//go:wasmexport malloc
func malloc(size uint32) uint32 {
	if mallocBase == 0 {
		mallocBase = uint32(uintptr(unsafe.Pointer(&mallocBuf[0])))
	}
	if mallocOffset+size > uint32(len(mallocBuf)) {
		return 0
	}
	ptr := mallocBase + mallocOffset
	mallocOffset += size
	return ptr
}

//go:wasmexport ResetAllocator
func resetAllocator() {
	mallocOffset = 0
}

// HandleRequest ignores the request payload and reports an empty response
// (outPtr=0, outLen=0). The test only cares whether the call into this
// export succeeds at all -- i.e. whether the host could write the request
// payload into this module's memory via its legacy "malloc" export.
//
//go:wasmexport HandleRequest
func handleRequest(ptr uint32, length uint32) uint64 {
	return 0
}

//go:wasmexport HandleEvent
func handleEvent(topicPtr uint32, topicLen uint32, payloadPtr uint32, payloadLen uint32) {
}

func main() {}

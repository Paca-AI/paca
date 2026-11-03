// Command fetchplugin is a minimal WASI-reactor fixture used by
// runtime_fetch_test.go to exercise the paca.fetch host function against a
// real wazero module. Like testdata/cacheplugin it talks to the host imports
// directly, but with a deliberately small, bounds-checked arena (the real
// SDK's is 1 MiB) so tests can hit the "response does not fit" boundary with
// kilobytes instead of megabytes.
//
// Rebuild after editing with:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared \
//	  -o ../fetch.wasm .
package main

import "unsafe"

// ArenaSize is mirrored by runtime_fetch_test.go.
const arenaSize = 64 * 1024

var mallocBuf [arenaSize]byte
var mallocOffset uint32
var mallocBase uint32

//go:wasmexport paca_malloc
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

// paca.fetch(reqPtr, reqLen, resPtrPtr, resLenPtr)
//
//go:wasmimport paca fetch
func hostFetch(reqPtr, reqLen, resPtrPtr, resLenPtr int64)

var outBuf [8]byte

// Fetch forwards the JSON request at [reqPtr:reqPtr+reqLen] to paca.fetch and
// returns (resPtr<<32 | resLen) of the envelope the host wrote back; a zero
// length means the host delivered nothing.
//
//go:wasmexport Fetch
func Fetch(reqPtr, reqLen uint32) uint64 {
	outPtr := uint32(uintptr(unsafe.Pointer(&outBuf[0])))
	hostFetch(int64(reqPtr), int64(reqLen), int64(outPtr), int64(outPtr+4))
	resPtr := uint32(outBuf[0]) | uint32(outBuf[1])<<8 | uint32(outBuf[2])<<16 | uint32(outBuf[3])<<24
	resLen := uint32(outBuf[4]) | uint32(outBuf[5])<<8 | uint32(outBuf[6])<<16 | uint32(outBuf[7])<<24
	return (uint64(resPtr) << 32) | uint64(resLen)
}

//go:wasmexport HandleRequest
func handleRequest(ptr uint32, length uint32) uint64 { return 0 }

//go:wasmexport HandleEvent
func handleEvent(topicPtr, topicLen, payloadPtr, payloadLen uint32) {}

func main() {}

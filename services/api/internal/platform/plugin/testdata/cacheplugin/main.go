// Command cacheplugin is a minimal WASI-reactor fixture used by
// runtime_test.go to exercise the paca.cache_get/cache_set/cache_delete host
// functions against a real wazero module instead of mocks. It talks to the
// host imports directly (no plugin-sdk-go dependency), the same way
// testdata/poisonplugin does for malloc/HandleRequest.
//
// Rebuild after editing with:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared \
//	  -o ../cache.wasm .
package main

import "unsafe"

// mallocBuf backs this fixture's bump allocator. 4 KiB is far more than the
// small keys/values these tests exchange.
var mallocBuf [4096]byte
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

// paca.cache_get(keyPtr i64, keyLen i64, valuePtrPtr i64, valueLenPtr i64)
//
//go:wasmimport paca cache_get
func hostCacheGet(keyPtr, keyLen, valuePtrPtr, valueLenPtr int64)

// paca.cache_set(keyPtr i64, keyLen i64, valuePtr i64, valueLen i64, ttlSeconds i32) -> ok i32
//
//go:wasmimport paca cache_set
func hostCacheSet(keyPtr, keyLen, valuePtr, valueLen int64, ttlSeconds int32) int32

// paca.cache_delete(keyPtr i64, keyLen i64) -> ok i32
//
//go:wasmimport paca cache_delete
func hostCacheDelete(keyPtr, keyLen int64) int32

// getOutBuf receives the [valuePtr, valueLen] pair the host writes back for
// cache_get, mirroring plugin-sdk-go's own wasm_backends.go pattern of a
// fixed package-level buffer rather than a stack local (so its address is
// stable linear-memory, not something that could get optimized away).
var getOutBuf [8]byte

// CacheSet stores the value at [valPtr:valPtr+valLen] under the key at
// [keyPtr:keyPtr+keyLen] with the given TTL in seconds (0 = no expiry).
// Returns 1 on success, 0 on failure.
//
//go:wasmexport CacheSet
func CacheSet(keyPtr, keyLen, valPtr, valLen uint32, ttlSeconds int32) int32 {
	return hostCacheSet(int64(keyPtr), int64(keyLen), int64(valPtr), int64(valLen), ttlSeconds)
}

// CacheGet reads the key at [keyPtr:keyPtr+keyLen] and returns
// (valuePtr<<32 | valueLen); a zero length means "miss".
//
//go:wasmexport CacheGet
func CacheGet(keyPtr, keyLen uint32) uint64 {
	outPtr := uint32(uintptr(unsafe.Pointer(&getOutBuf[0])))
	hostCacheGet(int64(keyPtr), int64(keyLen), int64(outPtr), int64(outPtr+4))
	valPtr := uint32(getOutBuf[0]) | uint32(getOutBuf[1])<<8 | uint32(getOutBuf[2])<<16 | uint32(getOutBuf[3])<<24
	valLen := uint32(getOutBuf[4]) | uint32(getOutBuf[5])<<8 | uint32(getOutBuf[6])<<16 | uint32(getOutBuf[7])<<24
	return (uint64(valPtr) << 32) | uint64(valLen)
}

// CacheDelete removes the key at [keyPtr:keyPtr+keyLen]. Returns 1 on
// success, 0 on failure.
//
//go:wasmexport CacheDelete
func CacheDelete(keyPtr, keyLen uint32) int32 {
	return hostCacheDelete(int64(keyPtr), int64(keyLen))
}

//go:wasmexport HandleRequest
func handleRequest(ptr uint32, length uint32) uint64 {
	return 0
}

//go:wasmexport HandleEvent
func handleEvent(topicPtr uint32, topicLen uint32, payloadPtr uint32, payloadLen uint32) {
}

func main() {}

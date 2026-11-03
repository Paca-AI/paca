package plugin

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Chunked fetch lets a plugin read an HTTP response larger than its fixed
// host-allocation arena. When a request sets "chunked": true and the full
// response envelope would not fit, paca.fetch parks the body host-side and
// returns only status/headers plus a body_handle and body_size. The plugin
// then calls paca.fetch again with {"body_handle","offset","limit"} to pull
// the body one piece at a time. Each chunk travels as base64 inside the JSON
// envelope (binary-safe, unlike a JSON string), and the handle is released
// when the last byte is read.
const (
	// maxFetchChunkHandles bounds how many parked bodies one plugin may hold
	// (each up to maxFetchResponseBodySize). The oldest is evicted first.
	maxFetchChunkHandles = 2
	// fetchChunkTTL bounds how long an unread body stays parked.
	fetchChunkTTL = 60 * time.Second
	// maxFetchChunkLimit caps a single chunk request regardless of what the
	// guest asks for; the guest's arena is the real bound.
	maxFetchChunkLimit = 1 << 20
)

type parkedBody struct {
	data    []byte
	expires time.Time
}

// fetchChunkStore holds the response bodies parked for chunked reads. One
// store exists per loaded plugin (it lives in registerFetchFunction's
// closure), so plugins cannot read each other's handles.
type fetchChunkStore struct {
	mu     sync.Mutex
	bodies map[string]*parkedBody
	order  []string // insertion order, for eviction
	now    func() time.Time
}

func newFetchChunkStore() *fetchChunkStore {
	return &fetchChunkStore{bodies: map[string]*parkedBody{}, now: time.Now}
}

// put parks body and returns its handle, evicting expired and then oldest
// entries to stay within maxFetchChunkHandles.
func (s *fetchChunkStore) put(body []byte) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	for len(s.order) >= maxFetchChunkHandles {
		s.dropLocked(s.order[0])
	}
	var raw [12]byte
	_, _ = rand.Read(raw[:])
	id := hex.EncodeToString(raw[:])
	s.bodies[id] = &parkedBody{data: body, expires: s.now().Add(fetchChunkTTL)}
	s.order = append(s.order, id)
	return id
}

// read returns up to limit bytes of the body at offset, the body's total
// size, and whether this read reaches the end. The handle is not released
// here: the caller calls release once the chunk was actually delivered, so a
// failed delivery can be retried.
func (s *fetchChunkStore) read(id string, offset, limit int) (chunk []byte, total int, done bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	b, ok := s.bodies[id]
	if !ok {
		return nil, 0, false, fmt.Errorf("fetch: unknown or expired body_handle")
	}
	total = len(b.data)
	if offset < 0 || offset > total {
		return nil, total, false, fmt.Errorf("fetch: offset %d out of range (body is %d bytes)", offset, total)
	}
	if limit <= 0 || limit > maxFetchChunkLimit {
		limit = maxFetchChunkLimit
	}
	end := min(offset+limit, total)
	return b.data[offset:end], total, end == total, nil
}

// release frees a parked body; unknown handles are ignored.
func (s *fetchChunkStore) release(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked(id)
}

func (s *fetchChunkStore) sweepLocked() {
	now := s.now()
	for _, id := range append([]string(nil), s.order...) {
		if now.After(s.bodies[id].expires) {
			s.dropLocked(id)
		}
	}
}

func (s *fetchChunkStore) dropLocked(id string) {
	delete(s.bodies, id)
	for i, o := range s.order {
		if o == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

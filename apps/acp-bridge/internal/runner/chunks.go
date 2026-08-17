package runner

import "sync"

// chunkBuffer paragraph-buffers streamed text per session/update kind
// (agent_message_chunk, agent_thought_chunk) so a reply isn't persisted and
// broadcast one tiny fragment at a time — mirrors
// services/agent-runner/internal/handler/handler.go's identical
// flushChunkBuf pattern for the Goose-in-sandbox path. Kept separate per
// kind since reasoning and reply text are semantically distinct and must
// never be concatenated into the same event.
type chunkBuffer struct {
	mu   sync.Mutex
	text map[string]*[]byte
}

func newChunkBuffer() *chunkBuffer {
	return &chunkBuffer{text: make(map[string]*[]byte)}
}

// append adds text to kind's buffer.
func (b *chunkBuffer) append(kind, text string) {
	if text == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	buf, ok := b.text[kind]
	if !ok {
		empty := []byte{}
		buf = &empty
		b.text[kind] = buf
	}
	*buf = append(*buf, text...)
}

// take returns and clears whatever is currently buffered for kind ("" if
// nothing was pending).
func (b *chunkBuffer) take(kind string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	buf, ok := b.text[kind]
	if !ok || len(*buf) == 0 {
		return ""
	}
	out := string(*buf)
	*buf = (*buf)[:0]
	return out
}

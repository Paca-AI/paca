package docdom

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
)

// ContentHash returns a stable fingerprint of a document's BlockNote content:
// 64 lowercase hex characters (sha256). The same blocks give the same hash
// whatever their block ids, key order or whitespace, so two writers that send
// "the same document" agree even though one generates fresh ids per write
// (the MCP Markdown parser), another strips them (the web editor's
// cleanBlocks) and Postgres re-serializes the JSONB it stores.
//
// Only a block's own "id" is ignored: the key directly on an element of the
// top-level array or of a "children" array. Every other field counts,
// including an "id" inside props or on an inline content node, so the hash
// never hides a real edit.
//
// nil, empty, JSON null and [] all hash alike (as []). Content that does not
// parse falls back to the hash of its raw bytes; ContentHash never fails.
func ContentHash(content json.RawMessage) string {
	return hashBytes(canonicalContent(content))
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// canonicalContent returns the bytes ContentHash hashes: content with block
// ids removed, re-encoded with sorted keys and no insignificant whitespace.
func canonicalContent(content json.RawMessage) []byte {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 {
		return []byte("[]")
	}
	// UseNumber keeps numbers as their literal text, so values beyond float64
	// precision are neither rounded nor reformatted.
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return content
	}
	if _, err := dec.Token(); err != io.EOF {
		return content // trailing data: not a single JSON value
	}
	switch blocks := v.(type) {
	case nil:
		return []byte("[]")
	case []any:
		stripBlockIDs(blocks)
	}
	// encoding/json writes map keys in sorted order.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return content
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

// stripBlockIDs deletes the "id" key of each block object in blocks and of
// the blocks nested under "children", mirroring cleanBlocks in the web app.
func stripBlockIDs(blocks []any) {
	for _, b := range blocks {
		block, ok := b.(map[string]any)
		if !ok {
			continue
		}
		delete(block, "id")
		if children, ok := block["children"].([]any); ok {
			stripBlockIDs(children)
		}
	}
}

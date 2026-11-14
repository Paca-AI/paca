package docdom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestContentHash_IgnoresIDsKeyOrderWhitespace(t *testing.T) {
	a := json.RawMessage(`[{"id":"a","type":"paragraph","content":[{"type":"text","text":"hi","styles":{}}]}]`)
	b := json.RawMessage(`[ {"content":[{"styles":{},"text":"hi","type":"text"}],"type":"paragraph","id":"zzz"} ]`)
	ha, hb := ContentHash(a), ContentHash(b)
	if ha != hb {
		t.Fatalf("hashes differ: %s vs %s", ha, hb)
	}
	if len(ha) != 64 {
		t.Fatalf("hash length = %d, want 64", len(ha))
	}
	for _, c := range ha {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("hash %q is not lowercase hex", ha)
		}
	}
	// A block without an id at all (what the web editor stores) is the same content.
	noID := json.RawMessage(`[{"type":"paragraph","content":[{"type":"text","text":"hi","styles":{}}]}]`)
	if ContentHash(noID) != ha {
		t.Fatal("a block with its id stripped must hash like the block with an id")
	}
}

func TestContentHash_NestedChildrenIDsIgnored(t *testing.T) {
	a := json.RawMessage(`[{"id":"1","type":"bulletListItem","children":[{"id":"2","type":"bulletListItem","children":[{"id":"3","type":"paragraph"}]}]}]`)
	b := json.RawMessage(`[{"id":"1","type":"bulletListItem","children":[{"id":"x","type":"bulletListItem","children":[{"id":"y","type":"paragraph"}]}]}]`)
	if ContentHash(a) != ContentHash(b) {
		t.Fatal("ids inside children[].children[] must not change the hash")
	}
}

func TestContentHash_DetectsRealChanges(t *testing.T) {
	base := `[
		{"id":"a","type":"paragraph","props":{"textAlignment":"left"},"content":[{"type":"text","text":"one","styles":{}}],
		 "children":[{"id":"c1","type":"paragraph","content":[{"type":"text","text":"x","styles":{}}]},
		             {"id":"c2","type":"paragraph","content":[{"type":"text","text":"y","styles":{}}]}]},
		{"id":"b","type":"paragraph","props":{"textAlignment":"left"},"content":[{"type":"text","text":"two","styles":{}}]}
	]`
	cases := map[string]string{
		"changed text": `[
		{"id":"a","type":"paragraph","props":{"textAlignment":"left"},"content":[{"type":"text","text":"ONE","styles":{}}],
		 "children":[{"id":"c1","type":"paragraph","content":[{"type":"text","text":"x","styles":{}}]},
		             {"id":"c2","type":"paragraph","content":[{"type":"text","text":"y","styles":{}}]}]},
		{"id":"b","type":"paragraph","props":{"textAlignment":"left"},"content":[{"type":"text","text":"two","styles":{}}]}
	]`,
		"changed styles": `[
		{"id":"a","type":"paragraph","props":{"textAlignment":"left"},"content":[{"type":"text","text":"one","styles":{"bold":true}}],
		 "children":[{"id":"c1","type":"paragraph","content":[{"type":"text","text":"x","styles":{}}]},
		             {"id":"c2","type":"paragraph","content":[{"type":"text","text":"y","styles":{}}]}]},
		{"id":"b","type":"paragraph","props":{"textAlignment":"left"},"content":[{"type":"text","text":"two","styles":{}}]}
	]`,
		"changed props": `[
		{"id":"a","type":"paragraph","props":{"textAlignment":"right"},"content":[{"type":"text","text":"one","styles":{}}],
		 "children":[{"id":"c1","type":"paragraph","content":[{"type":"text","text":"x","styles":{}}]},
		             {"id":"c2","type":"paragraph","content":[{"type":"text","text":"y","styles":{}}]}]},
		{"id":"b","type":"paragraph","props":{"textAlignment":"left"},"content":[{"type":"text","text":"two","styles":{}}]}
	]`,
		"changed type": `[
		{"id":"a","type":"heading","props":{"textAlignment":"left"},"content":[{"type":"text","text":"one","styles":{}}],
		 "children":[{"id":"c1","type":"paragraph","content":[{"type":"text","text":"x","styles":{}}]},
		             {"id":"c2","type":"paragraph","content":[{"type":"text","text":"y","styles":{}}]}]},
		{"id":"b","type":"paragraph","props":{"textAlignment":"left"},"content":[{"type":"text","text":"two","styles":{}}]}
	]`,
		"swapped block order": `[
		{"id":"b","type":"paragraph","props":{"textAlignment":"left"},"content":[{"type":"text","text":"two","styles":{}}]},
		{"id":"a","type":"paragraph","props":{"textAlignment":"left"},"content":[{"type":"text","text":"one","styles":{}}],
		 "children":[{"id":"c1","type":"paragraph","content":[{"type":"text","text":"x","styles":{}}]},
		             {"id":"c2","type":"paragraph","content":[{"type":"text","text":"y","styles":{}}]}]}
	]`,
		"swapped child order": `[
		{"id":"a","type":"paragraph","props":{"textAlignment":"left"},"content":[{"type":"text","text":"one","styles":{}}],
		 "children":[{"id":"c2","type":"paragraph","content":[{"type":"text","text":"y","styles":{}}]},
		             {"id":"c1","type":"paragraph","content":[{"type":"text","text":"x","styles":{}}]}]},
		{"id":"b","type":"paragraph","props":{"textAlignment":"left"},"content":[{"type":"text","text":"two","styles":{}}]}
	]`,
	}
	want := ContentHash(json.RawMessage(base))
	for name, changed := range cases {
		t.Run(name, func(t *testing.T) {
			if ContentHash(json.RawMessage(changed)) == want {
				t.Fatalf("%s must change the hash", name)
			}
		})
	}
}

func TestContentHash_KeepsNonBlockIDFields(t *testing.T) {
	a := json.RawMessage(`[{"id":"a","type":"image","props":{"id":"file-1"}}]`)
	b := json.RawMessage(`[{"id":"a","type":"image","props":{"id":"file-2"}}]`)
	if ContentHash(a) == ContentHash(b) {
		t.Fatal("an id inside props is content and must change the hash")
	}
	// Inline content is not a block either: its id (e.g. a mention's) counts.
	c := json.RawMessage(`[{"id":"a","type":"paragraph","content":[{"type":"mention","id":"u1"}]}]`)
	d := json.RawMessage(`[{"id":"a","type":"paragraph","content":[{"type":"mention","id":"u2"}]}]`)
	if ContentHash(c) == ContentHash(d) {
		t.Fatal("an id on an inline content node must change the hash")
	}
}

func TestContentHash_EmptyForms(t *testing.T) {
	want := ContentHash(json.RawMessage(`[]`))
	for name, in := range map[string]json.RawMessage{
		"nil":        nil,
		"empty":      json.RawMessage(``),
		"null":       json.RawMessage(`null`),
		"spaced":     json.RawMessage(` [ ] `),
		"spacedNull": json.RawMessage(" null\n"),
	} {
		if got := ContentHash(in); got != want {
			t.Errorf("%s: hash %s, want the hash of [] (%s)", name, got, want)
		}
	}
}

func TestContentHash_BigNumbersExact(t *testing.T) {
	a := json.RawMessage(`[{"type":"x","props":{"n":9007199254740993}}]`)
	b := json.RawMessage(`[{"type":"x","props":{"n":9007199254740992}}]`)
	if ContentHash(a) == ContentHash(b) {
		t.Fatal("numbers beyond float64 precision must keep their exact value")
	}
}

func TestContentHash_InvalidJSONFallsBack(t *testing.T) {
	for _, raw := range []string{`{bad`, `[{"type":"p"}] trailing`} {
		sum := sha256.Sum256([]byte(raw))
		if got, want := ContentHash(json.RawMessage(raw)), hex.EncodeToString(sum[:]); got != want {
			t.Errorf("ContentHash(%q) = %s, want the sha256 of the raw bytes %s", raw, got, want)
		}
	}
}

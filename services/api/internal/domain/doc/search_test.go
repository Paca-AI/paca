package docdom

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const sampleContent = `[
 {"id":"1","type":"heading","props":{},"content":[{"type":"text","text":"Deploy guide"}],"children":[]},
 {"id":"2","type":"paragraph","props":{},"content":[{"type":"text","text":"Run the migration before restarting the API."}],
  "children":[{"id":"3","type":"paragraph","content":[{"type":"text","text":"Nested note"}],"children":[]}]}
]`

func TestExtractPlainText_IgnoresStructure(t *testing.T) {
	got := ExtractPlainText(json.RawMessage(sampleContent))
	for _, want := range []string{"Deploy guide", "Run the migration", "Nested note"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "paragraph") {
		t.Errorf("structural key leaked into text: %q", got)
	}
	if ExtractPlainText(nil) != "" || ExtractPlainText(json.RawMessage("not json")) != "" {
		t.Error("empty/invalid content should yield empty text")
	}
}

func TestBuildSnippet(t *testing.T) {
	text := strings.Repeat("a ", 100) + "Needle here " + strings.Repeat("b ", 100)
	s := BuildSnippet(text, "needle")
	if !strings.Contains(s, "Needle here") || !strings.HasPrefix(s, "…") || !strings.HasSuffix(s, "…") {
		t.Errorf("unexpected snippet %q", s)
	}
	if BuildSnippet("short text", "zzz") != "" {
		t.Error("no match should yield empty snippet")
	}
	if got := BuildSnippet("héllo wörld", "WÖRLD"); got != "héllo wörld" {
		t.Errorf("unicode snippet = %q", got)
	}
}

func TestNewSearchHit(t *testing.T) {
	d := &Document{ID: uuid.New(), Title: "Deploy guide", Content: json.RawMessage(sampleContent)}
	if h := NewSearchHit(d, "deploy"); h.MatchedIn != SearchMatchTitle || h.Snippet != "" {
		t.Errorf("title hit = %+v", h)
	}
	h := NewSearchHit(d, "migration")
	if h.MatchedIn != SearchMatchContent || !strings.Contains(h.Snippet, "migration") {
		t.Errorf("content hit = %+v", h)
	}
}

func TestNewSearchHit_PhraseAcrossFormattingReportsBlock(t *testing.T) {
	content := json.RawMessage(`[
	 {"type":"heading","content":[{"type":"text","text":"Intro"}]},
	 {"type":"bulletListItem","content":[{"type":"text","text":"parent"}],"children":[
	   {"type":"bulletListItem","content":[{"type":"text","text":"run the "},{"type":"text","text":"blue","styles":{"bold":true}},{"type":"text","text":" deploy"}]}]},
	 {"type":"paragraph","content":[{"type":"text","text":"blue deploy again"}]}
	]`)
	d := &Document{ID: uuid.New(), Title: "Guide", Content: content}
	h := NewSearchHit(d, "the BLUE deploy")
	if h.MatchedIn != SearchMatchContent || h.Block != 3 {
		t.Fatalf("hit = %+v, want a content match in block 3 (the nested item)", h)
	}
	if h.Snippet != "run the blue deploy" {
		t.Fatalf("snippet = %q, want the matched block's text", h.Snippet)
	}
	// The first matching block wins.
	if h := NewSearchHit(d, "blue deploy"); h.Block != 3 {
		t.Fatalf("first match block = %d, want 3", h.Block)
	}
	if h := NewSearchHit(d, "again"); h.Block != 4 {
		t.Fatalf("last block = %d, want 4", h.Block)
	}
	// A title match has no block.
	if h := NewSearchHit(d, "guide"); h.MatchedIn != SearchMatchTitle || h.Block != 0 {
		t.Fatalf("title hit = %+v", h)
	}
}

func TestNewSearchHit_LongBlockSnippetIsTrimmed(t *testing.T) {
	text := strings.Repeat("a ", 100) + "needle " + strings.Repeat("b ", 100)
	content, _ := json.Marshal([]any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "first"}}},
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}},
	})
	h := NewSearchHit(&Document{ID: uuid.New(), Title: "x", Content: content}, "needle")
	if h.Block != 2 || !strings.HasPrefix(h.Snippet, "…") || !strings.HasSuffix(h.Snippet, "…") || !strings.Contains(h.Snippet, "needle") {
		t.Fatalf("hit = %+v", h)
	}
	if strings.Contains(h.Snippet, "first") {
		t.Fatalf("snippet reaches into another block: %q", h.Snippet)
	}
}

// Content that is not BlockNote's top-level array has lines that are not
// blocks, so its hits carry an excerpt but no block.
func TestNewSearchHit_NonArrayContentHasNoBlock(t *testing.T) {
	content := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"pm shape here"}]}]}`)
	h := NewSearchHit(&Document{ID: uuid.New(), Title: "x", Content: content}, "shape")
	if h.MatchedIn != SearchMatchContent || h.Block != 0 || h.Snippet != "pm shape here" {
		t.Fatalf("hit = %+v, want a content match with the excerpt and no block", h)
	}
}

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

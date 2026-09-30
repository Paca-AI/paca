package docdom

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// SearchMatchTitle / SearchMatchContent say where a SearchHit's query matched.
// A title match wins when both match.
const (
	SearchMatchTitle   = "title"
	SearchMatchContent = "content"
)

// SearchHit is one document matching a title-or-content search, with a short
// plain-text excerpt around the first match.
type SearchHit struct {
	Document  *Document
	MatchedIn string
	Snippet   string
}

// snippetRadius is how many runes of context BuildSnippet keeps either side
// of the match.
const snippetRadius = 80

// ExtractPlainText flattens BlockNote JSON into plain text by concatenating
// every string under a "text" key (inline text nodes), with a newline between
// top-level blocks. Structural keys ("type", "props", ids) are ignored, so a
// query like "paragraph" doesn't match the schema itself.
func ExtractPlainText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var root any
	if err := json.Unmarshal(content, &root); err != nil {
		return ""
	}
	var sb strings.Builder
	walkText(root, &sb)
	return strings.TrimSpace(sb.String())
}

func walkText(v any, sb *strings.Builder) {
	switch n := v.(type) {
	case []any:
		for _, c := range n {
			walkText(c, sb)
		}
	case map[string]any:
		if t, ok := n["text"].(string); ok {
			sb.WriteString(t)
		}
		if c, ok := n["content"]; ok {
			walkText(c, sb)
		}
		if c, ok := n["children"]; ok {
			walkText(c, sb)
		}
		// A block (has "type" and "id") ends a line; inline nodes don't.
		if _, isBlock := n["id"]; isBlock {
			sb.WriteString("\n")
		}
	}
}

// BuildSnippet returns an excerpt of text around the first case-insensitive
// occurrence of query, with newlines collapsed to spaces and "…" marking
// truncation. It returns "" when query doesn't occur.
func BuildSnippet(text, query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}
	runes := []rune(text)
	lowerRunes := []rune(strings.ToLower(text))
	// Lowercasing can change rune count for exotic scripts; fall back to the
	// unlowered text so indexes stay aligned.
	if len(lowerRunes) != len(runes) {
		lowerRunes = runes
	}
	qr := []rune(strings.ToLower(q))
	idx := indexRunes(lowerRunes, qr)
	if idx < 0 {
		return ""
	}
	start := max(0, idx-snippetRadius)
	end := min(len(runes), idx+len(qr)+snippetRadius)
	s := strings.Join(strings.Fields(string(runes[start:end])), " ")
	if start > 0 {
		s = "…" + s
	}
	if end < len(runes) {
		s += "…"
	}
	if !utf8.ValidString(s) {
		return ""
	}
	return s
}

func indexRunes(hay, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(hay) {
		return -1
	}
outer:
	for i := 0; i+len(needle) <= len(hay); i++ {
		for j := range needle {
			if hay[i+j] != needle[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}

// NewSearchHit classifies a matched document and builds its snippet. Title
// matches carry no snippet; content matches carry an excerpt.
func NewSearchHit(d *Document, query string) SearchHit {
	if strings.Contains(strings.ToLower(d.Title), strings.ToLower(strings.TrimSpace(query))) {
		return SearchHit{Document: d, MatchedIn: SearchMatchTitle}
	}
	return SearchHit{
		Document:  d,
		MatchedIn: SearchMatchContent,
		Snippet:   BuildSnippet(ExtractPlainText(d.Content), query),
	}
}

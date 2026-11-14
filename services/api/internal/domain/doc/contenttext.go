package docdom

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// ContentText flattens BlockNote content into the plain text the search index
// holds (documents.content_text): one line per block, in document order, with
// a block's children on the lines after it. A line is the block's inline text
// runs joined with nothing between them, so "foo **bar**" reads "foo bar" and
// a phrase that crosses bold, italic or a link is still one string.
//
// Each block is exactly one line, so the 1-based line number of a match is
// the block's position in a depth-first walk (SearchHit.Block). Two things
// keep it that way: a newline inside a text run (a hard break) becomes a
// space, and a table is one line with its cells, row after row, separated by
// tabs. An empty block is an empty line.
//
// Blocks are recognised by position (elements of the top-level array and of
// "children"), not by an "id": the web editor saves blocks without ids.
// Structural keys (type, props, ids) are never text. nil, null, [] and content
// that does not parse all give "".
//
// The API does not require BlockNote's top-level array. Content of any other
// shape gives every string stored under a "text" key, at any depth, in
// document order, one per line: what search matched in such content before
// this column existed, so no document it found becomes unfindable. Lines
// there are not blocks.
func ContentText(content json.RawMessage) string {
	text, _ := contentLines(content)
	return text
}

// contentLines is ContentText, and whether its lines are blocks (the content
// is BlockNote's top-level array, or empty).
func contentLines(content json.RawMessage) (string, bool) {
	if len(content) == 0 {
		return "", true
	}
	var blocks []any
	if err := json.Unmarshal(content, &blocks); err != nil {
		return textStrings(content), false
	}
	var lines []string
	appendBlockLines(blocks, &lines)
	return strings.Join(lines, "\n"), true
}

// SearchQuery normalizes a search query: surrounding space trimmed and line
// breaks turned into spaces. ContentText holds one block per line, so a
// query is matched within one line, by the SQL and by the hit builder alike.
func SearchQuery(q string) string {
	return strings.TrimSpace(lineBreaks.Replace(q))
}

// textStrings returns every string value under a "text" key in raw, in the
// order it appears, one per line ("" when raw does not parse).
func textStrings(raw json.RawMessage) string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	// Per open container: whether it is an object, and for an object whether
	// the next token is a key.
	type frame struct{ object, wantKey bool }
	var (
		stack   []frame
		lastKey string
		lines   []string
	)
	for {
		tok, err := dec.Token()
		if err != nil {
			if err != io.EOF {
				return ""
			}
			break
		}
		inObject := len(stack) > 0 && stack[len(stack)-1].object
		if inObject && stack[len(stack)-1].wantKey {
			if d, ok := tok.(json.Delim); ok && d == '}' {
				stack = stack[:len(stack)-1]
				continue
			}
			lastKey, _ = tok.(string)
			stack[len(stack)-1].wantKey = false
			continue
		}
		// tok is a value (or the end of an array).
		if inObject {
			stack[len(stack)-1].wantKey = true
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{':
				stack = append(stack, frame{object: true, wantKey: true})
			case '[':
				stack = append(stack, frame{})
			case ']':
				stack = stack[:len(stack)-1]
				if len(stack) > 0 && stack[len(stack)-1].object {
					stack[len(stack)-1].wantKey = true
				}
			}
		case string:
			if inObject && lastKey == "text" {
				lines = append(lines, lineBreaks.Replace(v))
			}
		}
	}
	return strings.Join(lines, "\n")
}

// lineBreaks turns a hard break inside a text run into a space, so a block
// stays one line.
var lineBreaks = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")

func appendBlockLines(blocks []any, lines *[]string) {
	for _, b := range blocks {
		block, ok := b.(map[string]any)
		if !ok {
			continue
		}
		var sb strings.Builder
		writeBlockContent(block["content"], &sb)
		*lines = append(*lines, lineBreaks.Replace(sb.String()))
		if children, ok := block["children"].([]any); ok {
			appendBlockLines(children, lines)
		}
	}
}

// writeBlockContent writes a block's "content": a string, an array of inline
// nodes, or a table's {"type":"tableContent","rows":[{"cells":[…]}]}.
func writeBlockContent(v any, sb *strings.Builder) {
	switch c := v.(type) {
	case string:
		sb.WriteString(c)
	case []any:
		writeInline(c, sb)
	case map[string]any:
		rows, _ := c["rows"].([]any)
		first := true
		for _, r := range rows {
			row, _ := r.(map[string]any)
			cells, _ := row["cells"].([]any)
			for _, cell := range cells {
				if !first {
					sb.WriteByte('\t')
				}
				first = false
				switch cc := cell.(type) {
				case []any: // older shape: the cell is its inline content
					writeInline(cc, sb)
				case map[string]any: // {"type":"tableCell","content":[…]}
					writeBlockContent(cc["content"], sb)
				}
			}
		}
	}
}

// writeInline writes the text of inline nodes: a text node's "text", and the
// nested "content" of a link (or any other inline node that wraps text).
func writeInline(nodes []any, sb *strings.Builder) {
	for _, n := range nodes {
		switch node := n.(type) {
		case string:
			sb.WriteString(node)
		case map[string]any:
			if t, ok := node["text"].(string); ok {
				sb.WriteString(t)
			}
			if inner, ok := node["content"]; ok {
				writeBlockContent(inner, sb)
			}
		}
	}
}

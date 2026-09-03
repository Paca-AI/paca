package annotationsvc

import "encoding/json"

// blockNoteText/blockNoteParagraph are the minimal subset of BlockNote's
// own block JSON shape needed here — the same shape task.Service already
// stores task.Task.Description as elsewhere in this codebase (see e.g.
// task_service_test.go's own literal BlockNote JSON).
type blockNoteText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type blockNoteParagraph struct {
	Type    string          `json:"type"`
	Content []blockNoteText `json:"content"`
}

func paragraph(text string) blockNoteParagraph {
	return blockNoteParagraph{Type: "paragraph", Content: []blockNoteText{{Type: "text", Text: text}}}
}

// buildTaskDescription renders a task description pre-filled from a page
// annotation as a single paragraph containing just the comment's own
// canonical URL — not the comment body/page path/element/console/network
// context as text. apps/web's BlockNote load path (see
// comment-blocknote.tsx's normalizeBlockContent/convertAnnotationLinks)
// recognizes that URL and swaps the paragraph for the rich annotationCard
// block, which fetches and renders all of that context live instead —
// exactly the same "plain text in, rich embed out on load" trick
// convertMermaidCodeBlocks already does for pasted/typed ```mermaid
// fences. Keeping only the URL here means the embed never goes stale: the
// task always shows the comment's current status/body/replies, not a
// snapshot frozen at task-creation time.
func buildTaskDescription(url string) json.RawMessage {
	blocks := []blockNoteParagraph{paragraph(url)}
	raw, err := json.Marshal(blocks)
	if err != nil {
		// blockNoteParagraph/blockNoteText are always marshalable (no
		// channels/funcs/cycles) — this can't actually fail, but fall back
		// to a minimal valid document rather than a nil Description if it
		// somehow ever did.
		return json.RawMessage(`[{"type":"paragraph","content":[]}]`)
	}
	return raw
}

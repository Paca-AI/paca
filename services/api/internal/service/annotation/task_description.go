package annotationsvc

import (
	"encoding/json"
	"fmt"

	annotationdom "github.com/Paca-AI/api/internal/domain/annotation"
)

// maxCapturedItems bounds how many console errors / failed requests get
// written into a generated task description — enough to be genuinely
// useful context, not so many that a noisy page turns the task
// description into a wall of text.
const maxCapturedItems = 5

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
// annotation: the comment body itself, then the captured page/element/
// console/network context — everything a human or agent needs to
// understand and act on the issue without re-opening the preview or
// re-running any browser automation (the annotation's whole reason for
// capturing this much at comment time in the first place).
func buildTaskDescription(a *annotationdom.PageAnnotation) json.RawMessage {
	blocks := []blockNoteParagraph{
		paragraph(a.Body),
		paragraph(fmt.Sprintf("Reported from page preview: %s", a.PagePath)),
		paragraph(fmt.Sprintf("Element: <%s> %q (selector: %s)", a.ElementSnapshot.TagName, a.ElementSnapshot.TextExcerpt, a.ElementSelector)),
	}

	if n := len(a.ConsoleErrors); n > 0 {
		shown := n
		if shown > maxCapturedItems {
			shown = maxCapturedItems
		}
		for _, e := range a.ConsoleErrors[:shown] {
			blocks = append(blocks, paragraph(fmt.Sprintf("Console %s: %s", e.Level, e.Message)))
		}
		if n > shown {
			blocks = append(blocks, paragraph(fmt.Sprintf("…and %d more console error(s)", n-shown)))
		}
	}

	if n := len(a.FailedRequests); n > 0 {
		shown := n
		if shown > maxCapturedItems {
			shown = maxCapturedItems
		}
		for _, req := range a.FailedRequests[:shown] {
			status := fmt.Sprintf("%d", req.StatusCode)
			if req.StatusCode == 0 {
				status = req.Error
			}
			blocks = append(blocks, paragraph(fmt.Sprintf("Failed request: %s %s → %s", req.Method, req.URL, status)))
		}
		if n > shown {
			blocks = append(blocks, paragraph(fmt.Sprintf("…and %d more failed request(s)", n-shown)))
		}
	}

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

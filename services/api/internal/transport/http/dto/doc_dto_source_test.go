package dto

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

func TestDocumentDTOs_CarrySource(t *testing.T) {
	d := &docdom.Document{
		ID: uuid.New(), ProjectID: uuid.New(), Title: "t",
		Source:       &docdom.DocSource{Repository: "paca", Path: "docs/x.md", Commit: "abc"},
		SourceLocked: true,
	}
	for name, v := range map[string]any{"full": DocumentFromEntity(d), "list": DocumentListItemFromEntity(d)} {
		b, _ := json.Marshal(v)
		if !strings.Contains(string(b), `"source":{"repository":"paca","path":"docs/x.md","commit":"abc"}`) ||
			!strings.Contains(string(b), `"source_locked":true`) {
			t.Errorf("%s: %s", name, b)
		}
	}
	plain := &docdom.Document{ID: uuid.New(), ProjectID: uuid.New(), Title: "t"}
	b, _ := json.Marshal(DocumentFromEntity(plain))
	if strings.Contains(string(b), `"source":`) || !strings.Contains(string(b), `"source_locked":false`) {
		t.Errorf("plain: %s", b)
	}
}

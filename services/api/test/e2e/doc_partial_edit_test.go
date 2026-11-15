package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// A partial edit through MCP edit_doc (Paca-AI/paca#747, PR 6): the tool reads
// the document, sends every block it did not touch back exactly as it read it
// and replaces only the touched one, with If-Match set to the ETag of the read.
// This checks the server half of "changing one line leaves untouched blocks
// identical in storage": those nine blocks are stored and read back unchanged.
// (The MCP half, that edit_doc sends the very objects it read, is covered by
// apps/mcp's doc-edit and edit_doc tests.)

// tenBlocks is a ten-block BlockNote document of the kinds MCP writes:
// headings, paragraphs with styled text, list items and a code block.
func tenBlocks() json.RawMessage {
	blocks := []string{
		`{"id":"b0","type":"heading","props":{"level":1,"textColor":"default","backgroundColor":"default","textAlignment":"left","isToggleable":false},"content":[{"type":"text","text":"Release plan","styles":{}}],"children":[]}`,
		`{"id":"b1","type":"paragraph","props":{"textColor":"red","backgroundColor":"default","textAlignment":"left"},"content":[{"type":"text","text":"Intro with ","styles":{}},{"type":"text","text":"bold","styles":{"bold":true}},{"type":"text","text":" text.","styles":{}}],"children":[]}`,
		`{"id":"b2","type":"heading","props":{"level":2,"textColor":"default","backgroundColor":"default","textAlignment":"left","isToggleable":false},"content":[{"type":"text","text":"Steps","styles":{}}],"children":[]}`,
		`{"id":"b3","type":"numberedListItem","props":{"textColor":"default","backgroundColor":"default","textAlignment":"left"},"content":[{"type":"text","text":"Build the image","styles":{}}],"children":[]}`,
		`{"id":"b4","type":"numberedListItem","props":{"textColor":"default","backgroundColor":"default","textAlignment":"left"},"content":[{"type":"text","text":"Push the image","styles":{}}],"children":[]}`,
		`{"id":"b5","type":"checkListItem","props":{"textColor":"default","backgroundColor":"default","textAlignment":"left","checked":true},"content":[{"type":"text","text":"tag release","styles":{}}],"children":[]}`,
		`{"id":"b6","type":"codeBlock","props":{"language":"bash"},"content":[{"type":"text","text":"# not a heading\nmake deploy","styles":{}}],"children":[]}`,
		`{"id":"b7","type":"quote","props":{"textColor":"default","backgroundColor":"default"},"content":[{"type":"text","text":"Careful with prod.","styles":{"italic":true}}],"children":[]}`,
		`{"id":"b8","type":"heading","props":{"level":2,"textColor":"default","backgroundColor":"default","textAlignment":"left","isToggleable":false},"content":[{"type":"text","text":"Rollback","styles":{}}],"children":[]}`,
		`{"id":"b9","type":"paragraph","props":{"textColor":"default","backgroundColor":"default","textAlignment":"left"},"content":[{"type":"text","text":"Run the previous tag.","styles":{}}],"children":[]}`,
	}
	out := "["
	for i, b := range blocks {
		if i > 0 {
			out += ","
		}
		out += b
	}
	return json.RawMessage(out + "]")
}

// readDocBlocks GETs the document and returns its top-level blocks as the raw
// JSON the API sent, and its ETag.
func readDocBlocks(t *testing.T, env *e2eEnv, client *http.Client, token, url string) ([]json.RawMessage, string) {
	t.Helper()
	status, e, hdr, raw := docRequest(t, env, client, token, http.MethodGet, url, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("get: %d %s", status, e.Error)
	}
	var body struct {
		Data struct {
			Content []json.RawMessage `json:"content"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	return body.Data.Content, hdr.Get("ETag")
}

func TestDocs_PartialEditKeepsUntouchedBlocksIdentical(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "doc-partial-user", "docpartialpass1")
	client, token := taskMemberLogin(t, env, "doc-partial-user", "docpartialpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	docs := fmt.Sprintf("%s/api/v1/projects/%s/docs", env.base, projID)

	status, e, _, _ := docRequest(t, env, client, token, http.MethodPost, docs, map[string]any{"title": "Release", "content": tenBlocks()}, nil)
	if status != http.StatusCreated {
		t.Fatalf("create: %d %s", status, e.Error)
	}
	docID, _ := assertDataMap(t, e)["id"].(string)
	url := docs + "/" + docID

	before, etag := readDocBlocks(t, env, client, token, url)
	if len(before) != 10 || etag == "" {
		t.Fatalf("read: %d blocks, ETag %q", len(before), etag)
	}
	// What edit_doc sends for "Push the image" -> "Push the tag": the nine
	// other blocks exactly as read, the edited one re-parsed from Markdown.
	const edited = 4
	content := make([]json.RawMessage, len(before))
	copy(content, before)
	content[edited] = json.RawMessage(`{"id":"b4","type":"numberedListItem","props":{"textColor":"default","backgroundColor":"default","textAlignment":"left"},"content":[{"type":"text","text":"Push the tag","styles":{}}],"children":[]}`)
	status, e, _, _ = docRequest(t, env, client, token, http.MethodPatch, url, map[string]any{"content": content, "note": "wording", "client": "paca-mcp/e2e"}, map[string]string{"If-Match": etag})
	if status != http.StatusOK {
		t.Fatalf("patch: %d %s", status, e.Error)
	}

	after, etagAfter := readDocBlocks(t, env, client, token, url)
	if len(after) != 10 {
		t.Fatalf("after the edit: %d blocks, want 10", len(after))
	}
	if etagAfter == etag {
		t.Fatalf("ETag unchanged after a content edit: %q", etagAfter)
	}
	for i := range before {
		if i == edited {
			if bytes.Equal(after[i], before[i]) {
				t.Fatalf("block %d did not change: %s", i, after[i])
			}
			continue
		}
		if !bytes.Equal(after[i], before[i]) {
			t.Errorf("block %d changed:\n before %s\n after  %s", i, before[i], after[i])
		}
	}

	// In storage: each untouched block is the same JSONB value as before.
	for i := range before {
		if i == edited {
			continue
		}
		var same bool
		if err := env.db.GetContext(env.ctx, &same, `SELECT content->$2::int = $3::jsonb FROM documents WHERE id = $1`, docID, i, string(before[i])); err != nil {
			t.Fatalf("compare stored block %d: %v", i, err)
		}
		if !same {
			t.Errorf("stored block %d differs from the block read before the edit", i)
		}
	}
}

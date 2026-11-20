package e2e_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Documentation shows up in the file manager API: its folders and documents are
// listed, documents are read-only there, and a folder made from Files is a
// documentation folder as far as the docs API is concerned.
func TestFilesAPI_DocumentationInTheFileTree(t *testing.T) {
	t.Parallel()
	env, client, token, projID, docs := docSeqSetup(t, "files-docs-tree")
	files := fmt.Sprintf("%s/api/v1/projects/%s/files", env.base, projID)

	folder := createDocFolderViaAPI(t, env, client, token, docs, "Governance", nil)
	inFolder := postDocViaAPI(t, env, client, token, docs, map[string]any{"title": "Charter", "folder_id": folder, "content": docBlocks("a", "x")})
	loose := postDocViaAPI(t, env, client, token, docs, map[string]any{"title": "Loose", "content": docBlocks("b", "y")})

	list := func(parent string) map[string]map[string]any {
		t.Helper()
		url := files + "/nodes"
		if parent != "" {
			url += "?parent_id=" + parent
		}
		status, e, _, raw := docRequest(t, env, client, token, http.MethodGet, url, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("list %q: %d %s", parent, status, raw)
		}
		out := map[string]map[string]any{}
		for _, it := range assertDataMap(t, e)["items"].([]any) {
			m := it.(map[string]any)
			out[m["name"].(string)] = m
		}
		return out
	}

	root := list("")
	documentation, ok := root["Documentation"]
	if !ok || documentation["system"] != "documentation" || documentation["area"] != "docs" {
		t.Fatalf("project root = %v, want the Documentation folder", root)
	}
	docID := documentation["id"].(string)

	top := list(docID)
	if top["Governance"]["kind"] != "folder" || top["Loose.md"]["kind"] != "doc" || top["Loose.md"]["id"] != loose {
		t.Fatalf("Documentation = %v", top)
	}
	if got := list(folder); got["Charter.md"]["kind"] != "doc" || got["Charter.md"]["id"] != inFolder || got["Charter.md"]["locked"] != false {
		t.Fatalf("Governance = %v", got)
	}

	// A document downloads as a Markdown file: its title as the first heading.
	status, _, hdr, raw := docRequest(t, env, client, token, http.MethodGet, docs+"/"+inFolder+"/markdown", nil, nil)
	if status != http.StatusOK || !strings.HasPrefix(string(raw), "# Charter\n") || !strings.Contains(string(raw), "x") {
		t.Fatalf("markdown download: %d %q", status, raw)
	}
	if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("content type = %q", ct)
	}
	if cd := hdr.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "Charter.md") {
		t.Fatalf("content disposition = %q", cd)
	}

	// Documents are managed by the docs editor.
	status, _, _, raw = docRequest(t, env, client, token, http.MethodPatch, files+"/nodes/"+inFolder, map[string]any{"name": "x"}, nil)
	if status == http.StatusOK {
		t.Fatalf("renaming a document from Files succeeded: %s", raw)
	}

	// A document is a file in Files: star it, open it, find it by name.
	itemNames := func(url string) map[string]bool {
		t.Helper()
		status, e, _, raw := docRequest(t, env, client, token, http.MethodGet, url, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("GET %s: %d %s", url, status, raw)
		}
		out := map[string]bool{}
		for _, it := range assertDataMap(t, e)["items"].([]any) {
			out[it.(map[string]any)["name"].(string)] = true
		}
		return out
	}
	if status, _, _, raw = docRequest(t, env, client, token, http.MethodPut, files+"/nodes/"+inFolder+"/star", map[string]any{"starred": true}, nil); status >= 300 {
		t.Fatalf("star a document: %d %s", status, raw)
	}
	if !itemNames(files + "/starred")["Charter.md"] {
		t.Fatal("a starred document is missing from Starred")
	}
	if status, _, _, raw = docRequest(t, env, client, token, http.MethodPost, files+"/nodes/"+inFolder+"/open", nil, nil); status >= 300 {
		t.Fatalf("open a document: %d %s", status, raw)
	}
	if !itemNames(files + "/recent")["Charter.md"] {
		t.Fatal("an opened document is missing from Recent")
	}
	if !itemNames(files + "/nodes?q=charter")["Charter.md"] {
		t.Fatal("search does not find the document")
	}
	// Renaming through the docs API shows in Files.
	if status, _, _, raw = docRequest(t, env, client, token, http.MethodPatch, docs+"/"+inFolder, map[string]any{"title": "Charter v2"}, nil); status != http.StatusOK {
		t.Fatalf("rename: %d %s", status, raw)
	}
	if !itemNames(files + "/nodes?parent_id=" + folder)["Charter v2.md"] {
		t.Fatal("the renamed document does not show its new name in Files")
	}

	// A document goes to the Files trash and comes back.
	if status, _, _, raw = docRequest(t, env, client, token, http.MethodDelete, files+"/nodes/"+inFolder, nil, nil); status != http.StatusNoContent {
		t.Fatalf("trash a document: %d %s", status, raw)
	}
	if status, _, _, _ = docRequest(t, env, client, token, http.MethodGet, docs+"/"+inFolder, nil, nil); status != http.StatusNotFound {
		t.Fatalf("a trashed document is still readable through docs: %d", status)
	}
	if !itemNames(files + "/trash")["Charter v2.md"] {
		t.Fatal("the trashed document is missing from the Files trash")
	}
	if status, _, _, raw = docRequest(t, env, client, token, http.MethodPost, files+"/nodes/"+inFolder+"/restore", nil, nil); status != http.StatusOK {
		t.Fatalf("restore a document: %d %s", status, raw)
	}
	if status, _, _, raw = docRequest(t, env, client, token, http.MethodGet, docs+"/"+inFolder, nil, nil); status != http.StatusOK {
		t.Fatalf("the restored document is not readable: %d %s", status, raw)
	}

	// A folder created in Files below Documentation is a docs folder too.
	status, e, _, raw := docRequest(t, env, client, token, http.MethodPost, files+"/folders", map[string]any{"parent_id": folder, "name": "Risks"}, nil)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("create folder in Documentation via Files: %d %s", status, raw)
	}
	made := assertDataMap(t, e)["id"].(string)
	status, e, _, raw = docRequest(t, env, client, token, http.MethodGet, docs+"/folders?parent_id="+folder, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("docs folders: %d %s", status, raw)
	}
	found := false
	for _, it := range assertDataMap(t, e)["items"].([]any) {
		if it.(map[string]any)["id"] == made {
			found = true
		}
	}
	if !found {
		t.Fatalf("the folder made from Files is missing from the docs API: %s", raw)
	}
	// And a new folder at the top of Documentation.
	if status, _, _, raw = docRequest(t, env, client, token, http.MethodPost, files+"/folders", map[string]any{"parent_id": docID, "name": "Research"}, nil); status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("create folder under the Documentation folder: %d %s", status, raw)
	}
}

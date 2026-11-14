package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Paca-AI/api/internal/events"
)

// Doc content hash, ETag and If-Match (Paca-AI/paca#747, PR 1).

// docBlocks is one paragraph with the given block id: the same document
// whatever the id, as two pushes of one Markdown file through MCP produce.
func docBlocks(id, text string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`[{"id":%q,"type":"paragraph","props":{"textAlignment":"left"},"content":[{"type":"text","text":%q,"styles":{}}],"children":[]}]`, id, text))
}

// docRequest sends one request with optional extra headers and returns the
// status, the decoded envelope and the response headers.
func docRequest(t *testing.T, env *e2eEnv, client *http.Client, token, method, url string, body any, headers map[string]string) (int, envelope, http.Header, []byte) {
	t.Helper()
	buf := &bytes.Buffer{}
	if body != nil {
		buf = jsonBody(t, body)
	}
	req := mustRequest(env.ctx, t, method, url, buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var e envelope
	_ = json.Unmarshal(raw, &e)
	return resp.StatusCode, e, resp.Header, raw
}

func docSnapshotCount(t *testing.T, env *e2eEnv, docID string) int {
	t.Helper()
	var n int
	if err := env.db.GetContext(env.ctx, &n, `SELECT count(*) FROM doc_snapshots WHERE document_id = $1`, docID); err != nil {
		t.Fatal(err)
	}
	return n
}

// docUpdatedEvents counts doc.updated entries for docID on the activities
// stream: the stream copy of the activity, which is also what announces the
// change to live clients.
func docUpdatedEvents(t *testing.T, env *e2eEnv, docID string) int {
	t.Helper()
	msgs, err := env.redisClient.XRange(env.ctx, events.StreamActivities, "-", "+").Result()
	if err != nil {
		t.Fatalf("read %s: %v", events.StreamActivities, err)
	}
	n := 0
	for _, m := range msgs {
		if m.Values["type"] == "doc.updated" && m.Values["entity_id"] == docID {
			n++
		}
	}
	return n
}

func TestDocs_IdenticalPushIsNoOp(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "doc-noop-user", "docnooppass1")
	client, token := taskMemberLogin(t, env, "doc-noop-user", "docnooppass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	docs := fmt.Sprintf("%s/api/v1/projects/%s/docs", env.base, projID)

	status, e, hdr, _ := docRequest(t, env, client, token, http.MethodPost, docs, map[string]any{"title": "Plan", "content": docBlocks("first", "hello")}, nil)
	if status != http.StatusCreated {
		t.Fatalf("create: %d %s", status, e.Error)
	}
	created := assertDataMap(t, e)
	docID, _ := created["id"].(string)
	hash, _ := created["content_hash"].(string)
	if len(hash) != 64 || !strings.HasPrefix(hdr.Get("ETag"), `"`+hash+`-`) {
		t.Fatalf("create: content_hash %q ETag %q", hash, hdr.Get("ETag"))
	}

	// First PATCH with real content so the doc has an update history.
	status, e, _, _ = docRequest(t, env, client, token, http.MethodPatch, docs+"/"+docID, map[string]any{"content": docBlocks("second", "hello world")}, nil)
	if status != http.StatusOK {
		t.Fatalf("first patch: %d %s", status, e.Error)
	}
	first := assertDataMap(t, e)
	snaps, evs := docSnapshotCount(t, env, docID), docUpdatedEvents(t, env, docID)
	if snaps != 1 || evs != 1 {
		t.Fatalf("after a real change: %d snapshots, %d doc.updated events; want 1 and 1", snaps, evs)
	}

	// The same blocks again with a fresh id (and the title unchanged): a no-op.
	status, e, hdr, _ = docRequest(t, env, client, token, http.MethodPatch, docs+"/"+docID, map[string]any{"title": "Plan", "content": docBlocks("third", "hello world")}, nil)
	if status != http.StatusOK {
		t.Fatalf("second patch: %d %s", status, e.Error)
	}
	second := assertDataMap(t, e)
	if second["updated_at"] != first["updated_at"] {
		t.Fatalf("updated_at moved on an identical push: %v -> %v", first["updated_at"], second["updated_at"])
	}
	if second["content_hash"] != first["content_hash"] || !strings.HasPrefix(hdr.Get("ETag"), `"`+first["content_hash"].(string)+`-`) {
		t.Fatalf("hash changed on an identical push: %v -> %v (ETag %q)", first["content_hash"], second["content_hash"], hdr.Get("ETag"))
	}
	if n := docSnapshotCount(t, env, docID); n != snaps {
		t.Fatalf("identical push took a snapshot: %d -> %d", snaps, n)
	}
	if n := docUpdatedEvents(t, env, docID); n != evs {
		t.Fatalf("identical push recorded a doc.updated activity: %d -> %d", evs, n)
	}

	// GET agrees, and If-None-Match with its ETag is a 304 with no body.
	status, e, hdr, _ = docRequest(t, env, client, token, http.MethodGet, docs+"/"+docID, nil, nil)
	if status != http.StatusOK || assertDataMap(t, e)["updated_at"] != first["updated_at"] {
		t.Fatalf("get: %d %v", status, e.Data)
	}
	status, _, _, raw := docRequest(t, env, client, token, http.MethodGet, docs+"/"+docID, nil, map[string]string{"If-None-Match": hdr.Get("ETag")})
	if status != http.StatusNotModified || len(raw) != 0 {
		t.Fatalf("If-None-Match: %d body %q", status, raw)
	}

	// The snapshot of the first version is addressable by its hash.
	status, e, _, _ = docRequest(t, env, client, token, http.MethodGet, docs+"/"+docID+"/snapshots?content_hash="+hash, nil, nil)
	items, _ := assertDataMap(t, e)["items"].([]any)
	if status != http.StatusOK || len(items) != 1 {
		t.Fatalf("snapshots by hash: %d %v", status, e.Data)
	}
}

func TestDocs_TwoWritersSecondGets412(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "doc-412-user", "doc412pass1")
	client, token := taskMemberLogin(t, env, "doc-412-user", "doc412pass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	docs := fmt.Sprintf("%s/api/v1/projects/%s/docs", env.base, projID)

	status, e, _, _ := docRequest(t, env, client, token, http.MethodPost, docs, map[string]any{"title": "Shared", "content": docBlocks("a", "base")}, nil)
	if status != http.StatusCreated {
		t.Fatalf("create: %d %s", status, e.Error)
	}
	docID, _ := assertDataMap(t, e)["id"].(string)

	// Both writers read the same version.
	_, _, hdr, _ := docRequest(t, env, client, token, http.MethodGet, docs+"/"+docID, nil, nil)
	etag := hdr.Get("ETag")
	if etag == "" {
		t.Fatal("GET returned no ETag")
	}

	// Writer A wins.
	status, e, hdrA, _ := docRequest(t, env, client, token, http.MethodPatch, docs+"/"+docID, map[string]any{"content": docBlocks("b", "from A")}, map[string]string{"If-Match": etag})
	if status != http.StatusOK {
		t.Fatalf("writer A: %d %s", status, e.Error)
	}
	afterA := assertDataMap(t, e)
	if hdrA.Get("ETag") == etag {
		t.Fatal("ETag did not change after a content write")
	}

	// Writer B, still holding the old ETag, is refused and told what is stored.
	status, e, _, raw := docRequest(t, env, client, token, http.MethodPatch, docs+"/"+docID, map[string]any{"content": docBlocks("c", "from B")}, map[string]string{"If-Match": etag})
	if status != http.StatusPreconditionFailed || e.ErrorCode != "VERSION_CONFLICT" {
		t.Fatalf("writer B: %d %q", status, e.ErrorCode)
	}
	var body struct {
		Details map[string]string `json:"error_details"`
	}
	_ = json.Unmarshal(raw, &body)
	if body.Details["content_hash"] != afterA["content_hash"] || body.Details["updated_at"] == "" || body.Details["updated_by"] == "" {
		t.Fatalf("412 details = %v, want A's hash %v", body.Details, afterA["content_hash"])
	}

	// B's content was not stored.
	_, e, _, _ = docRequest(t, env, client, token, http.MethodGet, docs+"/"+docID, nil, nil)
	if got := assertDataMap(t, e); !strings.Contains(fmt.Sprint(got["content"]), "from A") {
		t.Fatalf("stored content = %v, want writer A's", got["content"])
	}

	// A malformed If-Match is a 400, not ignored.
	status, e, _, _ = docRequest(t, env, client, token, http.MethodPatch, docs+"/"+docID, map[string]any{"title": "x"}, map[string]string{"If-Match": `"7"`})
	if status != http.StatusBadRequest {
		t.Fatalf("malformed If-Match: %d %q", status, e.ErrorCode)
	}
}

// A rename keeps the content hash, but a client revalidating with the old
// ETag must get the new title; and If-Match stays a precondition on content,
// so a write made with the pre-rename ETag still goes through.
func TestDocs_RenameChangesETagNotContentHash(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "doc-rename-user", "docrenamepass1")
	client, token := taskMemberLogin(t, env, "doc-rename-user", "docrenamepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	docs := fmt.Sprintf("%s/api/v1/projects/%s/docs", env.base, projID)

	status, e, hdr, _ := docRequest(t, env, client, token, http.MethodPost, docs, map[string]any{"title": "Before", "content": docBlocks("a", "body")}, nil)
	if status != http.StatusCreated {
		t.Fatalf("create: %d %s", status, e.Error)
	}
	created := assertDataMap(t, e)
	docID, _ := created["id"].(string)
	hash, _ := created["content_hash"].(string)
	oldETag := hdr.Get("ETag")

	status, e, hdr, _ = docRequest(t, env, client, token, http.MethodPatch, docs+"/"+docID, map[string]any{"title": "After"}, nil)
	if status != http.StatusOK {
		t.Fatalf("rename: %d %s", status, e.Error)
	}
	if got := assertDataMap(t, e)["content_hash"]; got != hash {
		t.Fatalf("content_hash changed on a rename: %v -> %v", hash, got)
	}
	newETag := hdr.Get("ETag")
	if newETag == oldETag || !strings.HasPrefix(newETag, `"`+hash+`-`) {
		t.Fatalf("ETag after a rename = %q (before %q)", newETag, oldETag)
	}

	status, e, _, _ = docRequest(t, env, client, token, http.MethodGet, docs+"/"+docID, nil, map[string]string{"If-None-Match": oldETag})
	if status != http.StatusOK || assertDataMap(t, e)["title"] != "After" {
		t.Fatalf("GET with the pre-rename ETag: %d %v, want 200 with the new title", status, e.Data)
	}
	status, _, _, raw := docRequest(t, env, client, token, http.MethodGet, docs+"/"+docID, nil, map[string]string{"If-None-Match": newETag})
	if status != http.StatusNotModified || len(raw) != 0 {
		t.Fatalf("GET with the current ETag: %d body %q", status, raw)
	}

	// The pre-rename ETag still names the stored content.
	status, e, _, _ = docRequest(t, env, client, token, http.MethodPatch, docs+"/"+docID, map[string]any{"content": docBlocks("b", "new body")}, map[string]string{"If-Match": oldETag})
	if status != http.StatusOK || assertDataMap(t, e)["title"] != "After" {
		t.Fatalf("content write with the pre-rename ETag: %d %v", status, e.Data)
	}
}

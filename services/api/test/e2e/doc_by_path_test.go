package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// Documents written and read by path, managed-copy source and lock
// (Paca-AI/paca#747, PR 3).

// byPathFx is a project owned by one user, with the by-path URL.
type byPathFx struct {
	env    *e2eEnv
	client *http.Client
	token  string
	projID string
	docs   string // .../projects/{id}/docs
}

func newByPathFx(t *testing.T, user string) *byPathFx {
	t.Helper()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, user, user+"pass1")
	client, token := taskMemberLogin(t, env, user, user+"pass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	return &byPathFx{env: env, client: client, token: token, projID: projID,
		docs: fmt.Sprintf("%s/api/v1/projects/%s/docs", env.base, projID)}
}

func (f *byPathFx) put(t *testing.T, body map[string]any, headers map[string]string) (int, envelope, http.Header) {
	t.Helper()
	st, e, hdr, _ := docRequest(t, f.env, f.client, f.token, http.MethodPut, f.docs+"/by-path", body, headers)
	return st, e, hdr
}

func pushBody(path, blockID, text string, source map[string]any) map[string]any {
	b := map[string]any{"path": path, "content": docBlocks(blockID, text)}
	if source != nil {
		b["source"] = source
	}
	return b
}

func src(repo, commit string) map[string]any {
	return map[string]any{"repository": repo, "path": "docs/plans/x.md", "commit": commit}
}

// errorDetails reads error_details from a raw error envelope.
func errorDetails(raw []byte) map[string]string {
	var e struct {
		Details map[string]string `json:"error_details"`
	}
	_ = json.Unmarshal(raw, &e)
	return e.Details
}

func (f *byPathFx) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.env.db.GetContext(f.env.ctx, &n, query, args...); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestUpsertByPath_CreatesFoldersAndDoc(t *testing.T) {
	t.Parallel()
	f := newByPathFx(t, "bypath-create")

	st, e, hdr := f.put(t, pushBody(" Plans / 2026 // My plan ", "a", "hello", src("paca", "abc")), nil)
	if st != http.StatusCreated {
		t.Fatalf("create: %d %s", st, e.Error)
	}
	d := assertDataMap(t, e)
	if d["title"] != "My plan" || d["changed"] != true || hdr.Get("ETag") == "" {
		t.Fatalf("create: %v ETag %q", d, hdr.Get("ETag"))
	}
	source, _ := d["source"].(map[string]any)
	if source["repository"] != "paca" || source["commit"] != "abc" || d["source_locked"] != false {
		t.Fatalf("source: %v locked %v", d["source"], d["source_locked"])
	}
	if n := f.count(t, `SELECT count(*) FROM doc_folders WHERE project_id = $1`, f.projID); n != 2 {
		t.Fatalf("%d folders, want 2", n)
	}
	var parentName string
	if err := f.env.db.GetContext(f.env.ctx, &parentName, `SELECT f.name FROM documents d JOIN doc_folders f ON f.id = d.folder_id WHERE d.id = $1`, d["id"]); err != nil || parentName != "2026" {
		t.Fatalf("folder of the doc: %q %v", parentName, err)
	}

	// Read back by path (case-insensitive) in one request.
	st, e, _, _ = docRequest(t, f.env, f.client, f.token, http.MethodGet, f.docs+"/by-path?path=plans/2026/my%20plan", nil, nil)
	if st != http.StatusOK || assertDataMap(t, e)["id"] != d["id"] {
		t.Fatalf("get by path: %d %v", st, e.Data)
	}
	// The tree listing carries the source too.
	st, e, _, _ = docRequest(t, f.env, f.client, f.token, http.MethodGet, f.docs+"?folder_id="+d["folder_id"].(string), nil, nil)
	items, _ := assertDataMap(t, e)["items"].([]any)
	if st != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["source"] == nil {
		t.Fatalf("list: %d %v", st, e.Data)
	}
}

func TestUpsertByPath_UpdatesExisting(t *testing.T) {
	t.Parallel()
	f := newByPathFx(t, "bypath-update")
	st, e, _ := f.put(t, pushBody("Plans/Doc", "a", "one", nil), nil)
	if st != http.StatusCreated {
		t.Fatalf("create: %d %s", st, e.Error)
	}
	id := assertDataMap(t, e)["id"]
	st, e, _ = f.put(t, pushBody("Plans/Doc", "b", "two", nil), nil)
	d := assertDataMap(t, e)
	if st != http.StatusOK || d["changed"] != true || d["id"] != id {
		t.Fatalf("update: %d %v", st, d)
	}
	if n := docSnapshotCount(t, f.env, id.(string)); n != 1 {
		t.Fatalf("%d snapshots, want 1", n)
	}
}

func TestUpsertByPath_Unchanged(t *testing.T) {
	t.Parallel()
	f := newByPathFx(t, "bypath-same")
	st, e, _ := f.put(t, pushBody("Doc", "a", "same", nil), nil)
	if st != http.StatusCreated {
		t.Fatalf("create: %d %s", st, e.Error)
	}
	first := assertDataMap(t, e)
	st, e, _ = f.put(t, pushBody("Doc", "other-id", "same", nil), nil)
	second := assertDataMap(t, e)
	if st != http.StatusOK || second["changed"] != false || second["updated_at"] != first["updated_at"] {
		t.Fatalf("identical push: %d %v", st, second)
	}
	if n := docSnapshotCount(t, f.env, first["id"].(string)); n != 0 {
		t.Fatalf("identical push took %d snapshots", n)
	}
}

// A push with the same content and a new commit records the commit only:
// updated_at unchanged, no snapshot, no doc.updated activity.
func TestUpsertByPath_SameContentNewCommitKeepsUpdatedAt(t *testing.T) {
	t.Parallel()
	f := newByPathFx(t, "bypath-commit")
	st, e, _ := f.put(t, pushBody("Plans/Doc", "a", "text", src("paca", "c1")), nil)
	if st != http.StatusCreated {
		t.Fatalf("create: %d %s", st, e.Error)
	}
	first := assertDataMap(t, e)
	id := first["id"].(string)

	st, e, hdr := f.put(t, pushBody("Plans/Doc", "b", "text", src("paca", "c2")), nil)
	second := assertDataMap(t, e)
	if st != http.StatusOK || second["changed"] != false || second["updated_at"] != first["updated_at"] {
		t.Fatalf("new commit: %d %v", st, second)
	}
	if s, _ := second["source"].(map[string]any); s["commit"] != "c2" {
		t.Fatalf("source not updated: %v", second["source"])
	}
	if n := docSnapshotCount(t, f.env, id); n != 0 {
		t.Fatalf("%d snapshots, want 0", n)
	}
	if n := docUpdatedEvents(t, f.env, id); n != 0 {
		t.Fatalf("%d doc.updated activities, want 0", n)
	}
	var stored string
	if err := f.env.db.GetContext(f.env.ctx, &stored, `SELECT source->>'commit' FROM documents WHERE id = $1`, id); err != nil || stored != "c2" {
		t.Fatalf("stored commit %q %v", stored, err)
	}
	// The ETag still changes, because the response (its source) did.
	if hdr.Get("ETag") == "" || !strings.HasPrefix(hdr.Get("ETag"), `"`+first["content_hash"].(string)+`-`) {
		t.Fatalf("ETag %q", hdr.Get("ETag"))
	}
}

func TestUpsertByPath_IfMatchStale412(t *testing.T) {
	t.Parallel()
	f := newByPathFx(t, "bypath-412")
	st, e, _ := f.put(t, pushBody("Doc", "a", "v1", nil), nil)
	if st != http.StatusCreated {
		t.Fatalf("create: %d %s", st, e.Error)
	}
	v1 := assertDataMap(t, e)["content_hash"].(string)
	st, e, _ = f.put(t, pushBody("Doc", "a", "v2", nil), map[string]string{"If-Match": `"` + v1 + `"`})
	if st != http.StatusOK {
		t.Fatalf("matching If-Match: %d %s", st, e.Error)
	}
	v2 := assertDataMap(t, e)["content_hash"].(string)
	st, e, _, raw := docRequest(t, f.env, f.client, f.token, http.MethodPut, f.docs+"/by-path", pushBody("Doc", "a", "v3", nil), map[string]string{"If-Match": `"` + v1 + `"`})
	if det := errorDetails(raw); st != http.StatusPreconditionFailed || e.ErrorCode != "VERSION_CONFLICT" || det["content_hash"] != v2 {
		t.Fatalf("stale If-Match: %d %s %v", st, e.ErrorCode, det)
	}
	// If-Match on a path with no document is a failed precondition too.
	// There is no stored version to name, so the answer has no details.
	st, e, _, raw = docRequest(t, f.env, f.client, f.token, http.MethodPut, f.docs+"/by-path", pushBody("Other", "a", "x", nil), map[string]string{"If-Match": "*"})
	if det := errorDetails(raw); st != http.StatusPreconditionFailed || e.ErrorCode != "VERSION_CONFLICT" || len(det) != 0 {
		t.Fatalf("If-Match on a missing doc: %d %s %v", st, e.ErrorCode, det)
	}
}

func TestUpsertByPath_PathIsFolder409(t *testing.T) {
	t.Parallel()
	f := newByPathFx(t, "bypath-folder")
	if st, e, _ := f.put(t, pushBody("Plans/Doc", "a", "x", nil), nil); st != http.StatusCreated {
		t.Fatalf("create: %d %s", st, e.Error)
	}
	st, e, _ := f.put(t, pushBody("plans", "a", "x", nil), nil)
	if st != http.StatusConflict || e.ErrorCode != "DOC_PATH_IS_FOLDER" {
		t.Fatalf("put folder path: %d %s", st, e.ErrorCode)
	}
	st, e, _, _ = docRequest(t, f.env, f.client, f.token, http.MethodGet, f.docs+"/by-path?path=Plans", nil, nil)
	if st != http.StatusConflict || e.ErrorCode != "DOC_PATH_IS_FOLDER" {
		t.Fatalf("get folder path: %d %s", st, e.ErrorCode)
	}
}

func TestUpsertByPath_BadPath400(t *testing.T) {
	t.Parallel()
	f := newByPathFx(t, "bypath-bad")
	for _, path := range []string{"", "   ", "/", "///", " / / "} {
		st, e, _ := f.put(t, pushBody(path, "a", "x", nil), nil)
		if st != http.StatusBadRequest || e.ErrorCode != "DOC_PATH_INVALID" {
			t.Errorf("path %q: %d %s", path, st, e.ErrorCode)
		}
	}
	st, e, _, _ := docRequest(t, f.env, f.client, f.token, http.MethodGet, f.docs+"/by-path?path=%2F%2F", nil, nil)
	if st != http.StatusBadRequest || e.ErrorCode != "DOC_PATH_INVALID" {
		t.Errorf("get //: %d %s", st, e.ErrorCode)
	}
	st, e, _, _ = docRequest(t, f.env, f.client, f.token, http.MethodGet, f.docs+"/by-path?path=Nope/x", nil, nil)
	if st != http.StatusNotFound || e.ErrorCode != "DOC_NOT_FOUND" {
		t.Errorf("get missing: %d %s", st, e.ErrorCode)
	}
	if n := f.count(t, `SELECT count(*) FROM documents WHERE project_id = $1`, f.projID); n != 0 {
		t.Fatalf("bad paths created %d documents", n)
	}
	// A source without a repository, and lock without a source.
	st, e, _ = f.put(t, pushBody("Doc", "a", "x", map[string]any{"path": "x.md"}), nil)
	if st != http.StatusBadRequest || e.ErrorCode != "DOC_SOURCE_INVALID" {
		t.Errorf("bad source: %d %s", st, e.ErrorCode)
	}
	b := pushBody("Doc", "a", "x", nil)
	b["lock"] = true
	st, e, _ = f.put(t, b, nil)
	if st != http.StatusBadRequest || e.ErrorCode != "DOC_LOCK_NEEDS_SOURCE" {
		t.Errorf("lock without source: %d %s", st, e.ErrorCode)
	}
}

// Two first pushes of one path at once end with one document and no
// duplicate folders.
func TestUpsertByPath_ConcurrentCreatesOneDoc(t *testing.T) {
	t.Parallel()
	f := newByPathFx(t, "bypath-race")
	const n = 8
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i], _, _ = f.put(t, pushBody("Plans/Deep/Same", fmt.Sprintf("id-%d", i), "same text", src("paca", "c")), nil)
		}(i)
	}
	wg.Wait()
	created := 0
	for i, c := range codes {
		switch c {
		case http.StatusCreated:
			created++
		case http.StatusOK:
		default:
			t.Fatalf("writer %d: %d", i, c)
		}
	}
	if created != 1 {
		t.Fatalf("%d writers created the doc, want 1 (codes %v)", created, codes)
	}
	if d := f.count(t, `SELECT count(*) FROM documents WHERE project_id = $1 AND deleted_at IS NULL`, f.projID); d != 1 {
		t.Fatalf("%d documents, want 1", d)
	}
	if fo := f.count(t, `SELECT count(*) FROM doc_folders WHERE project_id = $1`, f.projID); fo != 2 {
		t.Fatalf("%d folders, want 2", fo)
	}
}

// A locked managed copy: plain writes are refused, pushes from its own
// repository and moves are not, and a person can unlock it.
func TestUpsertByPath_LockedCopy(t *testing.T) {
	t.Parallel()
	f := newByPathFx(t, "bypath-lock")
	b := pushBody("Plans/Doc", "a", "v1", src("paca", "c1"))
	b["lock"] = true
	st, e, _ := f.put(t, b, nil)
	if st != http.StatusCreated || assertDataMap(t, e)["source_locked"] != true {
		t.Fatalf("create locked: %d %v", st, e.Data)
	}
	id := assertDataMap(t, e)["id"].(string)
	doc := f.docs + "/" + id

	st, e, _, _ = docRequest(t, f.env, f.client, f.token, http.MethodPatch, doc, map[string]any{"content": docBlocks("x", "edited in Paca")}, nil)
	if st != http.StatusConflict || e.ErrorCode != "DOC_LOCKED" {
		t.Fatalf("plain write: %d %s", st, e.ErrorCode)
	}
	if st, e, _ = f.put(t, pushBody("Plans/Doc", "a", "v2", src("paca", "c2")), nil); st != http.StatusOK || assertDataMap(t, e)["changed"] != true {
		t.Fatalf("same-repository push: %d %s", st, e.Error)
	}
	if st, e, _ = f.put(t, pushBody("Plans/Doc", "a", "v3", src("other", "c3")), nil); st != http.StatusConflict || e.ErrorCode != "DOC_LOCKED" {
		t.Fatalf("other-repository push: %d %s", st, e.ErrorCode)
	}
	if st, e, _ = f.put(t, pushBody("Plans/Doc", "a", "v3", nil), nil); st != http.StatusConflict {
		t.Fatalf("push without source: %d %s", st, e.ErrorCode)
	}
	st, e, _, _ = docRequest(t, f.env, f.client, f.token, http.MethodPatch, doc, map[string]any{"folder_id": nil, "position": 4}, nil)
	if st != http.StatusOK || assertDataMap(t, e)["folder_id"] != nil {
		t.Fatalf("move: %d %s %v", st, e.Error, e.Data)
	}
	st, e, _, _ = docRequest(t, f.env, f.client, f.token, http.MethodPatch, doc, map[string]any{"source_locked": false}, nil)
	if st != http.StatusOK || assertDataMap(t, e)["source_locked"] != false {
		t.Fatalf("unlock: %d %s", st, e.Error)
	}
	st, e, _, _ = docRequest(t, f.env, f.client, f.token, http.MethodPatch, doc, map[string]any{"content": docBlocks("x", "edited in Paca")}, nil)
	if st != http.StatusOK {
		t.Fatalf("write after unlock: %d %s", st, e.Error)
	}
}

// folderDenyRole creates a project role that may read and write documents
// everywhere in the project except inside folder denied, and returns its id.
func folderDenyRole(t *testing.T, f *byPathFx, denied string) string {
	t.Helper()
	policy := map[string]any{
		"version": "2026-10-01",
		"statements": []any{
			map[string]any{"effect": "Allow", "actions": []string{"projects:read", "docs:read", "docs:write"},
				"resources": []string{"project/" + f.projID, "project/" + f.projID + "/*"}},
			map[string]any{"effect": "Deny", "actions": []string{"docs:read", "docs:write"},
				"resources":  []string{"project/" + f.projID + "/doc/*"},
				"conditions": map[string]any{"In": map[string]any{"doc.ancestor_folder_ids": []string{denied}}}},
		},
	}
	st, e, _, raw := docRequest(t, f.env, f.client, f.token, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/roles", f.env.base, f.projID), map[string]any{"name": "no-secret-" + uuid.NewString(), "policy": policy}, nil)
	if st != http.StatusCreated {
		t.Fatalf("create role: %d %s", st, raw)
	}
	return assertDataMap(t, e)["id"].(string)
}

// By-path writes and reads are authorized against the resolved folder or
// document: a role denied on a folder cannot write there (nor create folders
// below it), and cannot learn that a document there exists.
func TestUpsertByPath_DeniedFolder403(t *testing.T) {
	t.Parallel()
	f := newByPathFx(t, "bypath-owner")
	st, e, _ := f.put(t, pushBody("Secret/Hidden", "a", "classified", nil), nil)
	if st != http.StatusCreated {
		t.Fatalf("owner create: %d %s", st, e.Error)
	}
	secretID := assertDataMap(t, e)["folder_id"].(string)
	if st, e, _ = f.put(t, pushBody("Open/Readme", "a", "hi", nil), nil); st != http.StatusCreated {
		t.Fatalf("owner create open: %d %s", st, e.Error)
	}

	roleID := folderDenyRole(t, f, secretID)
	seedUser(t, f.env, "bypath-limited", "bypathlimited1", "Limited")
	user, err := f.env.userRepo.FindByUsername(f.env.ctx, "bypath-limited")
	if err != nil {
		t.Fatal(err)
	}
	addMemberViaAPI(t, f.env, f.client, f.token, f.projID, user.ID.String(), roleID)
	mc, mt := taskMemberLogin(t, f.env, "bypath-limited", "bypathlimited1")
	put := func(path string) (int, envelope) {
		st, e, _, _ := docRequest(t, f.env, mc, mt, http.MethodPut, f.docs+"/by-path", pushBody(path, "a", "member text", nil), nil)
		return st, e
	}
	get := func(path string) (int, envelope) {
		st, e, _, _ := docRequest(t, f.env, mc, mt, http.MethodGet, f.docs+"/by-path?path="+path, nil, nil)
		return st, e
	}

	if st, e := put("Secret/New"); st != http.StatusForbidden {
		t.Fatalf("create in denied folder: %d %s", st, e.Error)
	}
	if st, e := put("Secret/Sub/New"); st != http.StatusForbidden {
		t.Fatalf("create below denied folder: %d %s", st, e.Error)
	}
	if st, e := put("Secret/Hidden"); st != http.StatusForbidden {
		t.Fatalf("overwrite in denied folder: %d %s", st, e.Error)
	}
	if n := f.count(t, `SELECT count(*) FROM doc_folders WHERE project_id = $1 AND name = 'Sub'`, f.projID); n != 0 {
		t.Fatalf("a denied write created %d folders", n)
	}
	var content string
	if err := f.env.db.GetContext(f.env.ctx, &content, `SELECT content::text FROM documents WHERE title = 'Hidden' AND project_id = $1`, f.projID); err != nil || strings.Contains(content, "member text") {
		t.Fatalf("hidden doc was overwritten: %v", err)
	}

	hidden, he := get("Secret/Hidden")
	missing, me := get("Secret/Nothing")
	if hidden != http.StatusNotFound || missing != http.StatusNotFound || he.ErrorCode != me.ErrorCode || he.Error != me.Error {
		t.Fatalf("read of a hidden doc: %d %v; of a missing one: %d %v", hidden, he, missing, me)
	}

	if st, e := put("Open/Mine"); st != http.StatusCreated {
		t.Fatalf("create in allowed folder: %d %s", st, e.Error)
	}
	if st, e := get("Open/Readme"); st != http.StatusOK {
		t.Fatalf("read allowed doc: %d %s", st, e.Error)
	}
}

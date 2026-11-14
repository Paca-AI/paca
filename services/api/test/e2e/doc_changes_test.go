package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
)

// The document change feed and batch read (Paca-AI/paca#747, PR 2), against
// the real router, authorizer and database.

type feedDoc struct {
	ID          string  `json:"id"`
	FolderID    *string `json:"folder_id"`
	Title       string  `json:"title"`
	ContentHash *string `json:"content_hash"`
	UpdatedAt   string  `json:"updated_at"`
	Deleted     bool    `json:"deleted"`
}

type feedFolder struct {
	ID       string  `json:"id"`
	ParentID *string `json:"parent_id"`
	Name     string  `json:"name"`
}

type feedReply struct {
	Cursor  string       `json:"cursor"`
	Reset   bool         `json:"reset"`
	Docs    []feedDoc    `json:"docs"`
	Folders []feedFolder `json:"folders"` // nil = JSON null
	raw     string
}

func (f feedReply) doc(id string) *feedDoc {
	for i := range f.Docs {
		if f.Docs[i].ID == id {
			return &f.Docs[i]
		}
	}
	return nil
}

func (f feedReply) ids() []string {
	out := make([]string, 0, len(f.Docs))
	for _, d := range f.Docs {
		out = append(out, d.ID)
	}
	sort.Strings(out)
	return out
}

func sortedIDs(ids ...string) []string {
	out := append([]string{}, ids...)
	sort.Strings(out)
	return out
}

func sameIDs(a, b []string) bool {
	return strings.Join(a, ",") == strings.Join(b, ",")
}

// docUser is a logged-in user and the docs URL of the project under test.
type docUser struct {
	t      *testing.T
	env    *e2eEnv
	client *http.Client
	token  string
	docs   string
}

func (u docUser) do(method, path string, body any) (int, envelope, []byte) {
	u.t.Helper()
	status, e, _, raw := docRequest(u.t, u.env, u.client, u.token, method, u.docs+path, body, nil)
	return status, e, raw
}

func (u docUser) must(want int, method, path string, body any) map[string]any {
	u.t.Helper()
	status, e, raw := u.do(method, path, body)
	if status != want {
		u.t.Fatalf("%s %s: status %d, want %d: %s", method, path, status, want, raw)
	}
	m, _ := e.Data.(map[string]any)
	return m
}

func (u docUser) createDoc(title string, folderID *string, text string) string {
	u.t.Helper()
	body := map[string]any{"title": title, "content": docBlocks("b", text)}
	if folderID != nil {
		body["folder_id"] = *folderID
	}
	id, _ := u.must(http.StatusCreated, http.MethodPost, "", body)["id"].(string)
	return id
}

func (u docUser) createFolder(name string, parentID *string) string {
	u.t.Helper()
	return createDocFolderViaAPI(u.t, u.env, u.client, u.token, u.docs, name, parentID)
}

// pull calls the change feed. An empty since sends no cursor.
func (u docUser) pull(since string) feedReply {
	u.t.Helper()
	path := "/changes"
	if since != "" {
		path += "?since=" + since
	}
	status, _, raw := u.do(http.MethodGet, path, nil)
	if status != http.StatusOK {
		u.t.Fatalf("GET %s: status %d: %s", path, status, raw)
	}
	var env struct {
		Data feedReply `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		u.t.Fatalf("decode feed reply %s: %v", raw, err)
	}
	env.Data.raw = string(raw)
	if env.Data.Cursor == "" || env.Data.Docs == nil {
		u.t.Fatalf("feed reply without a cursor or a docs list: %s", raw)
	}
	return env.Data
}

// docFeedOwner sets up a project and its creator, who can read every doc.
func docFeedOwner(t *testing.T, username string) (docUser, string) {
	t.Helper()
	env, client, token, projID, docs := docSeqSetup(t, username)
	return docUser{t: t, env: env, client: client, token: token, docs: docs}, projID
}

// docReaderPolicy lets a member read the project's docs except those in or
// under the denied folders.
func docReaderPolicy(projectID string, deniedFolders ...string) map[string]any {
	statements := []any{map[string]any{
		"effect":    "Allow",
		"actions":   []string{"projects:read", "docs:read"},
		"resources": []string{"project/" + projectID, "project/" + projectID + "/*"},
	}}
	if len(deniedFolders) > 0 {
		statements = append(statements, map[string]any{
			"effect":     "Deny",
			"actions":    []string{"docs:read"},
			"resources":  []string{"project/" + projectID + "/doc/*"},
			"conditions": map[string]any{"In": map[string]any{"doc.ancestor_folder_ids": deniedFolders}},
		})
	}
	return map[string]any{"version": "2026-10-01", "statements": statements}
}

// addDocReader adds a second user to the owner's project with a role that
// denies the given folders, and returns the user and the role id.
func addDocReader(t *testing.T, owner docUser, projID, username string, deniedFolders ...string) (docUser, string) {
	t.Helper()
	env := owner.env
	seedUser(t, env, username, "docreaderpass1", username)
	user, err := env.userRepo.FindByUsername(env.ctx, username)
	if err != nil {
		t.Fatalf("find user %q: %v", username, err)
	}
	rolesURL := fmt.Sprintf("%s/api/v1/projects/%s/roles", env.base, projID)
	status, e, _, raw := docRequest(t, env, owner.client, owner.token, http.MethodPost, rolesURL,
		map[string]any{"name": "doc-reader-" + uuid.NewString(), "policy": docReaderPolicy(projID, deniedFolders...)}, nil)
	if status != http.StatusCreated {
		t.Fatalf("create role: %d %s", status, raw)
	}
	roleID, _ := assertDataMap(t, e)["id"].(string)
	addMemberViaAPI(t, env, owner.client, owner.token, projID, user.ID.String(), roleID)
	client, token := taskMemberLogin(t, env, username, "docreaderpass1")
	return docUser{t: t, env: env, client: client, token: token, docs: owner.docs}, roleID
}

// setDocReaderPolicy replaces the reader role's denied folders.
func setDocReaderPolicy(t *testing.T, owner docUser, projID, roleID string, deniedFolders ...string) {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/projects/%s/roles/%s", owner.env.base, projID, roleID)
	status, _, _, raw := docRequest(t, owner.env, owner.client, owner.token, http.MethodPut, url,
		map[string]any{"name": "doc-reader-" + uuid.NewString(), "policy": docReaderPolicy(projID, deniedFolders...)}, nil)
	if status != http.StatusOK {
		t.Fatalf("update role: %d %s", status, raw)
	}
}

func TestChanges_NoCursorResets(t *testing.T) {
	t.Parallel()
	owner, _ := docFeedOwner(t, "feed-nocursor")

	folder := owner.createFolder("Plans", nil)
	a := owner.createDoc("A", nil, "alpha")
	b := owner.createDoc("B", &folder, "beta")
	gone := owner.createDoc("Gone", nil, "gamma")
	owner.must(http.StatusNoContent, http.MethodDelete, "/"+gone, nil)

	got := owner.pull("")
	if !got.Reset {
		t.Fatal("reset = false on a first pull")
	}
	if !sameIDs(got.ids(), sortedIDs(a, b)) {
		t.Fatalf("docs = %v, want the two live documents only (no tombstone)", got.ids())
	}
	if d := got.doc(a); d.Title != "A" || d.FolderID != nil || d.Deleted || d.ContentHash == nil || *d.ContentHash != docdom.ContentHash(docBlocks("b", "alpha")) {
		t.Fatalf("doc A = %+v", d)
	}
	if d := got.doc(b); d.FolderID == nil || *d.FolderID != folder {
		t.Fatalf("doc B = %+v, want folder %s", d, folder)
	}
	if len(got.Folders) != 1 || got.Folders[0].ID != folder || got.Folders[0].Name != "Plans" || got.Folders[0].ParentID != nil {
		t.Fatalf("folders = %+v, want the one folder", got.Folders)
	}

	// A project with nothing in it: a reset with empty lists, not null.
	empty, _ := docFeedOwner(t, "feed-nocursor-empty")
	first := empty.pull("")
	if !first.Reset || len(first.Docs) != 0 || first.Folders == nil || len(first.Folders) != 0 {
		t.Fatalf("empty project: %s", first.raw)
	}
	if again := empty.pull(first.Cursor); again.Reset || again.Cursor != first.Cursor {
		t.Fatalf("empty project, second pull: %s", again.raw)
	}
}

func TestChanges_UpToDateReturnsNothing(t *testing.T) {
	t.Parallel()
	owner, _ := docFeedOwner(t, "feed-uptodate")
	owner.createFolder("Plans", nil)
	a := owner.createDoc("A", nil, "alpha")
	first := owner.pull("")

	check := func(when string) {
		t.Helper()
		got := owner.pull(first.Cursor)
		if got.Reset || len(got.Docs) != 0 || got.Folders != nil {
			t.Fatalf("%s: %s; want no reset, no docs, folders null", when, got.raw)
		}
		if got.Cursor != first.Cursor {
			t.Fatalf("%s: cursor moved from %q to %q", when, first.Cursor, got.Cursor)
		}
	}
	check("nothing happened")

	// The same blocks under other ids is not a change (PR 1), so not in the feed.
	owner.must(http.StatusOK, http.MethodPatch, "/"+a, map[string]any{"content": docBlocks("other-id", "alpha")})
	check("after a no-op write")
}

func TestChanges_EditMoveDelete(t *testing.T) {
	t.Parallel()
	owner, _ := docFeedOwner(t, "feed-editmovedelete")
	folder := owner.createFolder("Plans", nil)
	edited := owner.createDoc("Edited", nil, "one")
	moved := owner.createDoc("Moved", nil, "two")
	deleted := owner.createDoc("Deleted", nil, "three")
	owner.createDoc("Untouched", nil, "four")
	first := owner.pull("")

	newHash, _ := owner.must(http.StatusOK, http.MethodPatch, "/"+edited, map[string]any{"content": docBlocks("b", "one, changed")})["content_hash"].(string)
	owner.must(http.StatusOK, http.MethodPatch, "/"+moved, map[string]any{"folder_id": folder})
	owner.must(http.StatusNoContent, http.MethodDelete, "/"+deleted, nil)
	// Created and deleted between two pulls: the client never had it.
	shortLived := owner.createDoc("Short-lived", nil, "five")
	owner.must(http.StatusNoContent, http.MethodDelete, "/"+shortLived, nil)

	got := owner.pull(first.Cursor)
	if got.Reset {
		t.Fatalf("reset on ordinary changes: %s", got.raw)
	}
	if !sameIDs(got.ids(), sortedIDs(edited, moved, deleted)) {
		t.Fatalf("docs = %v, want the three changed documents", got.ids())
	}
	if d := got.doc(edited); d.Deleted || d.ContentHash == nil || *d.ContentHash != newHash || newHash == *first.doc(edited).ContentHash {
		t.Fatalf("edited doc = %+v, want the new hash %s", d, newHash)
	}
	if d := got.doc(moved); d.Deleted || d.FolderID == nil || *d.FolderID != folder || *d.ContentHash != *first.doc(moved).ContentHash {
		t.Fatalf("moved doc = %+v, want folder %s and the same hash", d, folder)
	}
	if d := got.doc(deleted); !d.Deleted || d.Title != "" || d.ContentHash != nil || d.FolderID != nil {
		t.Fatalf("deleted doc = %+v, want a bare tombstone", d)
	}
	if got.Folders != nil {
		t.Fatalf("folders sent though no folder changed: %s", got.raw)
	}
	if got.Cursor == first.Cursor {
		t.Fatal("the cursor did not move")
	}
	if next := owner.pull(got.Cursor); next.Reset || len(next.Docs) != 0 {
		t.Fatalf("pull after catching up: %s", next.raw)
	}
}

func TestChanges_FolderRenameSendsTree(t *testing.T) {
	t.Parallel()
	owner, _ := docFeedOwner(t, "feed-folders")
	plans := owner.createFolder("Plans", nil)
	sub := owner.createFolder("2026", &plans)
	inSub := owner.createDoc("In sub", &sub, "x")
	first := owner.pull("")

	owner.must(http.StatusOK, http.MethodPatch, "/folders/"+plans, map[string]any{"name": "Roadmaps"})
	got := owner.pull(first.Cursor)
	if got.Reset || len(got.Docs) != 0 {
		t.Fatalf("after a rename: %s; want no reset and no docs", got.raw)
	}
	names := map[string]string{}
	for _, f := range got.Folders {
		names[f.ID] = f.Name
	}
	if len(got.Folders) != 2 || names[plans] != "Roadmaps" || names[sub] != "2026" {
		t.Fatalf("folders = %+v, want the full tree with the new name", got.Folders)
	}

	// Deleting a folder: its documents come back at the root, and the tree
	// is sent without the folder (and without the child it took with it).
	owner.must(http.StatusNoContent, http.MethodDelete, "/folders/"+sub, nil)
	after := owner.pull(got.Cursor)
	if after.Reset || !sameIDs(after.ids(), []string{inSub}) || after.doc(inSub).FolderID != nil || after.doc(inSub).Deleted {
		t.Fatalf("after a folder delete: %s; want the document, live, with folder_id null", after.raw)
	}
	if len(after.Folders) != 1 || after.Folders[0].ID != plans {
		t.Fatalf("folders = %+v, want only %s", after.Folders, plans)
	}
}

func TestChanges_AccessLossResets(t *testing.T) {
	t.Parallel()
	owner, projID := docFeedOwner(t, "feed-access-owner")
	secret := owner.createFolder("Secret", nil)
	open := owner.createFolder("Open", nil)
	team := owner.createFolder("Team", nil)
	d1 := owner.createDoc("D1", nil, "one")
	d2 := owner.createDoc("D2", &open, "two")
	d3 := owner.createDoc("D3", &team, "three")
	hidden := owner.createDoc("Hidden", &secret, "four")
	reader, roleID := addDocReader(t, owner, projID, "feed-access-reader", secret)

	first := reader.pull("")
	if !first.Reset || !sameIDs(first.ids(), sortedIDs(d1, d2, d3)) {
		t.Fatalf("first pull: %v, want D1, D2 and D3 but not the hidden doc", first.ids())
	}

	// expectReset pulls with the previous cursor and checks the reply is a
	// reset listing exactly want.
	expectReset := func(when, since string, want ...string) feedReply {
		t.Helper()
		got := reader.pull(since)
		if !got.Reset {
			t.Fatalf("%s: reset = false: %s", when, got.raw)
		}
		if !sameIDs(got.ids(), sortedIDs(want...)) {
			t.Fatalf("%s: docs = %v, want %v", when, got.ids(), sortedIDs(want...))
		}
		for _, d := range got.Docs {
			if d.Deleted {
				t.Fatalf("%s: a reset carries a tombstone: %s", when, got.raw)
			}
		}
		if got.Folders == nil {
			t.Fatalf("%s: a reset without the folder tree", when)
		}
		// And the reset is not sticky: the next pull is quiet.
		if next := reader.pull(got.Cursor); next.Reset || len(next.Docs) != 0 {
			t.Fatalf("%s: the pull after the reset: %s", when, next.raw)
		}
		return got
	}

	// 1. Another user moves a doc into the denied folder.
	owner.must(http.StatusOK, http.MethodPatch, "/"+d1, map[string]any{"folder_id": secret})
	got := expectReset("doc moved into a denied folder", first.Cursor, d2, d3)

	// 2. A folder moves under the denied one: its docs are not written at all.
	owner.must(http.StatusOK, http.MethodPatch, "/folders/"+team, map[string]any{"parent_id": secret})
	got = expectReset("folder moved under a denied folder", got.Cursor, d2)

	// 3. A policy edit denies another folder.
	setDocReaderPolicy(t, owner, projID, roleID, secret, open)
	got = expectReset("policy edit denies a folder", got.Cursor)

	// 4. Access comes back the same way: the docs reappear through a reset.
	setDocReaderPolicy(t, owner, projID, roleID)
	got = expectReset("policy edit allows everything", got.Cursor, d1, d2, d3, hidden)

	// The owner, whose access never changed, saw ordinary changes throughout.
	ownerFirst := owner.pull("")
	owner.must(http.StatusOK, http.MethodPatch, "/"+d2, map[string]any{"title": "D2 renamed"})
	if o := owner.pull(ownerFirst.Cursor); o.Reset || !sameIDs(o.ids(), []string{d2}) {
		t.Fatalf("owner: %s", o.raw)
	}
	if r := reader.pull(got.Cursor); r.Reset || !sameIDs(r.ids(), []string{d2}) || r.doc(d2).Title != "D2 renamed" {
		t.Fatalf("reader after an ordinary edit: %s", r.raw)
	}
}

func TestChanges_BadCursorResets(t *testing.T) {
	t.Parallel()
	owner, projID := docFeedOwner(t, "feed-badcursor")
	mine := owner.createDoc("Mine", nil, "x")
	good := owner.pull("")

	// A second project of the same user, with its own document and cursor.
	otherProj := createProjectForTasksViaAPI(t, owner.env, owner.client, owner.token)
	other := owner
	other.docs = fmt.Sprintf("%s/api/v1/projects/%s/docs", owner.env.base, otherProj)
	theirs := other.createDoc("Theirs", nil, "y")
	otherCursor := other.pull("").Cursor

	forged := docdom.EncodeChangeCursor(docdom.ChangeCursor{ProjectID: uuid.MustParse(projID), Seq: 1 << 40, Fingerprint: "00000000000000000000000000000000"})
	cases := map[string]string{
		"garbage":                  "this-is-not-a-cursor",
		"not base64":               "%25%25%25",
		"truncated":                good.Cursor[:len(good.Cursor)/2],
		"another project":          otherCursor,
		"sequence from the future": forged,
		"very long":                strings.Repeat("A", 4000),
	}
	for name, cursor := range cases {
		got := owner.pull(cursor)
		if !got.Reset {
			t.Errorf("%s: reset = false: %s", name, got.raw)
		}
		if !sameIDs(got.ids(), []string{mine}) || strings.Contains(got.raw, theirs) {
			t.Errorf("%s: docs = %v, want only this project's document", name, got.ids())
		}
		if c, ok := docdom.DecodeChangeCursor(got.Cursor); !ok || c.ProjectID.String() != projID {
			t.Errorf("%s: the new cursor is not for this project: %+v", name, c)
		}
	}
}

func TestChanges_DeletedDocInvisibleToCallerNotLeaked(t *testing.T) {
	t.Parallel()
	owner, projID := docFeedOwner(t, "feed-leak-owner")
	secret := owner.createFolder("Secret", nil)
	visible := owner.createDoc("Visible", nil, "one")
	seen := owner.createDoc("Seen then deleted", nil, "two")
	hiddenOld := owner.createDoc("Classified old", &secret, "three")
	reader, roleID := addDocReader(t, owner, projID, "feed-leak-reader", secret)

	first := reader.pull("")
	if !sameIDs(first.ids(), sortedIDs(visible, seen)) {
		t.Fatalf("first pull: %v", first.ids())
	}

	noLeak := func(when string, got feedReply, hidden ...string) {
		t.Helper()
		for _, id := range hidden {
			if strings.Contains(got.raw, id) {
				t.Fatalf("%s: the reply names hidden document %s: %s", when, id, got.raw)
			}
		}
		if strings.Contains(got.raw, "Classified") {
			t.Fatalf("%s: the reply carries a hidden title: %s", when, got.raw)
		}
	}

	// Deletions the reader may and may not learn about.
	owner.must(http.StatusNoContent, http.MethodDelete, "/"+seen, nil)
	owner.must(http.StatusNoContent, http.MethodDelete, "/"+hiddenOld, nil)
	hiddenNew := owner.createDoc("Classified new", &secret, "four")
	owner.must(http.StatusNoContent, http.MethodDelete, "/"+hiddenNew, nil)
	// Visible at the cursor, then moved out of sight and deleted there.
	movedAway := owner.createDoc("Open for now", nil, "five")
	second := reader.pull(first.Cursor)
	if second.Reset || !sameIDs(second.ids(), sortedIDs(seen, movedAway)) || !second.doc(seen).Deleted {
		t.Fatalf("second pull: %s; want a tombstone for the doc the reader had, and the new doc", second.raw)
	}
	noLeak("deleted inside the denied folder", second, hiddenOld, hiddenNew)

	owner.must(http.StatusOK, http.MethodPatch, "/"+movedAway, map[string]any{"folder_id": secret, "title": "Classified moved"})
	owner.must(http.StatusNoContent, http.MethodDelete, "/"+movedAway, nil)
	third := reader.pull(second.Cursor)
	if !third.Reset || !sameIDs(third.ids(), []string{visible}) {
		t.Fatalf("third pull: %s; want a reset listing only the visible doc", third.raw)
	}
	if strings.Contains(third.raw, "Classified moved") {
		t.Fatalf("third pull carries the title the doc got after it left the reader's sight: %s", third.raw)
	}

	// Deleting the denied folder moves its (deleted) documents to the root,
	// where the policy no longer hides them. They must still not surface as
	// tombstones, from any cursor the reader holds.
	owner.must(http.StatusNoContent, http.MethodDelete, "/folders/"+secret, nil)
	for name, cursor := range map[string]string{"first": first.Cursor, "second": second.Cursor, "third": third.Cursor, "none": ""} {
		got := reader.pull(cursor)
		noLeak("after the folder delete, cursor "+name, got, hiddenOld, hiddenNew)
		for _, d := range got.Docs {
			// The reader held "seen" and, at the second cursor, the doc that
			// was later moved away: a bare tombstone for those is correct.
			if d.Deleted && d.ID != seen && d.ID != movedAway {
				t.Fatalf("cursor %s: tombstone for %s, which the reader never had: %s", name, d.ID, got.raw)
			}
			if d.Deleted && (d.Title != "" || d.FolderID != nil || d.ContentHash != nil) {
				t.Fatalf("cursor %s: a tombstone with details: %s", name, got.raw)
			}
		}
		if got.Reset && !sameIDs(got.ids(), []string{visible}) {
			t.Fatalf("cursor %s: a reset listing %v, want only the visible doc: %s", name, got.ids(), got.raw)
		}
	}

	// The same, with nothing else to trigger a reset: a document that is
	// created and deleted inside a denied folder after the reader's cursor,
	// and whose folder is then deleted, is in no reply at all.
	vault := owner.createFolder("Vault", nil)
	setDocReaderPolicy(t, owner, projID, roleID, vault)
	base := reader.pull("")
	ghost := owner.createDoc("Classified ghost", &vault, "six")
	owner.must(http.StatusNoContent, http.MethodDelete, "/"+ghost, nil)
	owner.must(http.StatusNoContent, http.MethodDelete, "/folders/"+vault, nil)
	got := reader.pull(base.Cursor)
	if got.Reset || len(got.Docs) != 0 {
		t.Fatalf("after the ghost: %s; want no reset and no docs", got.raw)
	}
	noLeak("created and deleted out of sight", got, ghost)
}

func TestBatchGet_ScopedAndCapped(t *testing.T) {
	t.Parallel()
	owner, projID := docFeedOwner(t, "feed-batch-owner")
	secret := owner.createFolder("Secret", nil)
	a := owner.createDoc("A", nil, "alpha")
	b := owner.createDoc("B", nil, "beta")
	hidden := owner.createDoc("Classified", &secret, "gamma")
	deleted := owner.createDoc("Deleted", nil, "delta")
	owner.must(http.StatusNoContent, http.MethodDelete, "/"+deleted, nil)
	otherProj := createProjectForTasksViaAPI(t, owner.env, owner.client, owner.token)
	other := owner
	other.docs = fmt.Sprintf("%s/api/v1/projects/%s/docs", owner.env.base, otherProj)
	foreign := other.createDoc("Foreign", nil, "epsilon")
	reader, _ := addDocReader(t, owner, projID, "feed-batch-reader", secret)

	items := func(u docUser, ids ...string) ([]map[string]any, string) {
		t.Helper()
		status, e, raw := u.do(http.MethodPost, "/batch-get", map[string]any{"ids": ids})
		if status != http.StatusOK {
			t.Fatalf("batch-get: %d %s", status, raw)
		}
		list, _ := assertDataMap(t, e)["items"].([]any)
		out := make([]map[string]any, 0, len(list))
		for _, it := range list {
			out = append(out, it.(map[string]any))
		}
		return out, string(raw)
	}

	got, raw := items(reader, b, hidden, a, deleted, foreign, uuid.NewString(), b)
	if len(got) != 2 || got[0]["id"] != b || got[1]["id"] != a {
		t.Fatalf("reader got %s; want B then A and nothing else", raw)
	}
	if strings.Contains(raw, "Classified") || strings.Contains(raw, "Foreign") || strings.Contains(raw, "Deleted") {
		t.Fatalf("the reply carries a document the reader may not get: %s", raw)
	}
	if got[0]["content_hash"] != docdom.ContentHash(docBlocks("b", "beta")) || got[0]["content"] == nil || got[0]["title"] != "B" {
		t.Fatalf("item = %v, want the full document with its hash", got[0])
	}
	// The owner can read the hidden one, but still not another project's.
	if got, raw := items(owner, hidden, foreign); len(got) != 1 || got[0]["id"] != hidden {
		t.Fatalf("owner got %s; want only the doc in this project", raw)
	}

	ids := make([]string, 101)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	if status, e, raw := reader.do(http.MethodPost, "/batch-get", map[string]any{"ids": ids}); status != http.StatusBadRequest || e.ErrorCode != "BAD_REQUEST" {
		t.Fatalf("101 ids: %d %s", status, raw)
	}
	ids[100] = a
	if got, raw := items(reader, ids[1:]...); len(got) != 1 {
		t.Fatalf("100 ids: %s", raw)
	}

	// A user who is not a member gets nothing at all.
	seedUser(t, owner.env, "feed-batch-outsider", "outsiderpass1", "Outsider")
	oc, ot := taskMemberLogin(t, owner.env, "feed-batch-outsider", "outsiderpass1")
	outsider := docUser{t: t, env: owner.env, client: oc, token: ot, docs: owner.docs}
	for _, req := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/batch-get", map[string]any{"ids": []string{a}}},
		{http.MethodGet, "/changes", nil},
	} {
		if status, _, raw := outsider.do(req.method, req.path, req.body); status != http.StatusForbidden && status != http.StatusNotFound {
			t.Fatalf("outsider %s %s: %d %s", req.method, req.path, status, raw)
		} else if strings.Contains(string(raw), a) {
			t.Fatalf("outsider %s %s got a document: %s", req.method, req.path, raw)
		}
	}
}

// A write that commits while a feed request is running must land in the next
// pull: the request reads the sequence and the documents from one snapshot.
func TestChanges_WriteDuringRequestLandsInNextPull(t *testing.T) {
	t.Parallel()
	owner, projID := docFeedOwner(t, "feed-midrequest")
	project := uuid.MustParse(projID)
	before := owner.createDoc("Before", nil, "one")
	repo := pgRepo.NewDocumentRepository(owner.env.db)

	var during string
	var cursorSeq int64
	var fingerprint string
	err := repo.ReadDocChanges(owner.env.ctx, func(r docdom.ChangeReader) error {
		seq, _, err := r.DocSyncState(owner.env.ctx, project)
		if err != nil {
			return err
		}
		cursorSeq = seq
		// Two writes commit now, after the sequence was read: a new doc and
		// an edit of the existing one.
		during = owner.createDoc("During", nil, "two")
		owner.must(http.StatusOK, http.MethodPatch, "/"+before, map[string]any{"title": "Before, edited"})

		docs, err := r.ListVisibleDocs(owner.env.ctx, project)
		if err != nil {
			return err
		}
		if len(docs) != 1 || docs[0].ID.String() != before || docs[0].Title != "Before" {
			t.Errorf("the snapshot saw %d docs (first title %q); want only the doc as it was at seq %d", len(docs), docs[0].Title, seq)
		}
		if again, _, _ := r.DocSyncState(owner.env.ctx, project); again != seq {
			t.Errorf("the sequence moved inside the snapshot: %d -> %d", seq, again)
		}
		fingerprint, err = r.VisibleFingerprint(owner.env.ctx, project, nil)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	got := owner.pull(docdom.EncodeChangeCursor(docdom.ChangeCursor{ProjectID: project, Seq: cursorSeq, Fingerprint: fingerprint}))
	if got.Reset || !sameIDs(got.ids(), sortedIDs(before, during)) || got.doc(before).Title != "Before, edited" {
		t.Fatalf("next pull: %s; want both writes as ordinary changes", got.raw)
	}
}

// A row the hash backfill has not reached yet still reports its real hash,
// never the hash of an empty document.
func TestChanges_UnbackfilledRowReportsItsRealHash(t *testing.T) {
	t.Parallel()
	owner, _ := docFeedOwner(t, "feed-nullhash")
	id := owner.createDoc("Legacy", nil, "old content")
	first := owner.pull("")

	tx, err := owner.env.db.BeginTxx(owner.env.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(owner.env.ctx, `SELECT set_config('paca.skip_doc_seq', 'on', true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(owner.env.ctx, `UPDATE documents SET content_hash = NULL WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	want := docdom.ContentHash(docBlocks("b", "old content"))
	if got := owner.pull("").doc(id); got.ContentHash == nil || *got.ContentHash != want {
		t.Fatalf("reset: content_hash = %v, want %s", got.ContentHash, want)
	}
	// Not a change either: the client's cursor is still current.
	if got := owner.pull(first.Cursor); got.Reset || len(got.Docs) != 0 {
		t.Fatalf("pull with the old cursor: %s", got.raw)
	}
	owner.must(http.StatusOK, http.MethodPatch, "/"+id, map[string]any{"title": "Legacy, renamed"})
	if got := owner.pull(first.Cursor).doc(id); got == nil || got.ContentHash == nil || *got.ContentHash != want {
		t.Fatalf("change entry: %+v, want hash %s", got, want)
	}
}

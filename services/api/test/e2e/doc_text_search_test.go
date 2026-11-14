package e2e_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"testing"
	"time"

	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
	"github.com/Paca-AI/api/internal/worker"
)

// Search across formatting with a plain-text column and trigram indexes
// (Paca-AI/paca#747, PR 7).

type searchHit struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	MatchedIn string `json:"matched_in"`
	Snippet   string `json:"snippet"`
	Block     int    `json:"block"`
}

// search calls GET /docs/search and returns the hits.
func (u docUser) search(q string) []searchHit {
	u.t.Helper()
	status, _, raw := u.do(http.MethodGet, "/search?q="+url.QueryEscape(q), nil)
	if status != http.StatusOK {
		u.t.Fatalf("search %q: status %d: %s", q, status, raw)
	}
	var env struct {
		Data struct {
			Items []searchHit `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		u.t.Fatalf("decode search reply %s: %v", raw, err)
	}
	return env.Data.Items
}

func hitIDs(hits []searchHit) []string {
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ID)
	}
	return sortedIDs(ids...)
}

// createDocWith creates a document with the given BlockNote content.
func (u docUser) createDocWith(title string, folderID *string, content string) string {
	u.t.Helper()
	body := map[string]any{"title": title, "content": json.RawMessage(content)}
	if folderID != nil {
		body["folder_id"] = *folderID
	}
	id, _ := u.must(http.StatusCreated, http.MethodPost, "", body)["id"].(string)
	return id
}

// clearContentText makes documents look like rows written before migration
// 000090, without numbering the change.
func clearContentText(t *testing.T, env *e2eEnv, ids ...string) {
	t.Helper()
	tx, err := env.db.BeginTxx(env.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(env.ctx, `SELECT set_config('paca.skip_doc_seq', 'on', true)`); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(env.ctx, `UPDATE documents SET content_text = NULL WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func storedContentText(t *testing.T, env *e2eEnv, id string) *string {
	t.Helper()
	var text *string
	if err := env.db.GetContext(env.ctx, &text, `SELECT content_text FROM documents WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	return text
}

const boldPhraseContent = `[
 {"type":"heading","props":{"level":1},"content":[{"type":"text","text":"Runbook","styles":{}}],"children":[]},
 {"type":"paragraph","content":[{"type":"text","text":"Nothing here.","styles":{}}],"children":[
   {"type":"paragraph","content":[
     {"type":"text","text":"Always run the ","styles":{}},
     {"type":"text","text":"database","styles":{"bold":true}},
     {"type":"text","text":" migration","styles":{"italic":true}},
     {"type":"text","text":" first.","styles":{}}],"children":[]}]}
]`

func TestSearch_PhraseAcrossFormatting(t *testing.T) {
	t.Parallel()
	owner, _ := docFeedOwner(t, "search-across-fmt")
	id := owner.createDocWith("Ops", nil, boldPhraseContent)
	owner.createDoc("Other", nil, "database tables")

	hits := owner.search("the DATABASE migration")
	if len(hits) != 1 || hits[0].ID != id {
		t.Fatalf("hits = %+v, want only the runbook", hits)
	}
	if hits[0].MatchedIn != "content" || hits[0].Snippet != "Always run the database migration first." {
		t.Fatalf("hit = %+v", hits[0])
	}
	if got := storedContentText(t, owner.env, id); got == nil || *got != "Runbook\nNothing here.\nAlways run the database migration first." {
		t.Fatalf("content_text = %v", got)
	}
}

func TestSearch_ReportsBlock(t *testing.T) {
	t.Parallel()
	owner, _ := docFeedOwner(t, "search-block")
	id := owner.createDocWith("Ops", nil, boldPhraseContent)

	hits := owner.search("migration first")
	if len(hits) != 1 || hits[0].ID != id || hits[0].Block != 3 {
		t.Fatalf("hits = %+v, want the runbook at block 3 (the nested paragraph)", hits)
	}
	// A title match carries no block.
	status, _, raw := owner.do(http.MethodGet, "/search?q=ops", nil)
	if status != http.StatusOK {
		t.Fatalf("title search: %d %s", status, raw)
	}
	var env struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || len(env.Data.Items) != 1 {
		t.Fatalf("title search reply %s (%v)", raw, err)
	}
	if _, has := env.Data.Items[0]["block"]; has || env.Data.Items[0]["matched_in"] != "title" {
		t.Fatalf("title hit = %v, want matched_in title and no block", env.Data.Items[0])
	}
}

func TestSearch_ScopeStillApplies(t *testing.T) {
	t.Parallel()
	owner, projID := docFeedOwner(t, "search-scope-owner")
	secret := owner.createFolder("Secret", nil)
	visible := owner.createDoc("Visible", nil, "the zebracorn plan")
	hidden := owner.createDoc("Hidden", &secret, "the zebracorn secret")
	reader, _ := addDocReader(t, owner, projID, "search-scope-reader", secret)

	if got := hitIDs(owner.search("zebracorn")); !sameIDs(got, sortedIDs(visible, hidden)) {
		t.Fatalf("owner hits %v, want both", got)
	}
	if got := hitIDs(reader.search("zebracorn")); !sameIDs(got, sortedIDs(visible)) {
		t.Fatalf("reader hits %v, want only the visible doc", got)
	}
	// Not backfilled: the old JSON-path branch is scoped the same way.
	clearContentText(t, owner.env, visible, hidden)
	if got := hitIDs(reader.search("zebracorn")); !sameIDs(got, sortedIDs(visible)) {
		t.Fatalf("reader hits on unbackfilled rows %v, want only the visible doc", got)
	}
}

func TestSearch_UnbackfilledRowStillFound(t *testing.T) {
	t.Parallel()
	owner, _ := docFeedOwner(t, "search-unbackfilled")
	id := owner.createDoc("Old", nil, "an ancient quokka note")
	clearContentText(t, owner.env, id)

	hits := owner.search("quokka")
	if len(hits) != 1 || hits[0].ID != id || hits[0].MatchedIn != "content" || hits[0].Block != 1 {
		t.Fatalf("hits = %+v, want the old doc at block 1", hits)
	}
}

func TestSearch_LikeWildcardsAreLiteral(t *testing.T) {
	t.Parallel()
	owner, _ := docFeedOwner(t, "search-wildcards")
	pct := owner.createDoc("A", nil, "coverage is 100% now")
	owner.createDoc("B", nil, "coverage is 1000 now")
	under := owner.createDoc("C", nil, `path a_b\c`)
	owner.createDoc("D", nil, "path axb")

	if got := hitIDs(owner.search("100%")); !sameIDs(got, sortedIDs(pct)) {
		t.Fatalf("100%% hits %v, want only the doc with a literal %%", got)
	}
	if got := hitIDs(owner.search("a_b")); !sameIDs(got, sortedIDs(under)) {
		t.Fatalf("a_b hits %v, want only the doc with a literal _", got)
	}
	if got := hitIDs(owner.search(`b\c`)); !sameIDs(got, sortedIDs(under)) {
		t.Fatalf(`b\c hits %v, want the doc with a literal backslash`, got)
	}
}

// The plain-text backfill must not put documents in the change feed: it runs
// with paca.skip_doc_seq, so seq, updated_at and the project's counter stay
// where they were and a pull after it gets nothing.
func TestDocTextBackfill_KeepsChangeFeedQuiet(t *testing.T) {
	t.Parallel()
	owner, projID := docFeedOwner(t, "doc-text-backfill")
	a := owner.createDocWith("A", nil, boldPhraseContent)
	b := owner.createDoc("B", nil, "beta")
	gone := owner.createDoc("Gone", nil, "gamma")
	owner.must(http.StatusNoContent, http.MethodDelete, "/"+gone, nil)
	clearContentText(t, owner.env, a, b, gone)

	cursor := owner.pull("").Cursor
	if up := owner.pull(cursor); len(up.Docs) != 0 {
		t.Fatalf("pull before the backfill: %s", up.raw)
	}
	headBefore, _ := docSyncState(t, owner.env, projID)
	type row struct {
		Seq       int64     `db:"seq"`
		UpdatedAt time.Time `db:"updated_at"`
	}
	read := func(id string) row {
		var r row
		if err := owner.env.db.GetContext(owner.env.ctx, &r, `SELECT seq, updated_at FROM documents WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		return r
	}
	before := map[string]row{a: read(a), b: read(b), gone: read(gone)}

	bf := worker.NewDocHashBackfill(pgRepo.NewDocumentRepository(owner.env.db), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := bf.Run(owner.env.ctx); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{a: "Runbook\nNothing here.\nAlways run the database migration first.", b: "beta", gone: "gamma"}
	for id, text := range want {
		if got := storedContentText(t, owner.env, id); got == nil || *got != text {
			t.Errorf("doc %s: content_text %v, want %q", id, got, text)
		}
		if r := read(id); r.Seq != before[id].Seq || !r.UpdatedAt.Equal(before[id].UpdatedAt) {
			t.Errorf("doc %s: seq/updated_at moved from %+v to %+v", id, before[id], r)
		}
	}
	if head, _ := docSyncState(t, owner.env, projID); head != headBefore {
		t.Errorf("project seq moved from %d to %d", headBefore, head)
	}
	if up := owner.pull(cursor); up.Reset || len(up.Docs) != 0 {
		t.Fatalf("pull after the backfill: %s, want nothing", up.raw)
	}
}

// A by-path write goes through the same insert and update helpers, so it
// keeps content_text current too.
func TestUpsertByPath_StoresContentText(t *testing.T) {
	t.Parallel()
	f := newByPathFx(t, "bypath-text")
	st, e, _ := f.put(t, pushBody("Plans/x", "a", "first text", nil), nil)
	if st != http.StatusCreated {
		t.Fatalf("create: %d", st)
	}
	id, _ := assertDataMap(t, e)["id"].(string)
	if got := storedContentText(t, f.env, id); got == nil || *got != "first text" {
		t.Fatalf("after create content_text = %v", got)
	}
	if st, _, _ := f.put(t, pushBody("Plans/x", "a", "second text", nil), nil); st != http.StatusOK {
		t.Fatalf("update: %d", st)
	}
	if got := storedContentText(t, f.env, id); got == nil || *got != "second text" {
		t.Fatalf("after update content_text = %v", got)
	}
}

package e2e_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

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
// 000091, without numbering the change.
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

// The search must use the trigram indexes. Seeded and analysed table in this
// test's own database; the plan is read with EXPLAIN of the exact statement
// SearchDocuments runs.
func TestSearch_UsesTrigramIndexes(t *testing.T) {
	t.Parallel()
	owner, projID := docFeedOwner(t, "search-explain")
	env := owner.env
	// Enough filled rows, each with its own text, that an index beats reading
	// the project, plus a few not yet backfilled.
	if _, err := env.db.ExecContext(env.ctx, `INSERT INTO documents (id, project_id, title, content, content_text)
		SELECT gen_random_uuid(), $1, 'doc ' || g, '[]'::jsonb,
		       CASE WHEN g % 1000 = 0 THEN NULL ELSE 'line about topic ' || md5(g::text) || E'\nsecond line ' || g END
		FROM generate_series(1, 5000) AS g`, projID); err != nil {
		t.Fatal(err)
	}
	// VACUUM as well as ANALYZE: GIN cost estimates read the index metapage
	// and pending list, which only VACUUM updates, so an autovacuum between
	// this and the EXPLAIN cannot change the plan's inputs. Outside a
	// transaction, as VACUUM requires.
	if _, err := env.db.ExecContext(env.ctx, `VACUUM ANALYZE documents`); err != nil {
		t.Fatal(err)
	}
	q, args, err := pgRepo.NewDocumentRepository(env.db).SearchDocumentsQuery(env.ctx, uuid.MustParse(projID), "zebracorn", 20)
	if err != nil {
		t.Fatal(err)
	}
	// The repository normalizes the query itself: a line break can never
	// match content_text, which holds one block per line.
	_, brArgs, err := pgRepo.NewDocumentRepository(env.db).SearchDocumentsQuery(env.ctx, uuid.MustParse(projID), " the\r\nworker\n", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(brArgs) < 2 || brArgs[1] != "%the worker%" {
		t.Fatalf("pattern for a query with line breaks = %v, want %%the worker%%", brArgs)
	}
	explain := func(forced bool) string {
		t.Helper()
		tx, err := env.db.BeginTxx(env.ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if forced {
			if _, err := tx.ExecContext(env.ctx, `SET LOCAL enable_seqscan = off`); err != nil {
				t.Fatal(err)
			}
		}
		var lines []string
		if err := tx.SelectContext(env.ctx, &lines, `EXPLAIN `+q, args...); err != nil {
			t.Fatal(err)
		}
		return strings.Join(lines, "\n")
	}
	usesTrigram := func(plan string) bool {
		return strings.Contains(plan, "idx_documents_content_text_trgm") && strings.Contains(plan, "idx_documents_title_trgm")
	}
	// Every branch of the OR has an index path (stable whatever the costs).
	if plan := explain(true); !usesTrigram(plan) {
		t.Fatalf("with sequential scans off, the plan does not use the trigram indexes:\n%s", plan)
	}
	// And the planner picks them on its own for a project this size.
	if plan := explain(false); !usesTrigram(plan) || strings.Contains(plan, "Seq Scan on documents") {
		t.Fatalf("with default settings, the plan does not use the trigram indexes:\n%s", plan)
	}
}

// Content that is not BlockNote's top-level array (the API rejects it now,
// but older rows can hold it) was found by its "text" strings before
// content_text existed; it must still be found once filled.
func TestSearch_NonArrayContentFoundAfterBackfill(t *testing.T) {
	t.Parallel()
	owner, _ := docFeedOwner(t, "search-non-array")
	id := owner.createDoc("PM", nil, "placeholder")
	clearContentText(t, owner.env, id)
	if _, err := owner.env.db.ExecContext(owner.env.ctx,
		`UPDATE documents SET content = '{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"pm shape okapi"}]}]}'::jsonb WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	// Not backfilled: found through the old JSON-path branch.
	if hits := owner.search("shape okapi"); len(hits) != 1 || hits[0].ID != id {
		t.Fatalf("before backfill hits = %+v", hits)
	}
	bf := worker.NewDocHashBackfill(pgRepo.NewDocumentRepository(owner.env.db), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := bf.Run(owner.env.ctx); err != nil {
		t.Fatal(err)
	}
	if got := storedContentText(t, owner.env, id); got == nil || *got != "pm shape okapi" {
		t.Fatalf("content_text after backfill = %v", got)
	}
	hits := owner.search("shape okapi")
	if len(hits) != 1 || hits[0].ID != id || hits[0].MatchedIn != "content" || hits[0].Block != 0 || hits[0].Snippet != "pm shape okapi" {
		t.Fatalf("hits = %+v, want the doc with an excerpt and no block", hits)
	}
}

// A query is matched within one line of content_text: its line breaks become
// spaces, so the SQL and the excerpt agree.
func TestSearch_QueryLineBreakMatchesWithinOneBlock(t *testing.T) {
	t.Parallel()
	owner, _ := docFeedOwner(t, "search-newline")
	id := owner.createDoc("A", nil, "restart the worker now")
	owner.createDocWith("B", nil, `[{"type":"paragraph","content":[{"type":"text","text":"the"}]},{"type":"paragraph","content":[{"type":"text","text":"worker"}]}]`)

	hits := owner.search("the\nworker")
	if len(hits) != 1 || hits[0].ID != id || hits[0].Block != 1 || hits[0].Snippet != "restart the worker now" {
		t.Fatalf("hits = %+v, want only A at block 1", hits)
	}
}

// A writer that changes content without filling content_text (an API pod
// from before migration 000092 during a rolling upgrade, or any raw UPDATE)
// must not leave the old text in the index: the trigger turns content_text
// back to NULL, search falls back to the JSON-path match, and the backfill
// fills it again without putting the document in the change feed.
func TestSearch_OldWriterContentChangeClearsText(t *testing.T) {
	t.Parallel()
	owner, projID := docFeedOwner(t, "search-old-writer")
	id := owner.createDoc("Notes", nil, "alpha bravo")
	if got := storedContentText(t, owner.env, id); got == nil || *got != "alpha bravo" {
		t.Fatalf("after create content_text = %v", got)
	}

	if _, err := owner.env.db.ExecContext(owner.env.ctx,
		`UPDATE documents SET content = '[{"type":"paragraph","content":[{"type":"text","text":"charlie delta"}]}]'::jsonb WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if got := storedContentText(t, owner.env, id); got != nil {
		t.Fatalf("after a content change that did not fill it, content_text = %q, want NULL", *got)
	}
	if hits := owner.search("charlie delta"); len(hits) != 1 || hits[0].ID != id {
		t.Fatalf("search for the new text: %+v, want the doc", hits)
	}
	if hits := owner.search("alpha bravo"); len(hits) != 0 {
		t.Fatalf("search for the old text: %+v, want nothing", hits)
	}

	cursor := owner.pull("").Cursor
	headBefore, _ := docSyncState(t, owner.env, projID)
	type row struct {
		Seq       int64     `db:"seq"`
		UpdatedAt time.Time `db:"updated_at"`
	}
	read := func() row {
		var r row
		if err := owner.env.db.GetContext(owner.env.ctx, &r, `SELECT seq, updated_at FROM documents WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		return r
	}
	before := read()

	bf := worker.NewDocHashBackfill(pgRepo.NewDocumentRepository(owner.env.db), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := bf.Run(owner.env.ctx); err != nil {
		t.Fatal(err)
	}
	if got := storedContentText(t, owner.env, id); got == nil || *got != "charlie delta" {
		t.Fatalf("after the backfill content_text = %v, want %q", got, "charlie delta")
	}
	if r := read(); r.Seq != before.Seq || !r.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("seq/updated_at moved from %+v to %+v", before, r)
	}
	if head, _ := docSyncState(t, owner.env, projID); head != headBefore {
		t.Errorf("project seq moved from %d to %d", headBefore, head)
	}
	if up := owner.pull(cursor); up.Reset || len(up.Docs) != 0 {
		t.Fatalf("pull after the backfill: %s, want nothing", up.raw)
	}
	if hits := owner.search("charlie delta"); len(hits) != 1 || hits[0].ID != id || hits[0].Block != 1 {
		t.Fatalf("search after the backfill: %+v, want the doc at block 1", hits)
	}
}

// An UPDATE that lists content in SET without changing it (an old pod's
// rename may do this) keeps content_text: JSONB compares by value, so a
// re-serialized copy with other key order and spacing is the same content.
func TestSearch_OldWriterSameContentKeepsText(t *testing.T) {
	t.Parallel()
	owner, _ := docFeedOwner(t, "search-old-writer-same")
	id := owner.createDocWith("Notes", nil, `[{"type":"paragraph","content":[{"type":"text","text":"echo foxtrot","styles":{}}]}]`)

	if _, err := owner.env.db.ExecContext(owner.env.ctx,
		`UPDATE documents SET title = 'Renamed', content = content WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.env.db.ExecContext(owner.env.ctx,
		`UPDATE documents SET content = '[ { "content" : [ { "styles" : {}, "text" : "echo foxtrot", "type" : "text" } ], "type" : "paragraph" } ]'::jsonb WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if got := storedContentText(t, owner.env, id); got == nil || *got != "echo foxtrot" {
		t.Fatalf("content_text = %v, want %q", got, "echo foxtrot")
	}
}

// Writes through the API fill content_text themselves and declare it, so the
// trigger leaves it alone: a formatting-only change (same text, different
// content) and a text change both keep it filled and current, and each write
// is still numbered and in the change feed.
func TestDocUpdate_KeepsContentTextFilled(t *testing.T) {
	t.Parallel()
	owner, projID := docFeedOwner(t, "doc-update-text")
	plain := `[{"type":"paragraph","content":[{"type":"text","text":"golf hotel","styles":{}}]}]`
	bold := `[{"type":"paragraph","content":[{"type":"text","text":"golf ","styles":{}},{"type":"text","text":"hotel","styles":{"bold":true}}]}]`
	changed := `[{"type":"paragraph","content":[{"type":"text","text":"india juliet","styles":{}}]}]`
	id := owner.createDocWith("Notes", nil, plain)

	for _, step := range []struct{ content, want string }{{bold, "golf hotel"}, {changed, "india juliet"}} {
		cursor := owner.pull("").Cursor
		headBefore, _ := docSyncState(t, owner.env, projID)
		owner.must(http.StatusOK, http.MethodPatch, "/"+id, map[string]any{"content": json.RawMessage(step.content)})
		if got := storedContentText(t, owner.env, id); got == nil || *got != step.want {
			t.Fatalf("after writing %s content_text = %v, want %q", step.content, got, step.want)
		}
		if head, _ := docSyncState(t, owner.env, projID); head <= headBefore {
			t.Errorf("after writing %s project seq %d, want more than %d", step.content, head, headBefore)
		}
		if up := owner.pull(cursor); up.Reset || len(up.Docs) != 1 || up.Docs[0].ID != id {
			t.Errorf("pull after writing %s: %s, want the doc", step.content, up.raw)
		}
	}
	if hits := owner.search("golf hotel"); len(hits) != 0 {
		t.Fatalf("search for the replaced text: %+v, want nothing", hits)
	}
	if hits := owner.search("india juliet"); len(hits) != 1 || hits[0].ID != id {
		t.Fatalf("search for the current text: %+v, want the doc", hits)
	}
}

// Migration 000092 can be applied again over itself (each file runs in one
// transaction, as the migration runner does), and leaves one trigger.
func TestMigration000092_AppliesTwice(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	_, thisFile, _, _ := runtime.Caller(0)
	body, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations", "000092_clear_stale_content_text.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		tx, err := env.db.BeginTxx(env.ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(env.ctx, string(body)); err != nil {
			_ = tx.Rollback()
			t.Fatalf("apply %d: %v", i+1, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := env.db.GetContext(env.ctx, &n,
		`SELECT count(*) FROM pg_trigger WHERE tgrelid = 'documents'::regclass AND tgname = 'documents_clear_content_text'`); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("documents_clear_content_text triggers: %d, want 1", n)
	}
}

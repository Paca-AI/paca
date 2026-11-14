package e2e_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// The per-project document change sequence (Paca-AI/paca#747, PR 2).

// docSeq reads a document's seq and created_seq straight from the row.
func docSeq(t *testing.T, env *e2eEnv, docID string) (seq, createdSeq int64) {
	t.Helper()
	if err := env.db.QueryRowContext(env.ctx, `SELECT seq, created_seq FROM documents WHERE id = $1`, docID).Scan(&seq, &createdSeq); err != nil {
		t.Fatalf("read seq of %s: %v", docID, err)
	}
	return seq, createdSeq
}

// docSyncState reads the project's counters (zero when it has no row yet).
func docSyncState(t *testing.T, env *e2eEnv, projectID string) (seq, foldersSeq int64) {
	t.Helper()
	if err := env.db.QueryRowContext(env.ctx,
		`SELECT COALESCE(max(seq), 0), COALESCE(max(folders_seq), 0) FROM doc_sync_state WHERE project_id = $1`, projectID).Scan(&seq, &foldersSeq); err != nil {
		t.Fatalf("read doc_sync_state: %v", err)
	}
	return seq, foldersSeq
}

// docSeqSetup logs a project owner in and returns the project's docs URL.
func docSeqSetup(t *testing.T, username string) (env *e2eEnv, client *http.Client, token, projID, docsURL string) {
	t.Helper()
	env = newE2EEnv(t)
	seedTaskMemberUser(t, env, username, "docseqpass1")
	client, token = taskMemberLogin(t, env, username, "docseqpass1")
	projID = createProjectForTasksViaAPI(t, env, client, token)
	return env, client, token, projID, fmt.Sprintf("%s/api/v1/projects/%s/docs", env.base, projID)
}

func postDocViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, docsURL string, body map[string]any) string {
	t.Helper()
	status, e, _, raw := docRequest(t, env, client, token, http.MethodPost, docsURL, body, nil)
	if status != http.StatusCreated {
		t.Fatalf("create doc: %d %s", status, raw)
	}
	id, _ := assertDataMap(t, e)["id"].(string)
	return id
}

func createDocFolderViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, docsURL, name string, parentID *string) string {
	t.Helper()
	body := map[string]any{"name": name}
	if parentID != nil {
		body["parent_id"] = *parentID
	}
	status, e, _, raw := docRequest(t, env, client, token, http.MethodPost, docsURL+"/folders", body, nil)
	if status != http.StatusCreated {
		t.Fatalf("create folder: %d %s", status, raw)
	}
	id, _ := assertDataMap(t, e)["id"].(string)
	return id
}

func TestDocSeq_BumpsOnInsertUpdateSoftDelete(t *testing.T) {
	t.Parallel()
	env, client, token, projID, docs := docSeqSetup(t, "doc-seq-bump")

	a := postDocViaAPI(t, env, client, token, docs, map[string]any{"title": "A", "content": docBlocks("a", "one")})
	aSeq, aCreated := docSeq(t, env, a)
	if aSeq < 1 || aCreated != aSeq {
		t.Fatalf("after insert: seq %d, created_seq %d; want equal and positive", aSeq, aCreated)
	}
	b := postDocViaAPI(t, env, client, token, docs, map[string]any{"title": "B", "content": docBlocks("b", "two")})
	bSeq, _ := docSeq(t, env, b)
	if bSeq <= aSeq {
		t.Fatalf("second insert got seq %d, want more than %d", bSeq, aSeq)
	}

	if status, _, _, raw := docRequest(t, env, client, token, http.MethodPatch, docs+"/"+a, map[string]any{"content": docBlocks("a", "changed")}, nil); status != http.StatusOK {
		t.Fatalf("update: %d %s", status, raw)
	}
	updSeq, updCreated := docSeq(t, env, a)
	if updSeq <= bSeq || updCreated != aCreated {
		t.Fatalf("after update: seq %d (want > %d), created_seq %d (want %d)", updSeq, bSeq, updCreated, aCreated)
	}

	if status, _, _, raw := docRequest(t, env, client, token, http.MethodDelete, docs+"/"+a, nil, nil); status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", status, raw)
	}
	delSeq, _ := docSeq(t, env, a)
	if delSeq <= updSeq {
		t.Fatalf("after soft delete: seq %d, want more than %d", delSeq, updSeq)
	}
	if head, _ := docSyncState(t, env, projID); head != delSeq {
		t.Fatalf("doc_sync_state.seq = %d, want the last doc seq %d", head, delSeq)
	}

	// A tombstone keeps the seq of its deletion: a later write to the deleted
	// row (here the folder foreign key is not involved, so write it directly)
	// must not make the deletion look newer than it is.
	if _, err := env.db.ExecContext(env.ctx, `UPDATE documents SET position = position + 1 WHERE id = $1`, a); err != nil {
		t.Fatal(err)
	}
	if again, _ := docSeq(t, env, a); again != delSeq {
		t.Fatalf("a write to a deleted row moved its seq from %d to %d", delSeq, again)
	}
}

func TestDocSeq_FolderDeleteBumpsDocsAndFoldersSeq(t *testing.T) {
	t.Parallel()
	env, client, token, projID, docs := docSeqSetup(t, "doc-seq-folder")

	folder := createDocFolderViaAPI(t, env, client, token, docs, "Plans", nil)
	_, foldersAfterCreate := docSyncState(t, env, projID)
	if foldersAfterCreate < 1 {
		t.Fatalf("folders_seq after a folder insert = %d, want positive", foldersAfterCreate)
	}
	inside := postDocViaAPI(t, env, client, token, docs, map[string]any{"title": "In", "folder_id": folder, "content": docBlocks("a", "x")})
	outside := postDocViaAPI(t, env, client, token, docs, map[string]any{"title": "Out", "content": docBlocks("b", "y")})
	insideBefore, _ := docSeq(t, env, inside)
	outsideBefore, _ := docSeq(t, env, outside)
	headBefore, _ := docSyncState(t, env, projID)

	// A rename bumps folders_seq but no document.
	if status, _, _, raw := docRequest(t, env, client, token, http.MethodPatch, docs+"/folders/"+folder, map[string]any{"name": "Plans 2"}, nil); status != http.StatusOK {
		t.Fatalf("rename folder: %d %s", status, raw)
	}
	headRenamed, foldersRenamed := docSyncState(t, env, projID)
	if foldersRenamed <= headBefore || foldersRenamed != headRenamed {
		t.Fatalf("after rename: seq %d, folders_seq %d (head before %d)", headRenamed, foldersRenamed, headBefore)
	}
	if s, _ := docSeq(t, env, inside); s != insideBefore {
		t.Fatalf("a folder rename moved a document's seq from %d to %d", insideBefore, s)
	}

	if status, _, _, raw := docRequest(t, env, client, token, http.MethodDelete, docs+"/folders/"+folder, nil, nil); status != http.StatusNoContent {
		t.Fatalf("delete folder: %d %s", status, raw)
	}
	var folderID *string
	if err := env.db.GetContext(env.ctx, &folderID, `SELECT folder_id::text FROM documents WHERE id = $1`, inside); err != nil {
		t.Fatal(err)
	}
	if folderID != nil {
		t.Fatalf("the foreign key left folder_id = %s", *folderID)
	}
	insideAfter, _ := docSeq(t, env, inside)
	if insideAfter <= foldersRenamed {
		t.Fatalf("doc in the deleted folder: seq %d, want more than %d", insideAfter, foldersRenamed)
	}
	if s, _ := docSeq(t, env, outside); s != outsideBefore {
		t.Fatalf("doc outside the folder: seq moved from %d to %d", outsideBefore, s)
	}
	head, foldersSeq := docSyncState(t, env, projID)
	if foldersSeq <= foldersRenamed || head < insideAfter || head < foldersSeq {
		t.Fatalf("after folder delete: seq %d, folders_seq %d, doc seq %d", head, foldersSeq, insideAfter)
	}
}

func TestDocSeq_NoOpWriteDoesNotBump(t *testing.T) {
	t.Parallel()
	env, client, token, projID, docs := docSeqSetup(t, "doc-seq-noop")

	id := postDocViaAPI(t, env, client, token, docs, map[string]any{"title": "Plan", "content": docBlocks("first", "hello")})
	before, _ := docSeq(t, env, id)
	headBefore, _ := docSyncState(t, env, projID)

	// The same blocks under new ids: PR 1 skips the UPDATE, so nothing bumps.
	if status, _, _, raw := docRequest(t, env, client, token, http.MethodPatch, docs+"/"+id, map[string]any{"content": docBlocks("second", "hello")}, nil); status != http.StatusOK {
		t.Fatalf("no-op update: %d %s", status, raw)
	}
	after, _ := docSeq(t, env, id)
	headAfter, _ := docSyncState(t, env, projID)
	if after != before || headAfter != headBefore {
		t.Fatalf("a no-op write moved seq %d -> %d, project seq %d -> %d", before, after, headBefore, headAfter)
	}
}

// Rows that existed before the migration get numbered per project, oldest
// first, and running the migration again changes nothing.
func TestDocSeq_MigrationNumbersExistingRows(t *testing.T) {
	t.Parallel()
	env, client, token, projID, docs := docSeqSetup(t, "doc-seq-migrate")

	first := postDocViaAPI(t, env, client, token, docs, map[string]any{"title": "first", "content": docBlocks("a", "1")})
	second := postDocViaAPI(t, env, client, token, docs, map[string]any{"title": "second", "content": docBlocks("b", "2")})
	gone := postDocViaAPI(t, env, client, token, docs, map[string]any{"title": "gone", "content": docBlocks("c", "3")})

	// Put the project back to how a database looks before the migration:
	// unnumbered rows (one of them soft-deleted) and no counter.
	tx, err := env.db.BeginTxx(env.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`SELECT set_config('paca.skip_doc_seq', 'on', true)`,
		`UPDATE documents SET seq = 0, created_seq = 0, created_at = '2026-01-01'::timestamptz + (CASE title WHEN 'first' THEN 1 WHEN 'second' THEN 2 ELSE 3 END) * interval '1 minute'`,
		`UPDATE documents SET deleted_at = now() WHERE title = 'gone'`,
		`DELETE FROM doc_sync_state`,
	} {
		if _, err := tx.ExecContext(env.ctx, q); err != nil {
			_ = tx.Rollback()
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	_, thisFile, _, _ := runtime.Caller(0)
	migration, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations", "000088_add_doc_change_seq.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for run := 1; run <= 2; run++ {
		if _, err := env.db.ExecContext(env.ctx, string(migration)); err != nil {
			t.Fatalf("run %d of the migration: %v", run, err)
		}
		for i, id := range []string{first, second, gone} {
			seq, created := docSeq(t, env, id)
			if want := int64(i + 1); seq != want || created != want {
				t.Fatalf("run %d: doc %d has seq %d, created_seq %d; want both %d", run, i+1, seq, created, want)
			}
		}
		if head, _ := docSyncState(t, env, projID); head != 3 {
			t.Fatalf("run %d: doc_sync_state.seq = %d, want 3", run, head)
		}
	}

	// The trigger is back: the next write continues the sequence.
	if status, _, _, raw := docRequest(t, env, client, token, http.MethodPatch, docs+"/"+first, map[string]any{"title": "renamed"}, nil); status != http.StatusOK {
		t.Fatalf("update: %d %s", status, raw)
	}
	if seq, created := docSeq(t, env, first); seq != 4 || created != 1 {
		t.Fatalf("after the migration: seq %d, created_seq %d; want 4 and 1", seq, created)
	}
}

// Numbering a write takes the project's doc_sync_state row. Every writer must
// take it before any document or folder row, or a document save and a folder
// delete lock each other out (one holds the document and wants the counter,
// the other holds the counter and wants the document) and Postgres aborts one
// of them with a deadlock.
func TestDocSeq_ConcurrentWritesAndFolderDeleteDoNotDeadlock(t *testing.T) {
	t.Parallel()
	env, client, token, _, docs := docSeqSetup(t, "doc-seq-deadlock")

	const rounds, perFolder = 6, 12
	for round := 0; round < rounds; round++ {
		folder := createDocFolderViaAPI(t, env, client, token, docs, fmt.Sprintf("F%d", round), nil)
		ids := make([]string, perFolder)
		for i := range ids {
			ids[i] = postDocViaAPI(t, env, client, token, docs, map[string]any{"title": fmt.Sprintf("d%d", i), "folder_id": folder, "content": docBlocks("a", "x")})
		}

		type result struct {
			what   string
			status int
			body   string
		}
		results := make(chan result, 4*perFolder+2)
		var wg sync.WaitGroup
		run := func(what, method, url string, body any) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				status, _, _, raw := docRequest(t, env, client, token, method, url, body, nil)
				results <- result{what, status, string(raw)}
			}()
		}
		// Reverse id order for the saves, so they meet the cascade head-on.
		for i := perFolder - 1; i >= 0; i-- {
			run("save", http.MethodPatch, docs+"/"+ids[i], map[string]any{"content": docBlocks("a", fmt.Sprintf("round %d", round))})
			if i%3 == 0 {
				run("delete doc", http.MethodDelete, docs+"/"+ids[i], nil)
			}
			if i%2 == 0 {
				// A push by path takes the counter, then the folder lock,
				// then the document row. If it loses to the folder delete or
				// rename it creates the folder and the document again.
				run("push by path", http.MethodPut, docs+"/by-path", map[string]any{"path": fmt.Sprintf("F%d/d%d", round, i), "content": docBlocks("a", fmt.Sprintf("pushed %d", round))})
			}
			if i%4 == 0 {
				// At the top level: creating a doc in a folder that is being
				// deleted fails on the foreign key, a separate, older race.
				run("create", http.MethodPost, docs, map[string]any{"title": "new", "content": docBlocks("a", "y")})
			}
		}
		run("rename folder", http.MethodPatch, docs+"/folders/"+folder, map[string]any{"name": fmt.Sprintf("G%d", round)})
		run("delete folder", http.MethodDelete, docs+"/folders/"+folder, nil)
		wg.Wait()
		close(results)
		for r := range results {
			// Losing a race is fine (the doc or folder is already gone); a
			// server error is not.
			if r.status >= 500 {
				t.Fatalf("round %d, %s: status %d: %s", round, r.what, r.status, r.body)
			}
		}
	}
}

package e2e_test

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
	"github.com/Paca-AI/api/internal/worker"
)

// The content hash backfill against the real schema (Paca-AI/paca#747, PR 2):
// it must fill every NULL hash, deleted documents included, without moving
// seq or updated_at, so it never shows up in the change feed.
func TestDocHashBackfill_DoesNotBumpSeqOrUpdatedAt(t *testing.T) {
	t.Parallel()
	env, client, token, projID, docs := docSeqSetup(t, "doc-hash-backfill")

	live := postDocViaAPI(t, env, client, token, docs, map[string]any{"title": "live", "content": docBlocks("a", "alpha")})
	deleted := postDocViaAPI(t, env, client, token, docs, map[string]any{"title": "deleted", "content": docBlocks("b", "beta")})
	empty := postDocViaAPI(t, env, client, token, docs, map[string]any{"title": "empty"})
	kept := postDocViaAPI(t, env, client, token, docs, map[string]any{"title": "kept", "content": docBlocks("c", "gamma")})
	if status, _, _, raw := docRequest(t, env, client, token, http.MethodDelete, docs+"/"+deleted, nil, nil); status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", status, raw)
	}

	// Make three of them look like rows from before migration 000087. The
	// reset itself must not be numbered either, hence the setting.
	tx, err := env.db.BeginTxx(env.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(env.ctx, `SELECT set_config('paca.skip_doc_seq', 'on', true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(env.ctx, `UPDATE documents SET content_hash = NULL, content = CASE WHEN id = $4 THEN NULL ELSE content END WHERE id IN ($1, $2, $4) AND id <> $3`, live, deleted, kept, empty); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	type row struct {
		ID        string    `db:"id"`
		Hash      *string   `db:"content_hash"`
		Seq       int64     `db:"seq"`
		UpdatedAt time.Time `db:"updated_at"`
	}
	read := func() map[string]row {
		var rows []row
		if err := env.db.SelectContext(env.ctx, &rows, `SELECT id::text AS id, content_hash, seq, updated_at FROM documents WHERE project_id = $1`, projID); err != nil {
			t.Fatal(err)
		}
		out := map[string]row{}
		for _, r := range rows {
			out[r.ID] = r
		}
		return out
	}
	before := read()
	headBefore, _ := docSyncState(t, env, projID)
	keptHash := *before[kept].Hash

	b := worker.NewDocHashBackfill(pgRepo.NewDocumentRepository(env.db), slog.New(slog.NewTextHandler(io.Discard, nil)))
	filled, err := b.Run(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if filled != 3 {
		t.Fatalf("backfilled %d rows, want 3", filled)
	}

	after := read()
	want := map[string]string{
		live:    docdom.ContentHash(docBlocks("a", "alpha")),
		deleted: docdom.ContentHash(docBlocks("b", "beta")),
		empty:   docdom.ContentHash(nil),
		kept:    keptHash,
	}
	for id, hash := range want {
		got := after[id]
		if got.Hash == nil || *got.Hash != hash {
			t.Errorf("doc %s: hash %v, want %s", id, got.Hash, hash)
		}
		if got.Seq != before[id].Seq {
			t.Errorf("doc %s: seq moved from %d to %d", id, before[id].Seq, got.Seq)
		}
		if !got.UpdatedAt.Equal(before[id].UpdatedAt) {
			t.Errorf("doc %s: updated_at moved from %s to %s", id, before[id].UpdatedAt, got.UpdatedAt)
		}
	}
	if head, _ := docSyncState(t, env, projID); head != headBefore {
		t.Errorf("project seq moved from %d to %d", headBefore, head)
	}

	// The setting must not outlive the backfill's transactions: an ordinary
	// write on the same pool is numbered again.
	if status, _, _, raw := docRequest(t, env, client, token, http.MethodPatch, docs+"/"+live, map[string]any{"title": "live 2"}, nil); status != http.StatusOK {
		t.Fatalf("update: %d %s", status, raw)
	}
	if seq, _ := docSeq(t, env, live); seq <= headBefore {
		t.Fatalf("a write after the backfill got seq %d, want more than %d", seq, headBefore)
	}
	if filled, err := b.Run(env.ctx); err != nil || filled != 0 {
		t.Fatalf("second run: filled %d, err %v; want 0, nil", filled, err)
	}
}

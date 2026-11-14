package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// Plain-text column and trigram search (Paca-AI/paca#747, PR 7).

func contentTextOf(t *testing.T, f *scopeFx, id uuid.UUID) *string {
	t.Helper()
	var text *string
	if err := f.db.GetContext(context.Background(), &text, `SELECT content_text FROM documents WHERE id = $1`, id.String()); err != nil {
		t.Fatal(err)
	}
	return text
}

func TestDocumentWrites_StoreContentText(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewDocumentRepository(f.db)

	content := json.RawMessage(`[{"type":"paragraph","content":[{"type":"text","text":"foo "},{"type":"text","text":"bar","styles":{"bold":true}}]}]`)
	now := time.Now()
	d := &docdom.Document{ID: uuid.New(), ProjectID: f.p, Title: "t", Content: content, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateDocument(ctx, d); err != nil {
		t.Fatal(err)
	}
	if got := contentTextOf(t, f, d.ID); got == nil || *got != "foo bar" {
		t.Fatalf("after insert content_text = %v, want %q", got, "foo bar")
	}

	if _, err := repo.UpdateDocumentAtomic(ctx, d.ID, func(cur *docdom.Document) error {
		cur.Content = json.RawMessage(`[{"type":"paragraph","content":[{"type":"text","text":"one"}]},{"type":"paragraph","content":[{"type":"text","text":"two"}]}]`)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := contentTextOf(t, f, d.ID); got == nil || *got != "one\ntwo" {
		t.Fatalf("after update content_text = %v, want %q", got, "one\ntwo")
	}

	// An empty document stores '' (filled), never NULL (waiting for the backfill).
	empty := &docdom.Document{ID: uuid.New(), ProjectID: f.p, Title: "empty", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateDocument(ctx, empty); err != nil {
		t.Fatal(err)
	}
	if got := contentTextOf(t, f, empty.ID); got == nil || *got != "" {
		t.Fatalf("empty doc content_text = %v, want empty string", got)
	}
}

func TestBackfillContentText_FillsWithoutMovingSeq(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewDocumentRepository(f.db)

	live, deleted, empty := uuid.New(), uuid.New(), uuid.New()
	pgExec(t, f.db, `INSERT INTO documents (id, project_id, title, content) VALUES
		($1,$4,'live','[{"type":"paragraph","content":[{"type":"text","text":"a "},{"type":"text","text":"b","styles":{"italic":true}}]}]'::jsonb),
		($2,$4,'deleted','[{"type":"paragraph","content":[{"type":"text","text":"gone"}]}]'::jsonb),
		($3,$4,'empty',NULL)`, live, deleted, empty, f.p)
	pgExec(t, f.db, `UPDATE documents SET deleted_at = now() WHERE id = $1`, deleted)
	// Raw inserts bypass the repository, so these rows look like rows written
	// before migration 000090.
	for _, id := range []uuid.UUID{live, deleted, empty} {
		if got := contentTextOf(t, f, id); got != nil {
			t.Fatalf("seeded row %s already has content_text %q", id, *got)
		}
	}

	type row struct {
		ID        string    `db:"id"`
		Seq       int64     `db:"seq"`
		UpdatedAt time.Time `db:"updated_at"`
	}
	read := func() map[string]row {
		var rows []row
		if err := f.db.SelectContext(ctx, &rows, `SELECT id::text AS id, seq, updated_at FROM documents WHERE project_id = $1`, f.p); err != nil {
			t.Fatal(err)
		}
		out := map[string]row{}
		for _, r := range rows {
			out[r.ID] = r
		}
		return out
	}
	head := func() int64 {
		var seq int64
		if err := f.db.GetContext(ctx, &seq, `SELECT seq FROM doc_sync_state WHERE project_id = $1`, f.p); err != nil {
			t.Fatal(err)
		}
		return seq
	}
	before, headBefore := read(), head()

	n, err := repo.BackfillContentText(ctx, 2, docdom.ContentText)
	if err != nil || n != 2 {
		t.Fatalf("first batch: %d, %v; want 2 (the limit)", n, err)
	}
	n, err = repo.BackfillContentText(ctx, 2, docdom.ContentText)
	if err != nil || n != 1 {
		t.Fatalf("second batch: %d, %v; want 1", n, err)
	}
	if n, err := repo.BackfillContentText(ctx, 2, docdom.ContentText); err != nil || n != 0 {
		t.Fatalf("third batch: %d, %v; want 0", n, err)
	}

	want := map[uuid.UUID]string{live: "a b", deleted: "gone", empty: ""}
	for id, text := range want {
		if got := contentTextOf(t, f, id); got == nil || *got != text {
			t.Errorf("doc %s: content_text %v, want %q", id, got, text)
		}
	}
	after := read()
	for id, r := range before {
		if after[id].Seq != r.Seq || !after[id].UpdatedAt.Equal(r.UpdatedAt) {
			t.Errorf("doc %s: seq/updated_at moved from %d/%s to %d/%s", id, r.Seq, r.UpdatedAt, after[id].Seq, after[id].UpdatedAt)
		}
	}
	if h := head(); h != headBefore {
		t.Errorf("project seq moved from %d to %d", headBefore, h)
	}
}

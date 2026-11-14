package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// changesFx adds document helpers over scopeFx for the change feed queries.
type changesFx struct {
	*scopeFx
	repo *DocumentRepository
}

func newChangesFx(t *testing.T) *changesFx {
	f := newScopeFx(t)
	return &changesFx{scopeFx: f, repo: NewDocumentRepository(f.db)}
}

func (f *changesFx) folder(name string) uuid.UUID {
	id := uuid.New()
	pgExec(f.t, f.db, `INSERT INTO doc_folders (id, project_id, name) VALUES ($1,$2,$3)`, id, f.p, name)
	return id
}

func (f *changesFx) doc(title string, folder *uuid.UUID) uuid.UUID {
	id := uuid.New()
	pgExec(f.t, f.db, `INSERT INTO documents (id, project_id, folder_id, title, content, content_hash) VALUES ($1,$2,$3,$4,'[]'::jsonb,'h')`, id, f.p, folder, title)
	return id
}

func (f *changesFx) head() int64 {
	var seq int64
	if err := f.db.GetContext(context.Background(), &seq, `SELECT seq FROM doc_sync_state WHERE project_id = $1`, f.p); err != nil {
		f.t.Fatal(err)
	}
	return seq
}

// read runs fn in one feed snapshot.
func (f *changesFx) read(ctx context.Context, fn func(r docdom.ChangeReader)) {
	f.t.Helper()
	if err := f.repo.ReadDocChanges(ctx, func(r docdom.ChangeReader) error { fn(r); return nil }); err != nil {
		f.t.Fatal(err)
	}
}

func (f *changesFx) fingerprint(ctx context.Context, upTo *int64) string {
	f.t.Helper()
	var fp string
	f.read(ctx, func(r docdom.ChangeReader) {
		var err error
		if fp, err = r.VisibleFingerprint(ctx, f.p, upTo); err != nil {
			f.t.Fatal(err)
		}
	})
	return fp
}

func docIDs(docs []*docdom.Document) []string {
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.ID.String())
	}
	return sorted(out)
}

// The fingerprint taken with upToSeq describes the documents alive at that
// sequence, judged by what the caller can see now.
func TestVisibleFingerprint_TracksVisibilityNotEdits(t *testing.T) {
	f := newChangesFx(t)
	secret := f.folder("secret")
	a, b := f.doc("a", nil), f.doc("b", nil)
	f.doc("hidden", &secret)
	f.role(`{"version":"1","statements":[
		{"effect":"Allow","actions":["docs:read"],"resources":["project/*/doc/*"]},
		{"effect":"Deny","actions":["docs:read"],"resources":["project/*/doc/*"],"conditions":{"In":{"doc.ancestor_folder_ids":["` + secret.String() + `"]}}}]}`)
	ctx := f.ctx("docs:read", "doc")

	at := f.head()
	then := f.fingerprint(ctx, nil)
	if got := f.fingerprint(ctx, &at); got != then {
		t.Fatalf("fingerprint up to the head = %s, want the live one %s", got, then)
	}
	if unscoped := f.fingerprint(context.Background(), nil); unscoped == then {
		t.Fatal("the scoped and unscoped fingerprints are equal: the hidden doc is not filtered")
	}

	// Edits, new documents and deletions after the cursor do not change it.
	pgExec(t, f.db, `UPDATE documents SET title = 'a2' WHERE id = $1`, a)
	f.doc("new", nil)
	pgExec(t, f.db, `UPDATE documents SET deleted_at = now() WHERE id = $1`, b)
	if got := f.fingerprint(ctx, &at); got != then {
		t.Fatal("an edit, an insert and a delete after the cursor changed the fingerprint")
	}
	if now := f.fingerprint(ctx, nil); now == then {
		t.Fatal("the live fingerprint ignores an insert and a delete")
	}
	// A later write to the deleted row (the folder foreign key does this)
	// leaves it "deleted after the cursor", not "changed again".
	pgExec(t, f.db, `UPDATE documents SET position = 7 WHERE id = $1`, b)
	if got := f.fingerprint(ctx, &at); got != then {
		t.Fatal("a write to a tombstone changed the fingerprint")
	}

	// A move out of sight does.
	pgExec(t, f.db, `UPDATE documents SET folder_id = $2 WHERE id = $1`, a, secret)
	if got := f.fingerprint(ctx, &at); got == then {
		t.Fatal("a doc moved into a denied folder left the fingerprint unchanged")
	}
	pgExec(t, f.db, `UPDATE documents SET folder_id = NULL WHERE id = $1`, a)
	if got := f.fingerprint(ctx, &at); got != then {
		t.Fatal("moving the doc back did not restore the fingerprint")
	}

	// A caller whose scope allows nothing gets the fingerprint of no documents.
	other := newChangesFx(t)
	other.doc("x", nil)
	other.role(policyJSON("Allow", "tasks:read", "project/*/task/*", ""))
	if got := other.fingerprint(other.ctx("docs:read", "doc"), nil); got != emptyFingerprint {
		t.Fatalf("deny-all fingerprint = %s, want %s", got, emptyFingerprint)
	}
}

func TestListDocChanges_ScopedTombstonesAndOrder(t *testing.T) {
	f := newChangesFx(t)
	secret := f.folder("secret")
	kept, edited, deleted := f.doc("kept", nil), f.doc("edited", nil), f.doc("deleted", nil)
	hidden := f.doc("hidden", &secret)
	f.role(`{"version":"1","statements":[
		{"effect":"Allow","actions":["docs:read"],"resources":["project/*/doc/*"]},
		{"effect":"Deny","actions":["docs:read"],"resources":["project/*/doc/*"],"conditions":{"In":{"doc.ancestor_folder_ids":["` + secret.String() + `"]}}}]}`)
	ctx := f.ctx("docs:read", "doc")
	since := f.head()

	pgExec(t, f.db, `UPDATE documents SET deleted_at = now() WHERE id IN ($1, $2)`, deleted, hidden)
	pgExec(t, f.db, `UPDATE documents SET title = 'edited 2', content_hash = NULL, content = '[{"type":"paragraph"}]'::jsonb WHERE id = $1`, edited)
	shortLived := f.doc("short-lived", nil)
	pgExec(t, f.db, `UPDATE documents SET deleted_at = now() WHERE id = $1`, shortLived)
	created := f.doc("created", nil)

	f.read(ctx, func(r docdom.ChangeReader) {
		docs, err := r.ListDocChanges(ctx, f.p, since)
		if err != nil {
			t.Fatal(err)
		}
		// Oldest change first; no hidden tombstone, no short-lived one.
		if len(docs) != 3 || docs[0].ID != deleted || docs[1].ID != edited || docs[2].ID != created {
			t.Fatalf("changes = %v, want deleted, edited, created", docIDs(docs))
		}
		if docs[0].DeletedAt == nil {
			t.Fatal("the deleted doc is not marked deleted")
		}
		// No bodies; a NULL stored hash is computed from the body, not from nothing.
		want := docdom.ContentHash([]byte(`[{"type":"paragraph"}]`))
		if docs[1].Content != nil || docs[1].ContentHash == nil || *docs[1].ContentHash != want {
			t.Fatalf("edited doc: content %s hash %v, want no content and hash %s", docs[1].Content, docs[1].ContentHash, want)
		}
		all, err := r.ListVisibleDocs(ctx, f.p)
		if err != nil {
			t.Fatal(err)
		}
		if got := docIDs(all); len(got) != 3 || !equalStrings(got, sorted([]string{kept.String(), edited.String(), created.String()})) {
			t.Fatalf("visible docs = %v, want the three live visible ones", got)
		}
		seq, foldersSeq, err := r.DocSyncState(ctx, f.p)
		if err != nil || seq != f.head() || foldersSeq < 1 || foldersSeq > seq {
			t.Fatalf("state = %d, %d (%v)", seq, foldersSeq, err)
		}
		if s, fs, err := r.DocSyncState(ctx, uuid.New()); err != nil || s != 0 || fs != 0 {
			t.Fatalf("state of a project without docs = %d, %d (%v), want zeros", s, fs, err)
		}
	})
}

func TestFindDocumentsByIDs_ScopedToProjectAndCaller(t *testing.T) {
	f := newChangesFx(t)
	secret := f.folder("secret")
	a := f.doc("a", nil)
	hidden := f.doc("hidden", &secret)
	deleted := f.doc("deleted", nil)
	pgExec(t, f.db, `UPDATE documents SET deleted_at = now() WHERE id = $1`, deleted)
	otherProject := f.fx.project()
	foreign := uuid.New()
	pgExec(t, f.db, `INSERT INTO documents (id, project_id, title) VALUES ($1,$2,'foreign')`, foreign, otherProject)
	f.role(policyJSON("Allow", "docs:read", "project/*/doc/*", `{"NotIn":{"doc.ancestor_folder_ids":["`+secret.String()+`"]}}`))

	ids := []uuid.UUID{a, hidden, deleted, foreign, uuid.New()}
	got, err := f.repo.FindDocumentsByIDs(f.ctx("docs:read", "doc"), f.p, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != a || got[0].Content == nil {
		t.Fatalf("scoped: %v, want only doc a with its body", docIDs(got))
	}
	// Unscoped (an internal caller): still bound to the project and to live rows.
	got, err = f.repo.FindDocumentsByIDs(context.Background(), f.p, ids)
	if err != nil || !equalStrings(docIDs(got), sorted([]string{a.String(), hidden.String()})) {
		t.Fatalf("unscoped: %v (%v), want a and hidden", docIDs(got), err)
	}
	if got, err := f.repo.FindDocumentsByIDs(context.Background(), f.p, nil); err != nil || len(got) != 0 {
		t.Fatalf("no ids: %v (%v)", got, err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

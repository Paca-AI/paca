package docsvc

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// fakeChanges is a ChangeRepository over a fixed state. Fingerprints are
// looked up by sequence so a test can make "the same documents" hash
// differently, as a visibility change does.
type fakeChanges struct {
	project     uuid.UUID
	seq         int64
	foldersSeq  int64
	liveFP      string           // VisibleFingerprint(nil)
	fpAt        map[int64]string // VisibleFingerprint(&seq)
	changes     []*docdom.Document
	visible     []*docdom.Document
	folders     []*docdom.DocFolder
	byID        map[uuid.UUID]*docdom.Document
	calls       []string
	changesFrom int64
	failOn      string
}

func (f *fakeChanges) note(name string) error {
	f.calls = append(f.calls, name)
	if f.failOn == name {
		return errors.New(name + " failed")
	}
	return nil
}

func (f *fakeChanges) ReadDocChanges(_ context.Context, fn func(docdom.ChangeReader) error) error {
	return fn(f)
}

func (f *fakeChanges) DocSyncState(context.Context, uuid.UUID) (int64, int64, error) {
	return f.seq, f.foldersSeq, f.note("state")
}

func (f *fakeChanges) VisibleFingerprint(_ context.Context, _ uuid.UUID, upTo *int64) (string, error) {
	if upTo == nil {
		return f.liveFP, f.note("fingerprint-live")
	}
	return f.fpAt[*upTo], f.note("fingerprint-at")
}

func (f *fakeChanges) ListDocChanges(_ context.Context, _ uuid.UUID, since int64) ([]*docdom.Document, error) {
	f.changesFrom = since
	return f.changes, f.note("changes")
}

func (f *fakeChanges) ListVisibleDocs(context.Context, uuid.UUID) ([]*docdom.Document, error) {
	return f.visible, f.note("visible")
}

func (f *fakeChanges) ListAllFolders(context.Context, uuid.UUID) ([]*docdom.DocFolder, error) {
	return f.folders, f.note("folders")
}

func (f *fakeChanges) FindDocumentsByIDs(_ context.Context, projectID uuid.UUID, ids []uuid.UUID) ([]*docdom.Document, error) {
	if err := f.note("find"); err != nil {
		return nil, err
	}
	// Reverse order on purpose: the service must restore the asked order.
	var out []*docdom.Document
	for i := len(ids) - 1; i >= 0; i-- {
		if d, ok := f.byID[ids[i]]; ok && d.ProjectID == projectID {
			out = append(out, d)
		}
	}
	return out, nil
}

func newFakeChanges() *fakeChanges {
	p := uuid.New()
	hash := "h"
	live := &docdom.Document{ID: uuid.New(), ProjectID: p, Title: "live", ContentHash: &hash}
	return &fakeChanges{
		project: p, seq: 10, foldersSeq: 4, liveFP: "fp-now",
		fpAt:    map[int64]string{},
		visible: []*docdom.Document{live},
		folders: []*docdom.DocFolder{{ID: uuid.New(), ProjectID: p, Name: "Plans"}},
	}
}

func cursorOf(t *testing.T, set *docdom.ChangeSet) docdom.ChangeCursor {
	t.Helper()
	c, ok := docdom.DecodeChangeCursor(set.Cursor)
	if !ok {
		t.Fatalf("the reply's cursor %q does not decode", set.Cursor)
	}
	return c
}

func assertReset(t *testing.T, f *fakeChanges, set *docdom.ChangeSet) {
	t.Helper()
	if !set.Reset {
		t.Fatal("reset = false, want true")
	}
	if !reflect.DeepEqual(set.Docs, f.visible) {
		t.Fatalf("docs = %v, want the full visible list", set.Docs)
	}
	if !set.FoldersIncluded || !reflect.DeepEqual(set.Folders, f.folders) {
		t.Fatalf("folders = %v (included %v), want the full tree", set.Folders, set.FoldersIncluded)
	}
	if got := cursorOf(t, set); got != (docdom.ChangeCursor{ProjectID: f.project, Seq: f.seq, Fingerprint: f.liveFP}) {
		t.Fatalf("cursor = %+v, want this project at seq %d with the live fingerprint", got, f.seq)
	}
	for _, c := range f.calls {
		if c == "changes" {
			t.Fatal("a reset read the change list")
		}
	}
}

func TestChanges_NoCursorResets(t *testing.T) {
	f := newFakeChanges()
	set, err := NewChangesService(f).Changes(context.Background(), f.project, "")
	if err != nil {
		t.Fatal(err)
	}
	assertReset(t, f, set)
}

func TestChanges_BadCursorResets(t *testing.T) {
	other := uuid.New()
	valid := docdom.EncodeChangeCursor(docdom.ChangeCursor{ProjectID: uuid.New(), Seq: 3, Fingerprint: "x"})
	cases := map[string]func(f *fakeChanges) string{
		"garbage":   func(*fakeChanges) string { return "%%%not-base64%%%" },
		"truncated": func(*fakeChanges) string { return valid[:len(valid)-9] },
		"another project's cursor": func(f *fakeChanges) string {
			// Even with a sequence and fingerprint that would match here.
			return docdom.EncodeChangeCursor(docdom.ChangeCursor{ProjectID: other, Seq: f.seq, Fingerprint: f.liveFP})
		},
		"a sequence the project has not reached": func(f *fakeChanges) string {
			f.fpAt[f.seq+5] = "fp-future"
			return docdom.EncodeChangeCursor(docdom.ChangeCursor{ProjectID: f.project, Seq: f.seq + 5, Fingerprint: "fp-future"})
		},
		"a fingerprint that no longer matches": func(f *fakeChanges) string {
			f.fpAt[6] = "fp-after-access-change"
			return docdom.EncodeChangeCursor(docdom.ChangeCursor{ProjectID: f.project, Seq: 6, Fingerprint: "fp-at-6"})
		},
		"same sequence, different fingerprint": func(f *fakeChanges) string {
			return docdom.EncodeChangeCursor(docdom.ChangeCursor{ProjectID: f.project, Seq: f.seq, Fingerprint: "fp-before-role-edit"})
		},
	}
	for name, cursor := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeChanges()
			set, err := NewChangesService(f).Changes(context.Background(), f.project, cursor(f))
			if err != nil {
				t.Fatalf("got error %v, want a reset", err)
			}
			assertReset(t, f, set)
		})
	}
}

func TestChanges_UpToDateReturnsNothing(t *testing.T) {
	f := newFakeChanges()
	since := docdom.EncodeChangeCursor(docdom.ChangeCursor{ProjectID: f.project, Seq: f.seq, Fingerprint: f.liveFP})
	set, err := NewChangesService(f).Changes(context.Background(), f.project, since)
	if err != nil {
		t.Fatal(err)
	}
	if set.Reset || len(set.Docs) != 0 || set.FoldersIncluded || set.Folders != nil {
		t.Fatalf("got %+v, want no reset, no docs and no folders", set)
	}
	if set.Cursor != since {
		t.Fatalf("cursor changed from %q to %q with nothing new", since, set.Cursor)
	}
	// The up-to-date path is two reads: the sequence and one fingerprint.
	if want := []string{"state", "fingerprint-live"}; !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("reads = %v, want %v", f.calls, want)
	}
}

func TestChanges_ReturnsChangesSinceTheCursor(t *testing.T) {
	f := newFakeChanges()
	deletedAt := time.Now()
	f.changes = []*docdom.Document{
		{ID: uuid.New(), ProjectID: f.project, Title: "edited"},
		{ID: uuid.New(), ProjectID: f.project, DeletedAt: &deletedAt},
	}
	f.fpAt[6] = "fp-at-6" // unchanged visibility for the documents alive at 6
	f.foldersSeq = 6      // no folder change after the cursor
	since := docdom.EncodeChangeCursor(docdom.ChangeCursor{ProjectID: f.project, Seq: 6, Fingerprint: "fp-at-6"})

	set, err := NewChangesService(f).Changes(context.Background(), f.project, since)
	if err != nil {
		t.Fatal(err)
	}
	if set.Reset || !reflect.DeepEqual(set.Docs, f.changes) || f.changesFrom != 6 {
		t.Fatalf("reset %v, docs %v, read from %d; want the changes after 6", set.Reset, set.Docs, f.changesFrom)
	}
	if set.FoldersIncluded || set.Folders != nil {
		t.Fatalf("folders sent (%v) though none changed after the cursor", set.Folders)
	}
	if got := cursorOf(t, set); got.Seq != f.seq || got.Fingerprint != f.liveFP {
		t.Fatalf("new cursor = %+v, want seq %d and the live fingerprint", got, f.seq)
	}
	// The sequence is read before anything else.
	if f.calls[0] != "state" {
		t.Fatalf("first read was %q, want the sequence", f.calls[0])
	}
}

func TestChanges_FolderChangeSendsTree(t *testing.T) {
	f := newFakeChanges()
	f.fpAt[6] = "fp-at-6"
	f.foldersSeq = 9
	since := docdom.EncodeChangeCursor(docdom.ChangeCursor{ProjectID: f.project, Seq: 6, Fingerprint: "fp-at-6"})

	set, err := NewChangesService(f).Changes(context.Background(), f.project, since)
	if err != nil {
		t.Fatal(err)
	}
	if set.Reset || len(set.Docs) != 0 {
		t.Fatalf("reset %v, docs %v; want neither", set.Reset, set.Docs)
	}
	if !set.FoldersIncluded || !reflect.DeepEqual(set.Folders, f.folders) {
		t.Fatalf("folders = %v (included %v), want the full tree", set.Folders, set.FoldersIncluded)
	}
}

func TestChanges_RepositoryErrorIsReturned(t *testing.T) {
	for _, step := range []string{"state", "fingerprint-live", "fingerprint-at", "changes", "visible", "folders"} {
		f := newFakeChanges()
		f.failOn = step
		f.fpAt[6] = "fp-at-6"
		f.foldersSeq = 9
		cursor := docdom.EncodeChangeCursor(docdom.ChangeCursor{ProjectID: f.project, Seq: 6, Fingerprint: "fp-at-6"})
		if step == "visible" {
			cursor = ""
		}
		if set, err := NewChangesService(f).Changes(context.Background(), f.project, cursor); err == nil {
			t.Errorf("%s failed but Changes returned %+v", step, set)
		}
	}
}

func TestBatchGet_OrderDedupeAndCap(t *testing.T) {
	f := newFakeChanges()
	a := &docdom.Document{ID: uuid.New(), ProjectID: f.project, Title: "a"}
	b := &docdom.Document{ID: uuid.New(), ProjectID: f.project, Title: "b"}
	foreign := &docdom.Document{ID: uuid.New(), ProjectID: uuid.New(), Title: "foreign"}
	f.byID = map[uuid.UUID]*docdom.Document{a.ID: a, b.ID: b, foreign.ID: foreign}
	svc := NewChangesService(f)

	got, err := svc.BatchGet(context.Background(), f.project, []uuid.UUID{b.ID, uuid.New(), a.ID, b.ID, foreign.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != b || got[1] != a {
		t.Fatalf("got %v, want b then a, once each", got)
	}

	if got, err := svc.BatchGet(context.Background(), f.project, nil); err != nil || len(got) != 0 {
		t.Fatalf("no ids: got %v, %v; want an empty result", got, err)
	}

	ids := make([]uuid.UUID, docdom.MaxBatchGetIDs+1)
	for i := range ids {
		ids[i] = uuid.New()
	}
	f.calls = nil
	if _, err := svc.BatchGet(context.Background(), f.project, ids); !errors.Is(err, docdom.ErrTooManyDocIDs) {
		t.Fatalf("101 ids: err = %v, want ErrTooManyDocIDs", err)
	}
	if len(f.calls) != 0 {
		t.Fatal("the repository was queried for an oversized batch")
	}
	if _, err := svc.BatchGet(context.Background(), f.project, ids[:docdom.MaxBatchGetIDs]); err != nil {
		t.Fatalf("100 ids: %v", err)
	}
}

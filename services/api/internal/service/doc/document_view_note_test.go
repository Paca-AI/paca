package docsvc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	docsvc "github.com/Paca-AI/api/internal/service/doc"
)

// fakeViews is the view lookup a note service checks new notes against.
type fakeViews map[uuid.UUID]*sprintdom.SprintView

func (f fakeViews) FindViewByID(_ context.Context, id uuid.UUID) (*sprintdom.SprintView, error) {
	v, ok := f[id]
	if !ok {
		return nil, sprintdom.ErrViewNotFound
	}
	return v, nil
}

func (f fakeViews) add(projectID uuid.UUID, vt sprintdom.ViewType) uuid.UUID {
	id := uuid.New()
	f[id] = &sprintdom.SprintView{ID: id, ProjectID: projectID, ViewType: vt, Name: string(vt)}
	return id
}

func TestCreateDocument_NoteInNoteView(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	views := fakeViews{}
	projectID := uuid.New()
	viewID := views.add(projectID, sprintdom.ViewTypeNote)
	svc := docsvc.New(repo, nil).WithViews(views)

	d, err := svc.CreateDocument(ctx, docdom.CreateDocumentInput{ProjectID: projectID, ViewID: &viewID, Title: "Sprint goals"})
	if err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	if d.ViewID == nil || *d.ViewID != viewID || d.FolderID != nil {
		t.Fatalf("note = view %v folder %v, want view %s and no folder", d.ViewID, d.FolderID, viewID)
	}
	stored, err := repo.FindDocumentByID(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ViewID == nil || *stored.ViewID != viewID {
		t.Fatalf("stored view = %v, want %s", stored.ViewID, viewID)
	}
}

func TestCreateDocument_NoteViewMustBeANoteViewOfTheProject(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	views := fakeViews{}
	boardView := views.add(projectID, sprintdom.ViewTypeBoard)
	otherProjectNotes := views.add(uuid.New(), sprintdom.ViewTypeNote)
	missing := uuid.New()

	for name, viewID := range map[string]uuid.UUID{
		"task view":                   boardView,
		"another project's note view": otherProjectNotes,
		"missing view":                missing,
	} {
		t.Run(name, func(t *testing.T) {
			repo := newFakeDocRepo()
			svc := docsvc.New(repo, nil).WithViews(views)
			_, err := svc.CreateDocument(ctx, docdom.CreateDocumentInput{ProjectID: projectID, ViewID: &viewID, Title: "Note"})
			if !errors.Is(err, docdom.ErrNoteViewInvalid) {
				t.Fatalf("err = %v, want ErrNoteViewInvalid", err)
			}
			if len(repo.docs) != 0 {
				t.Fatalf("a refused note was stored: %d documents", len(repo.docs))
			}
		})
	}
}

func TestCreateDocument_NoteWithoutViewLookupIsRefused(t *testing.T) {
	viewID := uuid.New()
	_, err := docsvc.New(newFakeDocRepo(), nil).CreateDocument(context.Background(),
		docdom.CreateDocumentInput{ProjectID: uuid.New(), ViewID: &viewID, Title: "Note"})
	if !errors.Is(err, docdom.ErrNoteViewInvalid) {
		t.Fatalf("err = %v, want ErrNoteViewInvalid", err)
	}
}

func TestCreateDocument_NoteCannotHaveAFolder(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	views := fakeViews{}
	projectID := uuid.New()
	viewID := views.add(projectID, sprintdom.ViewTypeNote)
	svc := docsvc.New(repo, nil).WithViews(views)
	folder, err := svc.CreateFolder(ctx, docdom.CreateFolderInput{ProjectID: projectID, Name: "F"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CreateDocument(ctx, docdom.CreateDocumentInput{ProjectID: projectID, ViewID: &viewID, FolderID: &folder.ID, Title: "Note"})
	if !errors.Is(err, docdom.ErrNoteInFolder) {
		t.Fatalf("err = %v, want ErrNoteInFolder", err)
	}
}

func TestUpdateDocument_NoteCannotMoveIntoAFolder(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	views := fakeViews{}
	projectID := uuid.New()
	viewID := views.add(projectID, sprintdom.ViewTypeNote)
	svc := docsvc.New(repo, nil).WithViews(views)
	folder, err := svc.CreateFolder(ctx, docdom.CreateFolderInput{ProjectID: projectID, Name: "F"})
	if err != nil {
		t.Fatal(err)
	}
	note, err := svc.CreateDocument(ctx, docdom.CreateDocumentInput{ProjectID: projectID, ViewID: &viewID, Title: "Note"})
	if err != nil {
		t.Fatal(err)
	}

	into := &folder.ID
	if _, _, err := svc.UpdateDocument(ctx, projectID, note.ID, docdom.UpdateDocumentInput{FolderID: &into}); !errors.Is(err, docdom.ErrNoteInFolder) {
		t.Fatalf("moving a note into a folder: err = %v, want ErrNoteInFolder", err)
	}

	// The rest of a note is edited like any document; folder_id null on a
	// note (it has none) changes nothing.
	title, pos := "Renamed", 3
	var root *uuid.UUID
	d, change, err := svc.UpdateDocument(ctx, projectID, note.ID, docdom.UpdateDocumentInput{Title: &title, Position: &pos, FolderID: &root})
	if err != nil {
		t.Fatalf("rename and reorder a note: %v", err)
	}
	if !change.Title || !change.Position || change.Folder {
		t.Fatalf("change = %+v, want title and position only", change)
	}
	if d.ViewID == nil || *d.ViewID != viewID || d.FolderID != nil {
		t.Fatalf("note moved out of its view: view %v folder %v", d.ViewID, d.FolderID)
	}
}

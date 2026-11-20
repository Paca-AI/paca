package filesvc

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	filedom "github.com/Paca-AI/api/internal/domain/file"
)

// Documents and the documentation folders belong to the docs editor: the file
// manager lists them but never renames, moves, deletes or downloads them.
func TestDocNodesAreReadOnlyInTheFileManager(t *testing.T) {
	project := uuid.New()
	scope := filedom.Scope{ProjectID: &project}
	doc := &filedom.Node{ID: uuid.New(), ProjectID: &project, Kind: filedom.KindDoc, Name: "Charter", Area: "docs"}
	folder := &filedom.Node{ID: uuid.New(), ProjectID: &project, Kind: filedom.KindFolder, Name: "Governance", Area: "docs"}
	target := &filedom.Node{ID: uuid.New(), ProjectID: &project, Kind: filedom.KindFolder, Name: "Elsewhere"}
	file := &filedom.Node{ID: uuid.New(), ProjectID: &project, Kind: filedom.KindFile, Name: "g.docx", Area: "docs", Source: filedom.SourceUpload}

	repo := newFakeRepo(doc, folder, target, file)
	s := New(repo, &fakeFiles{}, &fakeStore{}, "b")
	s.now = func() time.Time { return time.Unix(0, 0) }
	ctx, actor := t.Context(), uuid.New()

	for _, n := range []*filedom.Node{doc, folder} {
		name := "x"
		if _, err := s.Update(ctx, scope, actor, n.ID, &name, false, nil); !errors.Is(err, filedom.ErrNodeLocked) {
			t.Errorf("rename %s = %v, want ErrNodeLocked", n.Name, err)
		}
		if _, err := s.Update(ctx, scope, actor, n.ID, nil, true, &target.ID); !errors.Is(err, filedom.ErrNodeLocked) {
			t.Errorf("move %s = %v, want ErrNodeLocked", n.Name, err)
		}
	}
	if _, _, err := s.DownloadURL(ctx, scope, actor, doc.ID, time.Minute, false); !errors.Is(err, filedom.ErrNotFile) {
		t.Errorf("download a document = %v, want ErrNotFile", err)
	}
	if len(repo.moved) != 0 {
		t.Errorf("repository moved %v", repo.moved)
	}
	// They do go to the trash, with their contents, like a file.
	for _, n := range []*filedom.Node{doc, folder} {
		if err := s.Trash(ctx, scope, actor, n.ID); err != nil {
			t.Errorf("trash %s = %v", n.Name, err)
		}
	}
	if len(repo.trashed) != 2 {
		t.Errorf("trashed %v, want the document and the folder", repo.trashed)
	}
	// The Documentation folder itself stays.
	root := &filedom.Node{ID: uuid.New(), ProjectID: &project, Kind: filedom.KindFolder, Name: "Documentation", SystemKey: filedom.SystemKeyDocumentation, Area: "docs"}
	repo.nodes[root.ID] = root
	if err := s.Trash(ctx, scope, actor, root.ID); !errors.Is(err, filedom.ErrNodeLocked) {
		t.Errorf("trash Documentation = %v, want ErrNodeLocked", err)
	}

	// A file inside Documentation is an ordinary file.
	name := "renamed.docx"
	if _, err := s.Update(ctx, scope, actor, file.ID, &name, false, nil); err != nil {
		t.Errorf("rename a file in Documentation = %v", err)
	}
}

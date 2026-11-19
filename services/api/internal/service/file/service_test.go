package filesvc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	filedom "github.com/Paca-AI/api/internal/domain/file"
	"github.com/Paca-AI/api/internal/platform/storage"
)

// fakeRepo is an in-memory filedom.Repository holding nodes by id.
type fakeRepo struct {
	nodes     map[uuid.UUID]*filedom.Node
	unlinked  []uuid.UUID
	purged    []uuid.UUID
	trashed   []uuid.UUID
	moved     map[uuid.UUID]*uuid.UUID
	descends  bool
	touched   int
	starred   map[uuid.UUID]bool
	purgeObjs []filedom.StorageObject
}

func newFakeRepo(nodes ...*filedom.Node) *fakeRepo {
	r := &fakeRepo{nodes: map[uuid.UUID]*filedom.Node{}, moved: map[uuid.UUID]*uuid.UUID{}, starred: map[uuid.UUID]bool{}}
	for _, n := range nodes {
		r.nodes[n.ID] = n
	}
	return r
}

func (r *fakeRepo) GetNode(_ context.Context, id, _ uuid.UUID) (*filedom.Node, error) {
	if n, ok := r.nodes[id]; ok {
		c := *n
		return &c, nil
	}
	return nil, filedom.ErrNodeNotFound
}
func (r *fakeRepo) List(context.Context, filedom.ListParams) ([]*filedom.Node, int, error) {
	return nil, 0, nil
}
func (r *fakeRepo) ListStarred(context.Context, filedom.Scope, uuid.UUID, int, int) ([]*filedom.Node, int, error) {
	return nil, 0, nil
}
func (r *fakeRepo) ListRecent(context.Context, filedom.Scope, uuid.UUID, int) ([]*filedom.Node, error) {
	return nil, nil
}
func (r *fakeRepo) ListTrash(context.Context, filedom.Scope, uuid.UUID, int, int) ([]*filedom.Node, int, error) {
	return nil, 0, nil
}
func (r *fakeRepo) Path(context.Context, uuid.UUID) ([]*filedom.Node, error) { return nil, nil }
func (r *fakeRepo) CreateFolder(_ context.Context, n *filedom.Node) error {
	r.nodes[n.ID] = n
	return nil
}
func (r *fakeRepo) Rename(_ context.Context, n *filedom.Node, name string) error {
	r.nodes[n.ID].Name = name
	return nil
}
func (r *fakeRepo) Move(_ context.Context, n *filedom.Node, parent *uuid.UUID) error {
	r.moved[n.ID] = parent
	return nil
}
func (r *fakeRepo) IsSelfOrDescendant(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return r.descends, nil
}
func (r *fakeRepo) Trash(_ context.Context, n *filedom.Node, _ uuid.UUID, _ time.Time) error {
	r.trashed = append(r.trashed, n.ID)
	return nil
}
func (r *fakeRepo) Restore(context.Context, *filedom.Node) error { return nil }
func (r *fakeRepo) PurgeSubtree(_ context.Context, n *filedom.Node) ([]filedom.StorageObject, error) {
	r.purged = append(r.purged, n.ID)
	return r.purgeObjs, nil
}
func (r *fakeRepo) PurgeTrashedBefore(context.Context, time.Time, int) ([]filedom.StorageObject, error) {
	return r.purgeObjs, nil
}
func (r *fakeRepo) UnlinkTaskAttachment(_ context.Context, id uuid.UUID) (bool, error) {
	r.unlinked = append(r.unlinked, id)
	return true, nil
}
func (r *fakeRepo) SetStarred(_ context.Context, _, item uuid.UUID, on bool) error {
	r.starred[item] = on
	return nil
}
func (r *fakeRepo) Touch(context.Context, uuid.UUID, uuid.UUID) error { r.touched++; return nil }

type fakeFiles struct {
	files map[uuid.UUID]*attachmentdom.File
}

func (f *fakeFiles) CreateFile(_ context.Context, file *attachmentdom.File) error {
	f.files[file.ID] = file
	return nil
}
func (f *fakeFiles) FindFileByID(_ context.Context, id uuid.UUID) (*attachmentdom.File, error) {
	if file, ok := f.files[id]; ok {
		return file, nil
	}
	return nil, attachmentdom.ErrFileNotFound
}
func (f *fakeFiles) UpdateFileStatus(_ context.Context, id uuid.UUID, st attachmentdom.UploadStatus, mp *string) error {
	f.files[id].UploadStatus = st
	f.files[id].MultipartUploadID = mp
	return nil
}
func (f *fakeFiles) DeleteFile(_ context.Context, id uuid.UUID) error {
	delete(f.files, id)
	return nil
}

type fakeStore struct {
	deleted []string
	got     string
}

func (s *fakeStore) PresignPutObject(_ context.Context, _, key, _ string, _ time.Duration) (string, error) {
	return "put://" + key, nil
}
func (s *fakeStore) PresignGetObject(_ context.Context, _, key string, _ time.Duration, disp string) (string, error) {
	s.got = disp
	return "get://" + key, nil
}
func (s *fakeStore) InitiateMultipartUpload(_ context.Context, _, _, _ string, _ int64, _ int64, _ time.Duration) (*storage.MultipartUpload, error) {
	return &storage.MultipartUpload{UploadID: "u1"}, nil
}
func (s *fakeStore) CompleteMultipartUpload(context.Context, string, string, string, []storage.CompletedPart) error {
	return nil
}
func (s *fakeStore) AbortMultipartUpload(context.Context, string, string, string) error { return nil }
func (s *fakeStore) DeleteObject(_ context.Context, _, key string) error {
	s.deleted = append(s.deleted, key)
	return nil
}
func (s *fakeStore) EnsureBucket(context.Context, string) error { return nil }
func (s *fakeStore) GetObject(context.Context, string, string, int64) ([]byte, error) {
	return nil, nil
}
func (s *fakeStore) PutObject(context.Context, string, string, string, []byte) error { return nil }

var (
	project = uuid.New()
	other   = uuid.New()
	actor   = uuid.New()
)

func folder(name, system string, p *uuid.UUID) *filedom.Node {
	return &filedom.Node{ID: uuid.New(), ProjectID: p, Kind: filedom.KindFolder, Name: name, SystemKey: system}
}

func fileNode(name, source string, parent *filedom.Node) *filedom.Node {
	n := &filedom.Node{ID: uuid.New(), ProjectID: &project, Kind: filedom.KindFile, Name: name, Source: source, StorageKey: "k/" + name, Bucket: "b"}
	if parent != nil {
		n.ParentID = &parent.ID
		n.ParentSystemKey = parent.SystemKey
	}
	return n
}

func newSvc(repo *fakeRepo) (*Service, *fakeFiles, *fakeStore) {
	files := &fakeFiles{files: map[uuid.UUID]*attachmentdom.File{}}
	store := &fakeStore{}
	return New(repo, files, store, "b"), files, store
}

var scope = filedom.Scope{ProjectID: &project}

func TestNodesOfAnotherScopeAreNotFound(t *testing.T) {
	foreign := folder("X", "", &other)
	svc, _, _ := newSvc(newFakeRepo(foreign))
	if _, err := svc.Get(t.Context(), scope, actor, foreign.ID); !errors.Is(err, filedom.ErrNodeNotFound) {
		t.Fatalf("Get across projects = %v, want not found", err)
	}
	if err := svc.Trash(t.Context(), scope, actor, foreign.ID); !errors.Is(err, filedom.ErrNodeNotFound) {
		t.Fatalf("Trash across projects = %v, want not found", err)
	}
	general := filedom.Scope{}
	if _, err := svc.Get(t.Context(), general, actor, foreign.ID); !errors.Is(err, filedom.ErrNodeNotFound) {
		t.Fatalf("a project folder must not be reachable from the general scope, got %v", err)
	}
}

func TestDefaultFoldersAreLocked(t *testing.T) {
	sys := folder("Task attachments", filedom.SystemKeyTaskRoot, &project)
	inSys := fileNode("img.png", filedom.SourceDoc, sys)
	repo := newFakeRepo(sys, inSys)
	svc, _, _ := newSvc(repo)

	if _, err := svc.Rename(t.Context(), scope, actor, sys.ID, "x"); !errors.Is(err, filedom.ErrNodeLocked) {
		t.Fatalf("rename default folder = %v", err)
	}
	if err := svc.Trash(t.Context(), scope, actor, sys.ID); !errors.Is(err, filedom.ErrNodeLocked) {
		t.Fatalf("trash default folder = %v", err)
	}
	if _, err := svc.Rename(t.Context(), scope, actor, inSys.ID, "x"); !errors.Is(err, filedom.ErrNodeLocked) {
		t.Fatalf("rename file inside default folder = %v", err)
	}
	if _, err := svc.Move(t.Context(), scope, actor, inSys.ID, nil); !errors.Is(err, filedom.ErrNodeLocked) {
		t.Fatalf("move file out of default folder = %v", err)
	}
	if _, err := svc.CreateFolder(t.Context(), scope, actor, &sys.ID, "sub"); !errors.Is(err, filedom.ErrNodeLocked) {
		t.Fatalf("create folder inside default folder = %v", err)
	}
	if _, err := svc.InitiateUpload(t.Context(), scope, actor, filedom.UploadInput{ParentID: &sys.ID, FileName: "a.txt", FileSize: 1}); !errors.Is(err, filedom.ErrNodeLocked) {
		t.Fatalf("upload into default folder = %v", err)
	}
}

// A file that something else references is deleted for good, never parked in
// the trash where it would later detach from its owner without a word.
func TestReferencedFilesAreDeletedForGood(t *testing.T) {
	for _, source := range []string{filedom.SourceDoc, filedom.SourceAnnotation} {
		sys := folder("Default", filedom.SystemKeyDocs, &project)
		f := fileNode("shot.png", source, sys)
		repo := newFakeRepo(sys, f)
		repo.purgeObjs = []filedom.StorageObject{{Bucket: "b", Key: "k/shot.png"}}
		svc, _, store := newSvc(repo)

		if err := svc.Trash(t.Context(), scope, actor, f.ID); err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		if len(repo.trashed) != 0 || len(repo.purged) != 1 || len(store.deleted) != 1 {
			t.Fatalf("%s: trashed %v purged %v deleted %v, want purged and removed from the store", source, repo.trashed, repo.purged, store.deleted)
		}
	}

	// A plain upload still goes to the trash.
	up := fileNode("plain.txt", filedom.SourceUpload, nil)
	repo := newFakeRepo(up)
	svc, _, _ := newSvc(repo)
	if err := svc.Trash(t.Context(), scope, actor, up.ID); err != nil || len(repo.trashed) != 1 || len(repo.purged) != 0 {
		t.Fatalf("trash upload = %v, trashed %v purged %v", err, repo.trashed, repo.purged)
	}
}

func TestTrashingATaskAttachmentDeletesItForGood(t *testing.T) {
	taskFolder := folder("PACA-1", filedom.SystemKeyTask, &project)
	att := fileNode("spec.pdf", filedom.SourceTaskAttachment, taskFolder)
	repo := newFakeRepo(taskFolder, att)
	repo.purgeObjs = []filedom.StorageObject{{Bucket: "b", Key: "k/spec.pdf"}}
	svc, _, store := newSvc(repo)

	if err := svc.Trash(t.Context(), scope, actor, att.ID); err != nil {
		t.Fatal(err)
	}
	if len(repo.unlinked) != 1 || repo.unlinked[0] != att.ID || len(repo.purged) != 1 || len(repo.trashed) != 0 {
		t.Fatalf("unlinked %v purged %v trashed %v: attachment must be unlinked and purged, not trashed", repo.unlinked, repo.purged, repo.trashed)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "k/spec.pdf" {
		t.Fatalf("deleted objects = %v", store.deleted)
	}
}

func TestMoveRules(t *testing.T) {
	a, b := folder("A", "", &project), folder("B", "", &project)
	f := fileNode("f.txt", filedom.SourceUpload, nil)
	repo := newFakeRepo(a, b, f)
	svc, _, _ := newSvc(repo)

	repo.descends = true
	if _, err := svc.Move(t.Context(), scope, actor, a.ID, &b.ID); !errors.Is(err, filedom.ErrMoveIntoSelf) {
		t.Fatalf("moving a folder into its descendant = %v", err)
	}
	// A file has no descendants, so the cycle check does not apply to it.
	if _, err := svc.Move(t.Context(), scope, actor, f.ID, &b.ID); err != nil {
		t.Fatalf("move file = %v", err)
	}
	if _, err := svc.Move(t.Context(), scope, actor, f.ID, &f.ID); !errors.Is(err, filedom.ErrNotFolder) {
		t.Fatalf("moving into a file = %v, want not-a-folder", err)
	}
}

func TestNameValidation(t *testing.T) {
	svc, _, _ := newSvc(newFakeRepo())
	for _, name := range []string{"", "  ", ".", "..", "a/b", `a\b`, "bad\nname"} {
		if _, err := svc.CreateFolder(t.Context(), scope, actor, nil, name); !errors.Is(err, filedom.ErrNameInvalid) {
			t.Errorf("CreateFolder(%q) = %v, want invalid name", name, err)
		}
	}
	n, err := svc.CreateFolder(t.Context(), scope, actor, nil, "  Reports  ")
	if err != nil || n.Name != "Reports" {
		t.Fatalf("CreateFolder trims the name: %+v, %v", n, err)
	}
}

func TestUploadFlow(t *testing.T) {
	svc, files, _ := newSvc(newFakeRepo())
	repoNode := fileNode("a.txt", filedom.SourceUpload, nil)
	_ = repoNode

	session, err := svc.InitiateUpload(t.Context(), scope, actor, filedom.UploadInput{FileName: "a.txt", ContentType: "text/plain", FileSize: 10})
	if err != nil || session.UploadURL == "" || session.IsMultipart {
		t.Fatalf("small upload = %+v, %v", session, err)
	}
	f := files.files[session.FileID]
	if f.ProjectID == nil || *f.ProjectID != project || f.UploadStatus != attachmentdom.UploadStatusPending {
		t.Fatalf("pending file = %+v, want project scope and pending", f)
	}

	big, err := svc.InitiateUpload(t.Context(), scope, actor, filedom.UploadInput{FileName: "b.bin", FileSize: storage.MultipartThreshold})
	if err != nil || !big.IsMultipart || big.Multipart.UploadID != "u1" {
		t.Fatalf("multipart upload = %+v, %v", big, err)
	}
	if _, err := svc.InitiateUpload(t.Context(), scope, actor, filedom.UploadInput{FileName: "c", FileSize: filedom.MaxFileSize + 1}); !errors.Is(err, filedom.ErrFileTooLarge) {
		t.Fatalf("oversize upload = %v", err)
	}
	if _, err := svc.InitiateUpload(t.Context(), scope, actor, filedom.UploadInput{FileName: "c", FileSize: 0}); !errors.Is(err, attachmentdom.ErrFileSizeZero) {
		t.Fatalf("empty upload = %v", err)
	}

	// Another user, or another scope, cannot complete the upload.
	if _, err := svc.CompleteUpload(t.Context(), scope, uuid.New(), filedom.CompleteInput{FileID: session.FileID}); !errors.Is(err, filedom.ErrUploadScopeMismatch) {
		t.Fatalf("complete as another user = %v", err)
	}
	if _, err := svc.CompleteUpload(t.Context(), filedom.Scope{}, actor, filedom.CompleteInput{FileID: session.FileID}); !errors.Is(err, filedom.ErrUploadScopeMismatch) {
		t.Fatalf("complete from the general scope = %v", err)
	}
}

func TestDownloadRules(t *testing.T) {
	f := fileNode("my file.txt", filedom.SourceUpload, nil)
	d := folder("D", "", &project)
	gone := fileNode("old.txt", filedom.SourceUpload, nil)
	now := time.Now()
	gone.DeletedAt = &now
	repo := newFakeRepo(f, d, gone)
	svc, _, store := newSvc(repo)

	url, _, err := svc.DownloadURL(t.Context(), scope, actor, f.ID, time.Minute, true)
	if err != nil || url != "get://k/my file.txt" || store.got == "" || repo.touched != 1 {
		t.Fatalf("download = %q, %v, disposition %q, touched %d", url, err, store.got, repo.touched)
	}
	if _, _, err := svc.DownloadURL(t.Context(), scope, actor, d.ID, time.Minute, false); !errors.Is(err, filedom.ErrNotFile) {
		t.Fatalf("download a folder = %v", err)
	}
	if _, _, err := svc.DownloadURL(t.Context(), scope, actor, gone.ID, time.Minute, false); !errors.Is(err, filedom.ErrNodeTrashed) {
		t.Fatalf("download a trashed file = %v", err)
	}
}

func TestRestoreAndPermanentDeleteNeedTheTrash(t *testing.T) {
	live := fileNode("a.txt", filedom.SourceUpload, nil)
	svc, _, _ := newSvc(newFakeRepo(live))
	if _, err := svc.Restore(t.Context(), scope, actor, live.ID); !errors.Is(err, filedom.ErrNodeNotTrashed) {
		t.Fatalf("restore a live file = %v", err)
	}
	if err := svc.DeletePermanently(t.Context(), scope, live.ID); !errors.Is(err, filedom.ErrNodeNotTrashed) {
		t.Fatalf("permanently delete a live file = %v", err)
	}
}

package exportsvc

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	activitydom "github.com/Paca-AI/api/internal/domain/activity"
	docdom "github.com/Paca-AI/api/internal/domain/doc"
	exportdom "github.com/Paca-AI/api/internal/domain/export"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/storage"
)

// ---- fakes ----

type fakeRepo struct {
	mu       sync.Mutex
	rows     map[uuid.UUID]*exportdom.ProjectExport
	claimErr error
}

func newFakeRepo() *fakeRepo { return &fakeRepo{rows: map[uuid.UUID]*exportdom.ProjectExport{}} }

// seed stores a row directly, bypassing the idle check (test fixtures).
func (r *fakeRepo) seed(e *exportdom.ProjectExport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *e
	r.rows[e.ID] = &c
}

func (r *fakeRepo) CreateIfIdle(_ context.Context, e *exportdom.ProjectExport, notBefore time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.rows {
		if x.ProjectID == e.ProjectID && x.Status.Active() && !x.UpdatedAt.Before(notBefore) {
			return false, nil
		}
	}
	c := *e
	r.rows[e.ID] = &c
	return true, nil
}
func (r *fakeRepo) FindByID(_ context.Context, id uuid.UUID) (*exportdom.ProjectExport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.rows[id]
	if !ok {
		return nil, exportdom.ErrNotFound
	}
	c := *e
	return &c, nil
}
func (r *fakeRepo) ListByProject(_ context.Context, pid uuid.UUID, _ int) ([]*exportdom.ProjectExport, error) {
	var out []*exportdom.ProjectExport
	for _, e := range r.rows {
		if e.ProjectID == pid {
			out = append(out, e)
		}
	}
	return out, nil
}
func (r *fakeRepo) Claim(_ context.Context, id uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.claimErr != nil {
		return false, r.claimErr
	}
	e, ok := r.rows[id]
	if !ok || e.Status != exportdom.StatusPending {
		return false, nil
	}
	e.Status = exportdom.StatusProcessing
	return true, nil
}
func (r *fakeRepo) MarkCompleted(_ context.Context, id uuid.UUID, key, name string, size int64, rows int, exp time.Time) error {
	e := r.rows[id]
	e.Status, e.FileKey, e.FileName, e.FileSize, e.RowCount, e.ExpiresAt = exportdom.StatusCompleted, &key, &name, &size, &rows, &exp
	return nil
}
func (r *fakeRepo) MarkFailed(ctx context.Context, id uuid.UUID, msg string, expiresAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e := r.rows[id]
	e.Status, e.ErrorMessage, e.ExpiresAt = exportdom.StatusFailed, &msg, &expiresAt
	return nil
}
func (r *fakeRepo) ListExpired(_ context.Context, now time.Time, _ int) ([]*exportdom.ProjectExport, error) {
	var out []*exportdom.ProjectExport
	for _, e := range r.rows {
		if e.ExpiresAt != nil && e.ExpiresAt.Before(now) {
			out = append(out, e)
		}
	}
	return out, nil
}
func (r *fakeRepo) ListStale(_ context.Context, notAfter time.Time, _ int) ([]*exportdom.ProjectExport, error) {
	var out []*exportdom.ProjectExport
	for _, e := range r.rows {
		if e.Status.Active() && e.UpdatedAt.Before(notAfter) {
			out = append(out, e)
		}
	}
	return out, nil
}
func (r *fakeRepo) Delete(_ context.Context, id uuid.UUID) error { delete(r.rows, id); return nil }

type fakeProjects struct {
	projectdom.Repository
	p *projectdom.Project
}

func (f fakeProjects) FindByID(context.Context, uuid.UUID) (*projectdom.Project, error) {
	return f.p, nil
}

type fakeMembers struct {
	projectdom.MemberRepository
	members []*projectdom.ProjectMember
}

func (f fakeMembers) ListMembers(context.Context, uuid.UUID) ([]*projectdom.ProjectMember, error) {
	return f.members, nil
}

type fakeSprints struct{ sprintdom.SprintRepository }

func (fakeSprints) ListSprints(context.Context, uuid.UUID) ([]*sprintdom.Sprint, error) {
	return nil, nil
}

// fakeTasks serves a fixed task list through the real keyset-cursor contract:
// it decodes the cursor the service built with taskdom.EncodeTaskCursor and
// resumes after the task that cursor names.
type fakeTasks struct {
	taskdom.Repository
	tasks   []*taskdom.Task
	listErr error
	calls   int
}

func (f *fakeTasks) ListTasks(_ context.Context, _ uuid.UUID, filter taskdom.TaskFilter, limit int, sort taskdom.TaskSort) ([]*taskdom.Task, bool, error) {
	f.calls++
	if f.listErr != nil {
		return nil, false, f.listErr
	}
	start := 0
	if filter.CursorAfter != nil {
		cur, err := taskdom.DecodeTaskCursor(*filter.CursorAfter)
		if err != nil {
			return nil, false, err
		}
		for i, t := range f.tasks {
			if t.ID.String() == cur.ID {
				start = i + 1
			}
		}
	}
	end := start + limit
	if end > len(f.tasks) {
		end = len(f.tasks)
	}
	return f.tasks[start:end], end < len(f.tasks), nil
}
func (f *fakeTasks) ListTaskStatuses(context.Context, uuid.UUID) ([]*taskdom.TaskStatus, error) {
	return nil, nil
}
func (f *fakeTasks) ListTaskTypes(context.Context, uuid.UUID) ([]*taskdom.TaskType, error) {
	return nil, nil
}
func (f *fakeTasks) ListCustomFieldDefinitions(context.Context, uuid.UUID) ([]*taskdom.CustomFieldDefinition, error) {
	return nil, nil
}
func (f *fakeTasks) FindTaskByID(context.Context, uuid.UUID) (*taskdom.Task, error) {
	return nil, errors.New("not found")
}

type fakeDocs struct {
	docdom.Repository
	folders []*docdom.DocFolder
	docs    []*docdom.Document
}

func (f fakeDocs) ListFolders(context.Context, uuid.UUID) ([]*docdom.DocFolder, error) {
	return f.folders, nil
}
func (f fakeDocs) ListDocuments(context.Context, uuid.UUID, *uuid.UUID, *string, *string, *int) ([]*docdom.Document, bool, error) {
	return f.docs, false, nil
}

// fakeActivity serves entries newest first through the real cursor contract.
type fakeActivity struct {
	activitydom.Repository
	entries []*activitydom.Activity // newest first
	calls   int
}

func (f *fakeActivity) List(_ context.Context, flt activitydom.ListFilter, limit int) ([]*activitydom.Activity, bool, error) {
	f.calls++
	var matched []*activitydom.Activity
	started := flt.Cursor == nil
	for _, a := range f.entries {
		if !started {
			if a.ID == flt.Cursor.ID {
				started = true
			}
			continue
		}
		if len(flt.EntityTypes) > 0 && !contains(flt.EntityTypes, a.EntityType) {
			continue
		}
		if len(flt.ActivityTypes) > 0 && !contains(flt.ActivityTypes, a.ActivityType) {
			continue
		}
		if contains(flt.ExcludeActivityTypes, a.ActivityType) {
			continue
		}
		matched = append(matched, a)
	}
	if len(matched) > limit {
		return matched[:limit], true, nil
	}
	return matched, false, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

type fakeStore struct {
	storage.Client
	objects   map[string][]byte
	putErr    error
	deleted   []string
	presigned []string
}

func (s *fakeStore) PutObject(_ context.Context, _, key, _ string, data []byte) error {
	if s.putErr != nil {
		return s.putErr
	}
	s.objects[key] = data
	return nil
}
func (s *fakeStore) DeleteObject(_ context.Context, _, key string) error {
	s.deleted = append(s.deleted, key)
	delete(s.objects, key)
	return nil
}
func (s *fakeStore) PresignGetObject(_ context.Context, _, key string, _ time.Duration, disposition string) (string, error) {
	s.presigned = append(s.presigned, disposition)
	return "https://files.example/" + key, nil
}

type fakePublisher struct {
	events.Publisher
	appended []string
	err      error
}

func (p *fakePublisher) Append(_ context.Context, stream, eventType string, _ any) error {
	if p.err != nil {
		return p.err
	}
	p.appended = append(p.appended, stream+"|"+eventType)
	return nil
}

// ---- harness ----

type harness struct {
	svc   *Service
	repo  *fakeRepo
	tasks *fakeTasks
	store *fakeStore
	acts  *fakeActivity
	docs  *fakeDocs
	pub   *fakePublisher
	pid   uuid.UUID
	now   time.Time
}

func newHarness(taskCount int) *harness {
	pid := uuid.New()
	tasks := &fakeTasks{}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < taskCount; i++ {
		tasks.tasks = append(tasks.tasks, &taskdom.Task{
			ID: uuid.New(), ProjectID: pid, TaskNumber: int64(i + 1), Title: "task",
			CreatedAt: base.Add(time.Duration(i) * time.Second), UpdatedAt: base,
		})
	}
	h := &harness{
		repo: newFakeRepo(), tasks: tasks, pid: pid, acts: &fakeActivity{}, docs: &fakeDocs{},
		store: &fakeStore{objects: map[string][]byte{}}, pub: &fakePublisher{},
		now: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC),
	}
	h.svc = New(h.repo, fakeProjects{p: &projectdom.Project{ID: pid, Name: "Paca", TaskIDPrefix: "PAC"}},
		fakeMembers{}, tasks, fakeSprints{}, h.docs, h.acts, h.store, "bucket", h.pub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.svc.now = func() time.Time { return h.now }
	return h
}

func errCode(err error) apierr.Code {
	var ae *apierr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

// ---- tests ----

func TestRequestExport_QueuesAndBlocksDuplicates(t *testing.T) {
	h := newHarness(0)
	user := uuid.New()
	e, err := h.svc.RequestExport(context.Background(), h.pid, user)
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != exportdom.StatusPending || e.RequestedBy == nil || *e.RequestedBy != user {
		t.Fatalf("unexpected export: %+v", e)
	}
	if len(h.pub.appended) != 1 || h.pub.appended[0] != events.StreamProjectExports+"|"+events.TopicProjectExportRequested {
		t.Fatalf("appended = %v", h.pub.appended)
	}

	_, err = h.svc.RequestExport(context.Background(), h.pid, user)
	if errCode(err) != apierr.CodeProjectExportInProgress {
		t.Fatalf("second request err = %v, want in-progress", err)
	}

	// An abandoned (stale) active export must not block forever.
	h.now = h.now.Add(exportdom.StaleAfter + time.Minute)
	if _, err := h.svc.RequestExport(context.Background(), h.pid, user); err != nil {
		t.Fatalf("request after stale window: %v", err)
	}
}

func TestRequestExport_QueueFailureFailsTheRow(t *testing.T) {
	h := newHarness(0)
	h.pub.err = errors.New("valkey down")
	if _, err := h.svc.RequestExport(context.Background(), h.pid, uuid.New()); err == nil {
		t.Fatal("expected error")
	}
	for _, e := range h.repo.rows {
		if e.Status != exportdom.StatusFailed {
			t.Errorf("row status = %s, want failed", e.Status)
		}
	}
	// And it must not block a retry.
	h.pub.err = nil
	if _, err := h.svc.RequestExport(context.Background(), h.pid, uuid.New()); err != nil {
		t.Fatalf("retry: %v", err)
	}
}

// readZip returns the archive's entries by name.
func readZip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if _, err := b.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		_ = rc.Close()
		out[f.Name] = b.String()
	}
	return out
}

func TestExecute_BuildsArchiveWithEverything(t *testing.T) {
	h := newHarness(pageSize*2 + 3)

	// 1,200 task entries newest first, alternating comment / non-comment, plus
	// one entry for a different entity type that must be ignored.
	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	taskID := h.tasks.tasks[0].ID
	for i := 0; i < 1200; i++ {
		typ, content := "task.updated", `{"field":"status"}`
		if i%2 == 0 {
			typ, content = activitydom.TypeComment, `[{"type":"paragraph","content":[{"type":"text","text":"hello"}]}]`
		}
		h.acts.entries = append(h.acts.entries, &activitydom.Activity{
			ID: uuid.New(), EntityType: "task", EntityID: &taskID, EntityTitle: "task",
			ActivityType: typ, Content: json.RawMessage(content), ActorName: "Ada", Origin: "user",
			CreatedAt: base.Add(-time.Duration(i) * time.Minute), UpdatedAt: base.Add(-time.Duration(i) * time.Minute),
		})
	}
	h.acts.entries = append(h.acts.entries, &activitydom.Activity{ID: uuid.New(), EntityType: "doc", ActivityType: activitydom.TypeComment})

	folder := &docdom.DocFolder{ID: uuid.New(), Name: "Guides"}
	h.docs.folders = []*docdom.DocFolder{folder}
	h.docs.docs = []*docdom.Document{
		{ID: uuid.New(), Title: "Intro", Content: json.RawMessage(`[{"type":"heading","props":{"level":2},"content":[{"type":"text","text":"Hi"}]}]`)},
		{ID: uuid.New(), Title: "Setup", FolderID: &folder.ID},
	}

	e, _ := h.svc.RequestExport(context.Background(), h.pid, uuid.New())
	if err := h.svc.Execute(context.Background(), e.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := h.repo.FindByID(context.Background(), e.ID)
	if got.Status != exportdom.StatusCompleted {
		t.Fatalf("status = %s", got.Status)
	}
	if got.RowCount == nil || *got.RowCount != pageSize*2+3 {
		t.Fatalf("row count = %v, want %d tasks", got.RowCount, pageSize*2+3)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(h.now.Add(exportdom.RetentionPeriod)) {
		t.Errorf("expires_at = %v", got.ExpiresAt)
	}
	wantKey := "exports/projects/" + h.pid.String() + "/" + e.ID.String() + "/Paca-export-2026-10-03.zip"
	if got.FileKey == nil || *got.FileKey != wantKey {
		t.Fatalf("file key = %v, want %s", got.FileKey, wantKey)
	}
	if h.tasks.calls != 3 {
		t.Errorf("ListTasks calls = %d, want 3 pages", h.tasks.calls)
	}

	files := readZip(t, h.store.objects[wantKey])
	for _, name := range []string{"tasks.csv", "task-comments.csv", "task-activities.csv", "docs/Intro.md", "docs/Guides/Setup.md"} {
		if _, ok := files[name]; !ok {
			t.Errorf("archive is missing %s (has %v)", name, keys(files))
		}
	}
	if n := strings.Count(files["tasks.csv"], "\n"); n != pageSize*2+3+1 {
		t.Errorf("tasks.csv lines = %d, want header + %d rows", n, pageSize*2+3)
	}
	if !strings.Contains(files["tasks.csv"], "PAC-1,") || !strings.Contains(files["tasks.csv"], "PAC-1003,") {
		t.Error("expected resolved task keys in tasks.csv")
	}
	// 1,200 entries at 500 per page = 3 pages per pass; each file takes its own pass.
	if n := strings.Count(files["task-comments.csv"], "\n"); n != 600+1 {
		t.Errorf("task-comments.csv lines = %d, want header + 600", n)
	}
	if n := strings.Count(files["task-activities.csv"], "\n"); n != 600+1 {
		t.Errorf("task-activities.csv lines = %d, want header + 600", n)
	}
	if !strings.Contains(files["task-comments.csv"], "PAC-1,task,Ada,user,hello,") {
		t.Errorf("comment row not resolved: %q", firstLines(files["task-comments.csv"], 2))
	}
	if !strings.Contains(files["task-activities.csv"], `task.updated,Ada,user,"{""field"":""status""}"`) {
		t.Errorf("activity row missing: %q", firstLines(files["task-activities.csv"], 2))
	}
	if files["docs/Intro.md"] != "# Intro\n\n## Hi\n" {
		t.Errorf("Intro.md = %q", files["docs/Intro.md"])
	}
	if files["docs/Guides/Setup.md"] != "# Setup\n" {
		t.Errorf("Setup.md = %q", files["docs/Guides/Setup.md"])
	}
}

func TestExecute_EmptyProjectStillProducesAValidArchive(t *testing.T) {
	h := newHarness(0)
	e, _ := h.svc.RequestExport(context.Background(), h.pid, uuid.New())
	if err := h.svc.Execute(context.Background(), e.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := h.repo.FindByID(context.Background(), e.ID)
	files := readZip(t, h.store.objects[*got.FileKey])
	if len(files) != 3 {
		t.Fatalf("entries = %v, want just the three CSVs", keys(files))
	}
	for name, body := range files {
		if strings.Count(body, "\n") != 1 {
			t.Errorf("%s should be a header only, got %q", name, body)
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func TestExecute_IsIdempotentOnRedelivery(t *testing.T) {
	h := newHarness(2)
	e, _ := h.svc.RequestExport(context.Background(), h.pid, uuid.New())
	if err := h.svc.Execute(context.Background(), e.ID); err != nil {
		t.Fatal(err)
	}
	callsAfterFirst := h.tasks.calls
	if err := h.svc.Execute(context.Background(), e.ID); err != nil {
		t.Fatal(err)
	}
	if h.tasks.calls != callsAfterFirst || len(h.store.objects) != 1 {
		t.Error("redelivered message re-ran a finished export")
	}
	// A message for a row that no longer exists is a no-op, not an error.
	if err := h.svc.Execute(context.Background(), uuid.New()); err != nil {
		t.Errorf("unknown export: %v", err)
	}
}

func TestExecute_FailuresAreRecordedWithoutLeakingCause(t *testing.T) {
	for name, mutate := range map[string]func(*harness){
		"read error":   func(h *harness) { h.tasks.listErr = errors.New("pq: secret-host connection refused") },
		"upload error": func(h *harness) { h.store.putErr = errors.New("dial tcp rustfs:9000: refused") },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(3)
			mutate(h)
			e, _ := h.svc.RequestExport(context.Background(), h.pid, uuid.New())
			if err := h.svc.Execute(context.Background(), e.ID); err == nil {
				t.Fatal("expected the cause to be returned for logging")
			}
			got, _ := h.repo.FindByID(context.Background(), e.ID)
			if got.Status != exportdom.StatusFailed {
				t.Fatalf("status = %s, want failed", got.Status)
			}
			if got.ErrorMessage == nil || *got.ErrorMessage != ErrorMessageGeneric {
				t.Errorf("error message = %v, want the generic message", got.ErrorMessage)
			}
			if len(h.store.objects) != 0 {
				t.Error("a failed export must not leave a file behind")
			}
		})
	}
}

func TestGetAndDownload(t *testing.T) {
	h := newHarness(1)
	e, _ := h.svc.RequestExport(context.Background(), h.pid, uuid.New())

	// Not ready while pending.
	if _, _, err := h.svc.DownloadURL(context.Background(), h.pid, e.ID); errCode(err) != apierr.CodeProjectExportNotReady {
		t.Fatalf("pending download err = %v", err)
	}
	// Other project's export reads as not found.
	if _, err := h.svc.Get(context.Background(), uuid.New(), e.ID); errCode(err) != apierr.CodeProjectExportNotFound {
		t.Fatalf("cross-project get err = %v", err)
	}
	if _, err := h.svc.Get(context.Background(), h.pid, uuid.New()); errCode(err) != apierr.CodeProjectExportNotFound {
		t.Fatalf("unknown get err = %v", err)
	}

	if err := h.svc.Execute(context.Background(), e.ID); err != nil {
		t.Fatal(err)
	}
	url, ttl, err := h.svc.DownloadURL(context.Background(), h.pid, e.ID)
	if err != nil || !strings.HasPrefix(url, "https://files.example/exports/projects/") || ttl <= 0 {
		t.Fatalf("download = %q %v %v", url, ttl, err)
	}
	if got := h.store.presigned[0]; got != `attachment; filename="Paca-export-2026-10-03.zip"` {
		t.Errorf("content disposition = %q", got)
	}

	h.now = h.now.Add(exportdom.RetentionPeriod + time.Second)
	if _, _, err := h.svc.DownloadURL(context.Background(), h.pid, e.ID); errCode(err) != apierr.CodeProjectExportExpired {
		t.Fatalf("expired download err = %v", err)
	}
}

func TestCleanupExpired(t *testing.T) {
	h := newHarness(1)
	done, _ := h.svc.RequestExport(context.Background(), h.pid, uuid.New())
	if err := h.svc.Execute(context.Background(), done.ID); err != nil {
		t.Fatal(err)
	}
	// A second project's export that never finished: stuck "processing".
	stuck := &exportdom.ProjectExport{ID: uuid.New(), ProjectID: uuid.New(), Kind: exportdom.KindProjectArchive,
		Status: exportdom.StatusProcessing, CreatedAt: h.now, UpdatedAt: h.now}
	h.repo.seed(stuck)

	if n, err := h.svc.CleanupExpired(context.Background()); err != nil || n != 0 {
		t.Fatalf("nothing is due yet: n=%d err=%v", n, err)
	}

	h.now = h.now.Add(exportdom.RetentionPeriod + time.Hour)
	n, err := h.svc.CleanupExpired(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("cleanup n=%d err=%v, want 1", n, err)
	}
	if len(h.store.objects) != 0 || len(h.store.deleted) != 1 {
		t.Error("expired file was not deleted from storage")
	}
	if _, err := h.repo.FindByID(context.Background(), done.ID); !errors.Is(err, exportdom.ErrNotFound) {
		t.Error("expired row was not deleted")
	}
	if s, _ := h.repo.FindByID(context.Background(), stuck.ID); s.Status != exportdom.StatusFailed {
		t.Errorf("stuck export status = %s, want failed", s.Status)
	}
}

func TestRequestExport_ConcurrentRequestsQueueOnlyOne(t *testing.T) {
	h := newHarness(0)
	const n = 20
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, err := h.svc.RequestExport(context.Background(), h.pid, uuid.New())
			results <- err
		}()
	}
	ok, busy := 0, 0
	for i := 0; i < n; i++ {
		switch err := <-results; {
		case err == nil:
			ok++
		case errCode(err) == apierr.CodeProjectExportInProgress:
			busy++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || busy != n-1 {
		t.Fatalf("queued %d, rejected %d; want exactly 1 queued", ok, busy)
	}
	if len(h.pub.appended) != 1 {
		t.Errorf("stream messages = %d, want 1", len(h.pub.appended))
	}
}

func TestCleanupExpired_SweepsFailedExports(t *testing.T) {
	h := newHarness(1)
	h.tasks.listErr = errors.New("db down")
	e, _ := h.svc.RequestExport(context.Background(), h.pid, uuid.New())
	if err := h.svc.Execute(context.Background(), e.ID); err == nil {
		t.Fatal("expected the failure to be returned")
	}
	failed, _ := h.repo.FindByID(context.Background(), e.ID)
	if failed.Status != exportdom.StatusFailed || failed.ExpiresAt == nil {
		t.Fatalf("a failed export must get an expiry, got %+v", failed)
	}

	if n, _ := h.svc.CleanupExpired(context.Background()); n != 0 {
		t.Fatalf("a fresh failure must stay visible, swept %d", n)
	}
	h.now = h.now.Add(exportdom.RetentionPeriod + time.Hour)
	if n, err := h.svc.CleanupExpired(context.Background()); err != nil || n != 1 {
		t.Fatalf("cleanup n=%d err=%v, want the failed row swept", n, err)
	}
	if _, err := h.repo.FindByID(context.Background(), e.ID); !errors.Is(err, exportdom.ErrNotFound) {
		t.Error("failed row was not deleted")
	}
}

func TestExecute_RecordsFailureEvenWhenRunContextIsDone(t *testing.T) {
	h := newHarness(3)
	e, _ := h.svc.RequestExport(context.Background(), h.pid, uuid.New())
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the run timed out or was shut down
	if err := h.svc.Execute(ctx, e.ID); err == nil {
		t.Fatal("expected an error")
	}
	got, _ := h.repo.FindByID(context.Background(), e.ID)
	if got.Status != exportdom.StatusFailed {
		t.Fatalf("status = %s, want failed (it would block new requests as processing)", got.Status)
	}
}

func TestExecute_ClaimErrorIsDistinguishableAndLeavesRowPending(t *testing.T) {
	h := newHarness(1)
	e, _ := h.svc.RequestExport(context.Background(), h.pid, uuid.New())
	h.repo.claimErr = errors.New("connection refused")
	err := h.svc.Execute(context.Background(), e.ID)
	if !errors.Is(err, ErrClaim) {
		t.Fatalf("err = %v, want ErrClaim", err)
	}
	got, _ := h.repo.FindByID(context.Background(), e.ID)
	if got.Status != exportdom.StatusPending {
		t.Errorf("status = %s, want pending so a replay can run it", got.Status)
	}
}

func TestCapWriter_RefusesToGrowPastLimit(t *testing.T) {
	cw := &capWriter{buf: &bytes.Buffer{}, limit: 4}
	if _, err := cw.Write([]byte("abcd")); err != nil {
		t.Fatal(err)
	}
	if _, err := cw.Write([]byte("e")); !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("err = %v, want ErrArchiveTooLarge", err)
	}
}

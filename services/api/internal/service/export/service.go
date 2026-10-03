// Package exportsvc implements project exports (a zip of the tasks, their
// comments and activities, and the documentation): the API-facing request/list/
// download operations and the worker-side Execute that builds the file.
package exportsvc

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
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

const (
	// listLimit caps how many past exports the list endpoint returns.
	listLimit = 20
	// pageSize is how many tasks Execute reads per query.
	pageSize = 500
	// downloadTTL is how long a presigned download URL stays valid.
	downloadTTL = 10 * time.Minute
	// activityPageSize is how many activity entries are read per query.
	activityPageSize = 500
	// archiveContentType is the generated file's content type.
	archiveContentType = "application/zip"
)

// ErrorMessageGeneric is what a failed export records. The real cause is only
// logged: it can carry internal detail (storage endpoints, SQL) that the
// requesting user has no business seeing.
const ErrorMessageGeneric = "The export could not be generated. Please try again."

// Service implements exportdom.Service and runs queued exports for the worker.
type Service struct {
	repo      exportdom.Repository
	projects  projectdom.Repository
	members   projectdom.MemberRepository
	tasks     taskdom.Repository
	sprints   sprintdom.SprintRepository
	docs      docdom.Repository
	activity  activitydom.Repository
	store     storage.Client
	bucket    string
	publisher events.Publisher
	log       *slog.Logger
	now       func() time.Time
	// publicURL is the app's origin, used for links in exported documents.
	publicURL string
}

var _ exportdom.Service = (*Service)(nil)

// New returns a Service. publisher is used to queue exports on
// events.StreamProjectExports.
func New(
	repo exportdom.Repository,
	projects projectdom.Repository,
	members projectdom.MemberRepository,
	tasks taskdom.Repository,
	sprints sprintdom.SprintRepository,
	docs docdom.Repository,
	activity activitydom.Repository,
	store storage.Client,
	bucket string,
	publisher events.Publisher,
	log *slog.Logger,
) *Service {
	return &Service{
		repo: repo, projects: projects, members: members, tasks: tasks, sprints: sprints,
		docs: docs, activity: activity,
		store: store, bucket: bucket, publisher: publisher, log: log, now: time.Now,
	}
}

// WithPublicURL sets the app's public origin, which exported documents use
// for links to in-app content (annotation cards).
func (s *Service) WithPublicURL(url string) *Service {
	s.publicURL = url
	return s
}

// RequestExport implements exportdom.Service.
func (s *Service) RequestExport(ctx context.Context, projectID, requestedBy uuid.UUID) (*exportdom.ProjectExport, error) {
	now := s.now()
	e := &exportdom.ProjectExport{
		ID:          uuid.New(),
		ProjectID:   projectID,
		RequestedBy: &requestedBy,
		Kind:        exportdom.KindProjectArchive,
		Status:      exportdom.StatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	created, err := s.repo.CreateIfIdle(ctx, e, now.Add(-exportdom.StaleAfter))
	if err != nil {
		return nil, err
	}
	if !created {
		return nil, apierr.New(apierr.CodeProjectExportInProgress, "an export is already in progress for this project")
	}

	payload := map[string]string{"export_id": e.ID.String(), "project_id": projectID.String()}
	if err := s.publisher.Append(ctx, events.StreamProjectExports, events.TopicProjectExportRequested, payload); err != nil {
		// Nothing will ever pick this row up: fail it now so it neither
		// blocks the next request nor sits "pending" forever.
		s.log.Error("project export: queue failed", "export_id", e.ID, "err", err)
		if ferr := s.repo.MarkFailed(ctx, e.ID, ErrorMessageGeneric, s.failedExpiry()); ferr != nil {
			s.log.Error("project export: mark failed after queue error", "export_id", e.ID, "err", ferr)
		}
		return nil, fmt.Errorf("export: queue: %w", err)
	}
	return e, nil
}

// List implements exportdom.Service.
func (s *Service) List(ctx context.Context, projectID uuid.UUID) ([]*exportdom.ProjectExport, error) {
	return s.repo.ListByProject(ctx, projectID, listLimit)
}

// Get implements exportdom.Service. An export that belongs to a different
// project reads as not found, so IDs cannot be probed across projects.
func (s *Service) Get(ctx context.Context, projectID, exportID uuid.UUID) (*exportdom.ProjectExport, error) {
	e, err := s.repo.FindByID(ctx, exportID)
	if err != nil {
		if errors.Is(err, exportdom.ErrNotFound) {
			return nil, apierr.New(apierr.CodeProjectExportNotFound, "export not found")
		}
		return nil, err
	}
	if e.ProjectID != projectID {
		return nil, apierr.New(apierr.CodeProjectExportNotFound, "export not found")
	}
	return e, nil
}

// DownloadURL implements exportdom.Service.
func (s *Service) DownloadURL(ctx context.Context, projectID, exportID uuid.UUID) (string, time.Duration, error) {
	e, err := s.Get(ctx, projectID, exportID)
	if err != nil {
		return "", 0, err
	}
	if e.Status != exportdom.StatusCompleted || e.FileKey == nil || e.FileName == nil {
		return "", 0, apierr.New(apierr.CodeProjectExportNotReady, "export file is not ready")
	}
	if e.Expired(s.now()) {
		return "", 0, apierr.New(apierr.CodeProjectExportExpired, "export has expired")
	}
	disposition := fmt.Sprintf(`attachment; filename="%s"`, *e.FileName)
	url, err := s.store.PresignGetObject(ctx, s.bucket, *e.FileKey, downloadTTL, disposition)
	if err != nil {
		return "", 0, fmt.Errorf("export: presign download: %w", err)
	}
	return url, downloadTTL, nil
}

// Execute runs one queued export end to end. It is called by the worker, never
// from a request. Errors that are the export's own failure (bad data, storage
// down) are recorded on the row and returned nil-or-error only for the
// caller's logging; the stream message should be acked either way.
func (s *Service) Execute(ctx context.Context, exportID uuid.UUID) error {
	claimed, err := s.repo.Claim(ctx, exportID)
	if err != nil {
		return err
	}
	if !claimed {
		// Redelivered, already running elsewhere, or deleted — nothing to do.
		return nil
	}

	e, err := s.repo.FindByID(ctx, exportID)
	if err != nil {
		return s.fail(ctx, exportID, fmt.Errorf("load export: %w", err))
	}
	if e.Kind != exportdom.KindProjectArchive {
		return s.fail(ctx, exportID, fmt.Errorf("unsupported export kind %q", e.Kind))
	}

	data, rows, name, err := s.buildArchive(ctx, e.ProjectID)
	if err != nil {
		return s.fail(ctx, exportID, err)
	}

	key := fmt.Sprintf("exports/projects/%s/%s/%s", e.ProjectID, e.ID, name)
	if err := s.store.PutObject(ctx, s.bucket, key, archiveContentType, data); err != nil {
		return s.fail(ctx, exportID, fmt.Errorf("upload export: %w", err))
	}
	expires := s.now().Add(exportdom.RetentionPeriod)
	if err := s.repo.MarkCompleted(ctx, exportID, key, name, int64(len(data)), rows, expires); err != nil {
		// The file is orphaned if we can't record it; remove it.
		if derr := s.store.DeleteObject(ctx, s.bucket, key); derr != nil {
			s.log.Warn("project export: delete orphaned object", "key", key, "err", derr)
		}
		return s.fail(ctx, exportID, fmt.Errorf("record completion: %w", err))
	}
	s.log.Info("project export: completed", "export_id", exportID, "project_id", e.ProjectID, "rows", rows, "bytes", len(data))
	return nil
}

// failedExpiry is when a failed export's row is swept: failures stay visible for
// the same retention as a completed export's file, then go.
func (s *Service) failedExpiry() time.Time { return s.now().Add(exportdom.RetentionPeriod) }

func (s *Service) fail(ctx context.Context, id uuid.UUID, cause error) error {
	s.log.Error("project export: failed", "export_id", id, "err", cause)
	if err := s.repo.MarkFailed(ctx, id, ErrorMessageGeneric, s.failedExpiry()); err != nil {
		s.log.Error("project export: mark failed", "export_id", id, "err", err)
	}
	return cause
}

// CleanupExpired deletes the files and rows of exports past their retention,
// and fails exports stuck in an active state (their worker died), so they stop
// blocking new requests. Returns how many rows it removed.
func (s *Service) CleanupExpired(ctx context.Context) (int, error) {
	now := s.now()
	stale, err := s.repo.ListStale(ctx, now.Add(-exportdom.StaleAfter), 100)
	if err != nil {
		return 0, err
	}
	for _, e := range stale {
		if err := s.repo.MarkFailed(ctx, e.ID, ErrorMessageGeneric, s.failedExpiry()); err != nil {
			s.log.Warn("project export: fail stale export", "export_id", e.ID, "err", err)
		}
	}

	expired, err := s.repo.ListExpired(ctx, now, 100)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, e := range expired {
		if e.FileKey != nil {
			if err := s.store.DeleteObject(ctx, s.bucket, *e.FileKey); err != nil {
				// Keep the row so the next sweep retries the delete.
				s.log.Warn("project export: delete expired object", "export_id", e.ID, "err", err)
				continue
			}
		}
		if err := s.repo.Delete(ctx, e.ID); err != nil {
			s.log.Warn("project export: delete expired row", "export_id", e.ID, "err", err)
			continue
		}
		removed++
	}
	return removed, nil
}

// buildArchive builds the export zip:
//
//	tasks.csv            every task, IDs resolved to names
//	task-comments.csv    every task comment
//	task-activities.csv  every other task activity (status changes, edits, ...)
//	docs/...             each document as Markdown, in its folder tree
//
// Everything streams into the zip entry by entry; the tasks, comments and
// activities are read page by page so no query holds a whole table. It returns
// the zip bytes, the number of tasks exported and the file name.
func (s *Service) buildArchive(ctx context.Context, projectID uuid.UUID) (data []byte, tasks int, fileName string, err error) {
	project, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		return nil, 0, "", fmt.Errorf("load project: %w", err)
	}
	look, err := s.loadLookups(ctx, project)
	if err != nil {
		return nil, 0, "", err
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	modified := s.now()
	entry := func(name string) (io.Writer, error) {
		return zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: modified})
	}

	// tasks.csv first: it fills look.taskNumbers, which the comments and
	// activities files use to show each entry's task key.
	w, err := entry("tasks.csv")
	if err != nil {
		return nil, 0, "", fmt.Errorf("create tasks.csv: %w", err)
	}
	if tasks, err = s.writeTasks(ctx, w, project.ID, look); err != nil {
		return nil, 0, "", err
	}

	if w, err = entry("task-comments.csv"); err != nil {
		return nil, 0, "", fmt.Errorf("create task-comments.csv: %w", err)
	}
	if err := s.writeTaskActivity(ctx, w, project.ID, look, true); err != nil {
		return nil, 0, "", err
	}

	if w, err = entry("task-activities.csv"); err != nil {
		return nil, 0, "", fmt.Errorf("create task-activities.csv: %w", err)
	}
	if err := s.writeTaskActivity(ctx, w, project.ID, look, false); err != nil {
		return nil, 0, "", err
	}

	if err := s.writeDocs(ctx, entry, project.ID); err != nil {
		return nil, 0, "", err
	}

	if err := zw.Close(); err != nil {
		return nil, 0, "", fmt.Errorf("close zip: %w", err)
	}
	return buf.Bytes(), tasks, exportFileName(project.Name, s.now()), nil
}

// writeTasks pages through every task (keyset cursor) into the tasks CSV and
// returns how many it wrote.
func (s *Service) writeTasks(ctx context.Context, out io.Writer, projectID uuid.UUID, look *lookups) (int, error) {
	cw, err := newCSVWriter(out, look, tasksHeader(look.fields))
	if err != nil {
		return 0, err
	}
	var (
		cursor *string
		sort   taskdom.TaskSort
	)
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		page, hasMore, err := s.tasks.ListTasks(ctx, projectID, taskdom.TaskFilter{CursorAfter: cursor}, pageSize, sort)
		if err != nil {
			return 0, fmt.Errorf("list tasks: %w", err)
		}
		if err := s.resolveParents(ctx, look, page); err != nil {
			return 0, err
		}
		for _, t := range page {
			if err := cw.writeTask(t); err != nil {
				return 0, err
			}
		}
		if !hasMore || len(page) == 0 {
			break
		}
		c := taskdom.EncodeTaskCursor(page[len(page)-1], sort)
		cursor = &c
	}
	if err := cw.finish(); err != nil {
		return 0, err
	}
	return cw.rows, nil
}

// writeTaskActivity pages through the project's task activity log, newest
// first, writing either the comments (comments=true) or every other entry
// (comments=false) into its CSV.
func (s *Service) writeTaskActivity(ctx context.Context, out io.Writer, projectID uuid.UUID, look *lookups, comments bool) error {
	header := activitiesHeader
	if comments {
		header = commentsHeader
	}
	cw, err := newCSVWriter(out, look, header)
	if err != nil {
		return err
	}
	f := activitydom.ListFilter{ProjectID: projectID, EntityTypes: []string{string(events.EntityTask)}}
	if comments {
		f.ActivityTypes = []string{activitydom.TypeComment}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, hasMore, err := s.activity.List(ctx, f, activityPageSize)
		if err != nil {
			return fmt.Errorf("list task activity: %w", err)
		}
		for _, a := range page {
			isComment := a.ActivityType == activitydom.TypeComment
			if isComment != comments {
				continue
			}
			if comments {
				err = cw.writeComment(a)
			} else {
				err = cw.writeActivity(a)
			}
			if err != nil {
				return err
			}
		}
		if !hasMore || len(page) == 0 {
			break
		}
		f.Cursor = &activitydom.Cursor{CreatedAt: page[len(page)-1].CreatedAt, ID: page[len(page)-1].ID}
	}
	return cw.finish()
}

// writeDocs adds each document as a Markdown file under docs/.
func (s *Service) writeDocs(ctx context.Context, entry func(string) (io.Writer, error), projectID uuid.UUID) error {
	folders, err := s.docs.ListFolders(ctx, projectID)
	if err != nil {
		return fmt.Errorf("list doc folders: %w", err)
	}
	docs, _, err := s.docs.ListDocuments(ctx, projectID, nil, nil, nil, nil)
	if err != nil {
		return fmt.Errorf("list docs: %w", err)
	}
	for _, f := range buildDocFiles(folders, docs, s.publicURL) {
		w, err := entry(f.Path)
		if err != nil {
			return fmt.Errorf("create %s: %w", f.Path, err)
		}
		if _, err := io.WriteString(w, f.Body); err != nil {
			return fmt.Errorf("write %s: %w", f.Path, err)
		}
	}
	return nil
}

// resolveParents makes sure every parent referenced by a page is in
// look.taskNumbers. Tasks come back oldest first so a parent has almost always
// been written already; the rare parent created after its child is fetched.
func (s *Service) resolveParents(ctx context.Context, look *lookups, page []*taskdom.Task) error {
	for _, t := range page {
		look.taskNumbers[t.ID] = t.TaskNumber
	}
	for _, t := range page {
		if t.ParentTaskID == nil {
			continue
		}
		if _, ok := look.taskNumbers[*t.ParentTaskID]; ok {
			continue
		}
		parent, err := s.tasks.FindTaskByID(ctx, *t.ParentTaskID)
		if err != nil {
			// A parent that no longer exists just leaves the cell empty.
			continue
		}
		look.taskNumbers[parent.ID] = parent.TaskNumber
	}
	return nil
}

func (s *Service) loadLookups(ctx context.Context, project *projectdom.Project) (*lookups, error) {
	statuses, err := s.tasks.ListTaskStatuses(ctx, project.ID)
	if err != nil {
		return nil, fmt.Errorf("list statuses: %w", err)
	}
	types, err := s.tasks.ListTaskTypes(ctx, project.ID)
	if err != nil {
		return nil, fmt.Errorf("list task types: %w", err)
	}
	fields, err := s.tasks.ListCustomFieldDefinitions(ctx, project.ID)
	if err != nil {
		return nil, fmt.Errorf("list custom fields: %w", err)
	}
	sprints, err := s.sprints.ListSprints(ctx, project.ID)
	if err != nil {
		return nil, fmt.Errorf("list sprints: %w", err)
	}
	members, err := s.members.ListMembers(ctx, project.ID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}

	l := &lookups{
		taskIDPrefix: project.TaskIDPrefix,
		statuses:     make(map[uuid.UUID]*taskdom.TaskStatus, len(statuses)),
		types:        make(map[uuid.UUID]*taskdom.TaskType, len(types)),
		sprints:      make(map[uuid.UUID]string, len(sprints)),
		members:      make(map[uuid.UUID]string, len(members)),
		taskNumbers:  make(map[uuid.UUID]int64),
		fields:       fields,
	}
	for _, st := range statuses {
		l.statuses[st.ID] = st
	}
	for _, ty := range types {
		l.types[ty.ID] = ty
	}
	for _, sp := range sprints {
		l.sprints[sp.ID] = sp.Name
	}
	for _, m := range members {
		l.members[m.ID] = m.DisplayName()
	}
	return l, nil
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// exportFileName builds "<project>-export-<date>.zip" with the project name
// reduced to characters that are safe in a Content-Disposition header and an
// object key.
func exportFileName(projectName string, now time.Time) string {
	base := strings.Trim(unsafeFileChars.ReplaceAllString(projectName, "-"), "-.")
	if base == "" {
		base = "project"
	}
	if len(base) > 60 {
		base = strings.Trim(base[:60], "-.")
	}
	return fmt.Sprintf("%s-export-%s.zip", base, now.UTC().Format("2006-01-02"))
}

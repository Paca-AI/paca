package activitysvc_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	activitydom "github.com/Paca-AI/api/internal/domain/activity"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/events"
	activitysvc "github.com/Paca-AI/api/internal/service/activity"
)

var (
	errNotFound     = errors.New("not found")
	errForbidden    = errors.New("forbidden")
	errNotAComment  = errors.New("not a comment")
	errInvalid      = errors.New("invalid")
	errUnidentified = errors.New("unidentified")
)

var testKind = activitysvc.CommentKind{
	EntityType:           events.EntityTask,
	IDKey:                "task_id",
	TopicAdded:           "task.comment.added",
	TopicUpdated:         "task.comment.updated",
	TopicDeleted:         "task.comment.deleted",
	ErrNotFound:          errNotFound,
	ErrForbidden:         errForbidden,
	ErrNotAComment:       errNotAComment,
	ErrContentInvalid:    errInvalid,
	ErrActorUnidentified: errUnidentified,
}

// membersByUser resolves each user to a member with the same ID.
type membersByUser struct{}

func (membersByUser) FindMemberByActor(_ context.Context, _, actorID uuid.UUID, _ *uuid.UUID) (*projectdom.ProjectMember, error) {
	return &projectdom.ProjectMember{ID: actorID}, nil
}

// recorded captures every entry recorded.
type recorded struct{ entries []activitysvc.Entry }

func (r *recorded) Record(_ context.Context, e activitysvc.Entry) { r.entries = append(r.entries, e) }

var content = json.RawMessage(`[{"type":"paragraph","content":[{"type":"text","text":"hi"}]}]`)

func newService() (*activitysvc.Service, *activitysvc.MemoryRepository, *recorded) {
	repo := activitysvc.NewMemoryRepository()
	rec := &recorded{}
	return activitysvc.New(repo, membersByUser{}, rec), repo, rec
}

func addComment(t *testing.T, svc *activitysvc.Service, projectID, author uuid.UUID) *activitydom.Activity {
	t.Helper()
	a, _, err := svc.AddComment(context.Background(), testKind, activitysvc.CommentInput{
		ProjectID: projectID, EntityID: uuid.New(), ActorID: author, Content: content,
	})
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	return a
}

func TestAddComment_StoresAndPublishes(t *testing.T) {
	svc, repo, rec := newService()
	projectID, author := uuid.New(), uuid.New()

	a := addComment(t, svc, projectID, author)

	stored, err := repo.FindByID(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("comment not stored: %v", err)
	}
	if stored.ActivityType != activitydom.TypeComment || stored.Origin != string(events.OriginUser) {
		t.Errorf("unexpected stored comment: %+v", stored)
	}
	if len(rec.entries) != 1 || rec.entries[0].Topic != testKind.TopicAdded || !rec.entries[0].Plugins {
		t.Fatalf("expected one plugin-visible %q entry, got %+v", testKind.TopicAdded, rec.entries)
	}
	payload := rec.entries[0].Payload.(map[string]any)
	if payload["id"] != a.ID || payload["task_id"] != a.EntityIDOrNil() {
		t.Errorf("payload must carry the comment id and task_id, got %v", payload)
	}
}

func TestAddComment_InvalidContent(t *testing.T) {
	svc, _, _ := newService()
	for _, c := range []string{``, `[]`, `null`, `"text"`, `{"text":"  "}`} {
		_, _, err := svc.AddComment(context.Background(), testKind, activitysvc.CommentInput{
			ProjectID: uuid.New(), EntityID: uuid.New(), ActorID: uuid.New(), Content: json.RawMessage(c),
		})
		if !errors.Is(err, errInvalid) {
			t.Errorf("content %q: expected the kind's invalid-content error, got %v", c, err)
		}
	}
}

func TestUpdateComment_OnlyAuthor(t *testing.T) {
	svc, _, _ := newService()
	projectID, author := uuid.New(), uuid.New()
	a := addComment(t, svc, projectID, author)

	if _, err := svc.UpdateComment(context.Background(), testKind, a.ID, projectID, uuid.New(), nil, content); !errors.Is(err, errForbidden) {
		t.Fatalf("expected forbidden for a non-author, got %v", err)
	}
	if _, err := svc.UpdateComment(context.Background(), testKind, a.ID, projectID, author, nil, content); err != nil {
		t.Fatalf("author update failed: %v", err)
	}
}

func TestComment_OtherProjectOrKindIsNotFound(t *testing.T) {
	svc, _, _ := newService()
	projectID, author := uuid.New(), uuid.New()
	a := addComment(t, svc, projectID, author)

	if err := svc.DeleteComment(context.Background(), testKind, a.ID, uuid.New(), author, nil); !errors.Is(err, errNotFound) {
		t.Errorf("cross-project delete: expected not found, got %v", err)
	}
	docKind := testKind
	docKind.EntityType = events.EntityDoc
	if err := svc.DeleteComment(context.Background(), docKind, a.ID, projectID, author, nil); !errors.Is(err, errNotFound) {
		t.Errorf("a task comment addressed as a doc comment: expected not found, got %v", err)
	}
	if err := svc.DeleteComment(context.Background(), testKind, uuid.New(), projectID, author, nil); !errors.Is(err, errNotFound) {
		t.Errorf("unknown id: expected not found, got %v", err)
	}
}

func TestDeleteComment_ThenEditIsNotFound(t *testing.T) {
	svc, repo, _ := newService()
	projectID, author := uuid.New(), uuid.New()
	a := addComment(t, svc, projectID, author)

	if err := svc.DeleteComment(context.Background(), testKind, a.ID, projectID, author, nil); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if items, _ := repo.ListForEntity(context.Background(), "task", a.EntityIDOrNil()); len(items) != 0 {
		t.Errorf("deleted comment still on the timeline")
	}
	if _, err := svc.UpdateComment(context.Background(), testKind, a.ID, projectID, author, nil, content); !errors.Is(err, errNotFound) {
		t.Errorf("editing a deleted comment: expected not found, got %v", err)
	}
}

func TestNotAComment(t *testing.T) {
	svc, repo, _ := newService()
	projectID := uuid.New()
	entityID := uuid.New()
	id := uuid.New()
	_ = repo.Create(context.Background(), &activitydom.Activity{
		ID: id, ProjectID: projectID, EntityType: "task", EntityID: &entityID, ActivityType: "task.created",
	})
	if err := svc.DeleteComment(context.Background(), testKind, id, projectID, uuid.New(), nil); !errors.Is(err, errNotAComment) {
		t.Errorf("expected not-a-comment, got %v", err)
	}
}

func TestList_FiltersAndPaginates(t *testing.T) {
	svc, repo, _ := newService()
	projectID := uuid.New()
	for i := 0; i < 3; i++ {
		_ = repo.Create(context.Background(), &activitydom.Activity{ID: uuid.New(), ProjectID: projectID, EntityType: "sprint", ActivityType: "sprint.created"})
	}
	_ = repo.Create(context.Background(), &activitydom.Activity{ID: uuid.New(), ProjectID: projectID, EntityType: "task", ActivityType: "task.created"})
	_ = repo.Create(context.Background(), &activitydom.Activity{ID: uuid.New(), ProjectID: uuid.New(), EntityType: "sprint", ActivityType: "sprint.created"})

	page, more, err := svc.List(context.Background(), activitydom.ListFilter{ProjectID: projectID, EntityTypes: []string{"sprint"}}, 2)
	if err != nil || len(page) != 2 || !more {
		t.Fatalf("expected a full first page with more, got %d items more=%v err=%v", len(page), more, err)
	}
	cur, _ := activitydom.DecodeCursor(activitydom.EncodeCursor(page[1]))
	rest, more, _ := svc.List(context.Background(), activitydom.ListFilter{ProjectID: projectID, EntityTypes: []string{"sprint"}, Cursor: cur}, 2)
	if len(rest) != 1 || more {
		t.Fatalf("expected the last sprint entry on page two, got %d more=%v", len(rest), more)
	}
}

func TestDiscardAndNilRecorderAreSafe(t *testing.T) {
	activitysvc.Discard.Record(context.Background(), activitysvc.Entry{Topic: "x"})
	activitysvc.NewRecorder(nil).Record(context.Background(), activitysvc.Entry{Topic: "x"})
	activitysvc.New(activitysvc.NewMemoryRepository(), nil, nil).Record(context.Background(), activitysvc.Entry{Topic: "x"})
}

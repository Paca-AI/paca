package postgres

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

func TestTaskLinkRepository_ListTaskLinks_Pages(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewTaskLinkRepository(db)
	project := fx.project()
	base := time.Now().UTC().Add(-time.Hour)

	hub := uuid.New()
	pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1,$2,1,'hub')`, hub, project)
	var want []uuid.UUID // oldest first
	for i := 0; i < 5; i++ {
		other := uuid.New()
		pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1,$2,$3,$4)`, other, project, i+2, fmt.Sprintf("other-%d", i))
		link := uuid.New()
		src, dst := hub, other
		if i%2 == 1 { // the hub is the target of some links
			src, dst = other, hub
		}
		pgExec(t, db, `INSERT INTO task_links (id, source_task_id, target_task_id, link_type, created_at) VALUES ($1,$2,$3,'relates_to',$4)`,
			link, src, dst, base.Add(time.Duration(i)*time.Minute))
		want = append(want, link)
	}
	// A link to a deleted task is not listed or counted.
	gone := uuid.New()
	pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, title, deleted_at) VALUES ($1,$2,99,'gone',now())`, gone, project)
	pgExec(t, db, `INSERT INTO task_links (id, source_task_id, target_task_id, link_type) VALUES ($1,$2,$3,'blocks')`, uuid.New(), hub, gone)

	var got []uuid.UUID
	var cursor *string
	for range 10 {
		page, hasMore, total, err := repo.ListTaskLinks(t.Context(), hub, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		if total != 5 {
			t.Fatalf("total = %d, want 5", total)
		}
		for _, l := range page {
			got = append(got, l.ID)
			if l.LinkedTask == nil || l.DisplayLinkType == "" {
				t.Fatalf("link must carry its linked task and display type: %+v", l)
			}
		}
		if !hasMore {
			break
		}
		c := taskdom.EncodeLinkCursor(page[len(page)-1])
		cursor = &c
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("paged links = %v, want %v", got, want)
	}
	if all, err := repo.ListAllTaskLinks(t.Context(), hub); err != nil || len(all) != 5 {
		t.Fatalf("ListAllTaskLinks = %d err=%v", len(all), err)
	}
	bad := "!!"
	if _, _, _, err := repo.ListTaskLinks(t.Context(), hub, &bad, 2); !errors.Is(err, taskdom.ErrInvalidCursor) {
		t.Fatalf("bad cursor err = %v", err)
	}
}

func TestTaskLinkRepository_ListProjectTaskLinks_Pages(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewTaskLinkRepository(db)
	project := fx.project()
	base := time.Now().UTC().Add(-time.Hour)

	var tasks []uuid.UUID
	for i := 0; i < 6; i++ {
		id := uuid.New()
		pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1,$2,$3,$4)`, id, project, i+1, fmt.Sprintf("t-%d", i))
		tasks = append(tasks, id)
	}
	var want []uuid.UUID // oldest first
	for i := 0; i < 5; i++ {
		link := uuid.New()
		pgExec(t, db, `INSERT INTO task_links (id, source_task_id, target_task_id, link_type, created_at) VALUES ($1,$2,$3,'blocks',$4)`,
			link, tasks[i], tasks[i+1], base.Add(time.Duration(i)*time.Minute))
		want = append(want, link)
	}

	var got []uuid.UUID
	var cursor *string
	for range 10 {
		page, hasMore, err := repo.ListProjectTaskLinks(t.Context(), project, taskdom.LinkTypeBlocks, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range page {
			got = append(got, l.ID)
			if l.LinkType != taskdom.LinkTypeBlocks || l.LinkedTask != nil {
				t.Fatalf("unexpected link shape: %+v", l)
			}
		}
		if !hasMore {
			break
		}
		c := taskdom.EncodeLinkCursor(page[len(page)-1])
		cursor = &c
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("paged links = %v, want %v", got, want)
	}
	bad := "!!"
	if _, _, err := repo.ListProjectTaskLinks(t.Context(), project, taskdom.LinkTypeBlocks, &bad, 2); !errors.Is(err, taskdom.ErrInvalidCursor) {
		t.Fatalf("bad cursor err = %v", err)
	}
}

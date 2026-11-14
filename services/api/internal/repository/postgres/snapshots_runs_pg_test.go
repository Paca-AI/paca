package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

func TestListSnapshots_PagesNewestFirstWithoutBodies(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewDocumentRepository(f.db)
	docID := uuid.New()
	pgExec(t, f.db, `INSERT INTO documents (id, project_id, title) VALUES ($1,$2,'d')`, docID, f.p)
	for n := 1; n <= 5; n++ {
		pgExec(t, f.db, `INSERT INTO doc_snapshots (id, document_id, title, content, snapshot_number) VALUES ($1,$2,$3,'[{"type":"paragraph"}]'::jsonb,$4)`,
			uuid.New(), docID, fmt.Sprintf("v%d", n), n)
	}

	var got []int64
	var cursor *string
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("pagination did not terminate")
		}
		page, more, err := repo.ListSnapshots(ctx, docID, nil, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range page {
			got = append(got, s.SnapshotNumber)
			if len(s.Content) != 0 {
				t.Fatalf("snapshot %d list item carries a body", s.SnapshotNumber)
			}
		}
		if !more {
			break
		}
		c := docdom.EncodeSnapshotCursor(page[len(page)-1])
		cursor = &c
	}
	if fmt.Sprint(got) != "[5 4 3 2 1]" {
		t.Fatalf("snapshot order = %v, want newest first", got)
	}
	bad := "nope"
	if _, _, err := repo.ListSnapshots(ctx, docID, nil, &bad, 2); !errors.Is(err, docdom.ErrInvalidCursor) {
		t.Fatalf("bad cursor err = %v", err)
	}
}

func TestListRunsByAutomation_PagesNewestFirst(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewAutomationRepository(f.db)
	autoID, trigger := uuid.New(), uuid.New()
	pgExec(t, f.db, `INSERT INTO automations (id, project_id, name, status) VALUES ($1,$2,'a','active')`, autoID, f.p)
	pgExec(t, f.db, `INSERT INTO automation_nodes (id, automation_id, kind, type, config) VALUES ($1,$2,'trigger','status_changed','{}'::jsonb)`, trigger, autoID)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var ids []uuid.UUID
	// Two runs share a timestamp so the id tiebreak is exercised.
	for i, at := range []time.Duration{0, time.Minute, time.Minute, 2 * time.Minute, 3 * time.Minute} {
		id := uuid.New()
		ids = append(ids, id)
		pgExec(t, f.db, `INSERT INTO automation_runs (id, automation_id, trigger_node_id, status, started_at) VALUES ($1,$2,$3,'completed',$4)`,
			id, autoID, trigger, base.Add(at))
		_ = i
	}

	seen := map[uuid.UUID]bool{}
	var times []time.Time
	var cursor *string
	for pages := 0; ; pages++ {
		if pages > 6 {
			t.Fatal("pagination did not terminate")
		}
		page, more, err := repo.ListRunsByAutomation(ctx, autoID, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range page {
			if seen[r.ID] {
				t.Fatalf("run %s returned twice", r.ID)
			}
			seen[r.ID] = true
			times = append(times, r.StartedAt)
		}
		if !more {
			break
		}
		c := automationdom.EncodeRunCursor(page[len(page)-1])
		cursor = &c
	}
	if len(seen) != len(ids) {
		t.Fatalf("saw %d runs, want %d", len(seen), len(ids))
	}
	for i := 1; i < len(times); i++ {
		if times[i].After(times[i-1]) {
			t.Fatalf("not newest first: %v", times)
		}
	}
	bad := "nope"
	if _, _, err := repo.ListRunsByAutomation(ctx, autoID, &bad, 2); !errors.Is(err, automationdom.ErrInvalidCursor) {
		t.Fatalf("bad cursor err = %v", err)
	}
}

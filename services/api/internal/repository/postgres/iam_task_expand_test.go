package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// refinementFx is a project with two refinement-category statuses and a
// backlog one, plus a user whose role may write only refinement tasks.
type refinementFx struct {
	a                   *iam.Authorizer
	pr                  iam.Principal
	p                   uuid.UUID
	ref, ref2, backlog  uuid.UUID
	otherProjectStatus  uuid.UUID
	collection, project string
}

func newRefinementFx(t *testing.T, db *sqlx.DB) *refinementFx {
	t.Helper()
	fx := newIAMFx(t, db)
	f := &refinementFx{p: fx.project(), ref: uuid.New(), ref2: uuid.New(), backlog: uuid.New(), otherProjectStatus: uuid.New()}
	q := fx.project()
	pgExec(t, db, `INSERT INTO task_statuses (id, project_id, name, category) VALUES
		($1,$4,'Refinement','refinement'), ($2,$4,'Grooming','refinement'), ($3,$4,'Backlog','backlog')`, f.ref, f.ref2, f.backlog, f.p)
	pgExec(t, db, `INSERT INTO task_statuses (id, project_id, name, category) VALUES ($1,$2,'Refinement','refinement')`, f.otherProjectStatus, q)
	u := fx.user(false)
	fx.member(f.p, u, false)
	fx.attach(fx.role(`{"version":"1","statements":[{"effect":"Allow","actions":["tasks:write"],"resources":["project/*/task/*"],
		"conditions":{"In":{"task.status_category":["refinement"]}}}]}`), u, &f.p)
	f.a = iam.NewAuthorizer(NewIAMStore(db), iam.NewRegistry(), iam.NewAttributeSchema())
	for _, l := range NewIAMAttributeLoaders(db) {
		f.a.RegisterLoader(l)
	}
	f.pr = iam.Principal{Type: "user", ID: u.String()}
	f.project = "project/" + f.p.String()
	f.collection = f.project + "/task/*"
	return f
}

func TestTaskExpand_SetsCategoryFromStatus(t *testing.T) {
	f := newRefinementFx(t, newIAMPGTestDB(t))
	for name, c := range map[string]struct {
		status uuid.UUID
		want   bool
	}{"refinement": {f.ref, true}, "backlog": {f.backlog, false}} {
		r, err := f.a.AuthorizeCreate(context.Background(), f.pr, "tasks:write", f.collection, map[string][]string{"task.status_id": {c.status.String()}})
		if err != nil || r.Allowed != c.want {
			t.Errorf("create in %s: allowed=%v err=%v, want %v", name, r.Allowed, err, c.want)
		}
	}
}

func TestTaskExpand_ChangeUsesNewCategory(t *testing.T) {
	db := newIAMPGTestDB(t)
	f := newRefinementFx(t, db)
	task := uuid.New()
	pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, status_id, title) VALUES ($1,$2,1,$3,'draft')`, task, f.p, f.ref)
	res := f.project + "/task/" + task.String()
	for name, c := range map[string]struct {
		status uuid.UUID
		want   bool
	}{"to backlog": {f.backlog, false}, "to another refinement status": {f.ref2, true}} {
		r, err := f.a.AuthorizeChange(context.Background(), f.pr, "tasks:write", res, map[string][]string{"task.status_id": {c.status.String()}})
		if err != nil || r.Allowed != c.want {
			t.Errorf("move %s: allowed=%v err=%v, want %v", name, r.Allowed, err, c.want)
		}
	}
}

func TestTaskExpand_CrossProjectStatusDenied(t *testing.T) {
	f := newRefinementFx(t, newIAMPGTestDB(t))
	for _, status := range []string{f.otherProjectStatus.String(), uuid.NewString(), "not-a-uuid"} {
		r, err := f.a.AuthorizeCreate(context.Background(), f.pr, "tasks:write", f.collection, map[string][]string{"task.status_id": {status}})
		if err != nil || r.Allowed {
			t.Errorf("create with status %s: allowed=%v err=%v, want denied without error", status, r.Allowed, err)
		}
	}
}

func TestTaskExpand_NoStatusOnCreateDenied(t *testing.T) {
	f := newRefinementFx(t, newIAMPGTestDB(t))
	for name, attrs := range map[string]map[string][]string{"no attributes": {}, "null status": {"task.status_id": nil}} {
		r, err := f.a.AuthorizeCreate(context.Background(), f.pr, "tasks:write", f.collection, attrs)
		if err != nil || r.Allowed {
			t.Errorf("create with %s: allowed=%v err=%v, want denied", name, r.Allowed, err)
		}
	}
}

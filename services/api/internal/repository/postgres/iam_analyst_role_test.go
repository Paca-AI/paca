package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/bootstrap/defaultroles"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// The shipped Analyst role, attached to an agent over the real store and
// loaders: it creates and refines drafts in Refinement, cannot approve them,
// cannot touch other tasks, and reads code without being able to push.
func TestIAMAnalystRoleEndToEnd(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	p, q := fx.project(), fx.project()
	ref, ref2, backlog := uuid.New(), uuid.New(), uuid.New()
	pgExec(t, db, `INSERT INTO task_statuses (id, project_id, name, category) VALUES
		($1,$4,'Refinement','refinement'), ($2,$4,'Grooming','refinement'), ($3,$4,'Backlog','backlog')`, ref, ref2, backlog, p)
	draft, ready, foreign := uuid.New(), uuid.New(), uuid.New()
	pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, status_id, title) VALUES
		($1,$3,1,$4,'draft'), ($2,$3,2,$5,'ready')`, draft, ready, p, ref, backlog)
	qRef := uuid.New()
	pgExec(t, db, `INSERT INTO task_statuses (id, project_id, name, category) VALUES ($1,$2,'Refinement','refinement')`, qRef, q)
	pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, status_id, title) VALUES ($1,$2,1,$3,'other')`, foreign, q, qRef)

	agent := fx.agent(&p, false)
	fx.member(p, agent, false)
	var analyst []byte
	for _, r := range defaultroles.Instantiate(p) {
		if r.Name == "Analyst" {
			analyst = r.Policy
		}
	}
	if analyst == nil {
		t.Fatal("no Analyst template")
	}
	fx.attach(fx.role(string(analyst)), agent, &p)

	a := iam.NewAuthorizer(NewIAMStore(db), iam.NewRegistry(), iam.NewAttributeSchema())
	for _, l := range NewIAMAttributeLoaders(db) {
		a.RegisterLoader(l)
	}
	pr := iam.Principal{Type: "agent", ID: agent.String()}
	project := "project/" + p.String()
	task := func(id uuid.UUID) string { return project + "/task/" + id.String() }
	status := func(id uuid.UUID) map[string][]string { return map[string][]string{"task.status_id": {id.String()}} }
	ctx := context.Background()

	cases := []struct {
		name string
		call func() (iam.Result, error)
		want bool
	}{
		{"create in Refinement", func() (iam.Result, error) {
			return a.AuthorizeCreate(ctx, pr, "tasks:write", project+"/task/*", status(ref))
		}, true},
		{"create in Backlog", func() (iam.Result, error) {
			return a.AuthorizeCreate(ctx, pr, "tasks:write", project+"/task/*", status(backlog))
		}, false},
		{"create without status", func() (iam.Result, error) {
			return a.AuthorizeCreate(ctx, pr, "tasks:write", project+"/task/*", map[string][]string{})
		}, false},
		{"task create gate", func() (iam.Result, error) { return a.Authorize(ctx, pr, "tasks:write", project) }, true},
		{"edit Refinement task", func() (iam.Result, error) { return a.Authorize(ctx, pr, "tasks:write", task(draft)) }, true},
		{"edit Backlog task title", func() (iam.Result, error) { return a.Authorize(ctx, pr, "tasks:write", task(ready)) }, false},
		{"approve", func() (iam.Result, error) {
			return a.AuthorizeChange(ctx, pr, "tasks:write", task(draft), status(backlog))
		}, false},
		{"move between refinement statuses", func() (iam.Result, error) {
			return a.AuthorizeChange(ctx, pr, "tasks:write", task(draft), status(ref2))
		}, true},
		{"pull a Backlog task into Refinement", func() (iam.Result, error) {
			return a.AuthorizeChange(ctx, pr, "tasks:write", task(ready), status(ref))
		}, false},
		{"read any task", func() (iam.Result, error) { return a.Authorize(ctx, pr, "tasks:read", task(ready)) }, true},
		{"read credential", func() (iam.Result, error) { return a.Authorize(ctx, pr, "git:read", project) }, true},
		{"write credential", func() (iam.Result, error) { return a.Authorize(ctx, pr, "git:write", project) }, false},
		{"other project", func() (iam.Result, error) {
			return a.Authorize(ctx, pr, "tasks:write", "project/"+q.String()+"/task/"+foreign.String())
		}, false},
	}
	for _, c := range cases {
		r, err := c.call()
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if r.Allowed != c.want {
			t.Errorf("%s: allowed=%v, want %v", c.name, r.Allowed, c.want)
		}
	}
}

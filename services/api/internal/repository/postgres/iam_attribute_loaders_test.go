package postgres

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

func loadSorted(t *testing.T, l iam.AttributeLoader, id, project string, keys ...string) map[string][]string {
	t.Helper()
	got, err := l.Load(context.Background(), id, project, keys)
	if err != nil {
		t.Fatalf("%s.Load(%s): %v", l.Kind(), id, err)
	}
	for _, v := range got {
		sort.Strings(v)
	}
	return got
}

func TestIAMAttributeLoaders(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	loaders := map[string]iam.AttributeLoader{}
	for _, l := range NewIAMAttributeLoaders(db) {
		loaders[l.Kind()] = l
	}
	for _, k := range []string{"task", "doc", "agent", "environment", "conversation"} {
		if loaders[k] == nil {
			t.Fatalf("missing loader for %s", k)
		}
	}
	p := fx.project()

	t.Run("task", func(t *testing.T) {
		sprint, status, typ, task := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		pgExec(t, db, `INSERT INTO sprints (id, project_id, name) VALUES ($1, $2, 'S5')`, sprint, p)
		pgExec(t, db, `INSERT INTO task_statuses (id, project_id, name, category) VALUES ($1, $2, 'Todo', 'todo')`, status, p)
		pgExec(t, db, `INSERT INTO task_types (id, project_id, name) VALUES ($1, $2, 'Bug')`, typ, p)
		pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, task_type_id, status_id, sprint_id, title) VALUES ($1,$2,1,$3,$4,$5,'t')`,
			task, p, typ, status, sprint)
		u := fx.user(false)
		a := fx.agent(&p, false)
		um, am := fx.member(p, u, false, false), fx.member(p, a, true, false)
		gone := fx.member(p, fx.user(false), false, true)
		for _, m := range []uuid.UUID{um, am, gone} {
			pgExec(t, db, `INSERT INTO task_assignees (task_id, member_id) VALUES ($1, $2)`, task, m)
		}
		l := loaders["task"]
		got := loadSorted(t, l, task.String(), p.String(), "task.sprint_id", "task.status_id", "task.type_id", "task.assignee_id")
		wantAssign := []string{u.String(), a.String()}
		sort.Strings(wantAssign)
		want := map[string][]string{
			"task.sprint_id": {sprint.String()}, "task.status_id": {status.String()},
			"task.type_id": {typ.String()}, "task.assignee_id": wantAssign,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
		// only requested keys
		got = loadSorted(t, l, task.String(), p.String(), "task.sprint_id")
		if !reflect.DeepEqual(got, map[string][]string{"task.sprint_id": {sprint.String()}}) {
			t.Fatalf("subset: %v", got)
		}
		// NULL columns are absent
		t2 := uuid.New()
		pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1,$2,2,'t2')`, t2, p)
		if got = loadSorted(t, l, t2.String(), p.String(), "task.sprint_id", "task.assignee_id"); len(got) != 0 {
			t.Fatalf("null attrs must be absent: %v", got)
		}
		// missing / malformed ids
		for _, id := range []string{uuid.NewString(), "not-a-uuid"} {
			if _, err := l.Load(context.Background(), id, p.String(), []string{"task.sprint_id"}); !errors.Is(err, iam.ErrResourceNotInProject) {
				t.Fatalf("missing/malformed %s: %v", id, err)
			}
		}
		// unknown key ignored
		if got = loadSorted(t, l, task.String(), p.String(), "task.nope"); len(got) != 0 {
			t.Fatalf("unknown key: %v", got)
		}
		// soft-deleted task: absent
		pgExec(t, db, `UPDATE tasks SET deleted_at = NOW() WHERE id = $1`, task)
		if _, err := l.Load(context.Background(), task.String(), p.String(), []string{"task.sprint_id"}); !errors.Is(err, iam.ErrResourceNotInProject) {
			t.Fatalf("deleted: %v", err)
		}
	})

	t.Run("view", func(t *testing.T) {
		sprint, other, sv, bv := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		pgExec(t, db, `INSERT INTO sprints (id, project_id, name) VALUES ($1, $2, 'S5')`, sprint, p)
		pgExec(t, db, `INSERT INTO sprint_views (id, sprint_id, project_id, name, view_context) VALUES ($1,$2,$3,'v','sprint')`, sv, sprint, p)
		pgExec(t, db, `INSERT INTO sprint_views (id, project_id, name, view_context) VALUES ($1,$2,'b','backlog')`, bv, p)
		l := loaders["view"]
		if got := loadSorted(t, l, sv.String(), p.String(), "view.sprint_id"); !reflect.DeepEqual(got, map[string][]string{"view.sprint_id": {sprint.String()}}) {
			t.Fatalf("sprint view: %v", got)
		}
		// A backlog view has no sprint: the attribute is absent.
		if got := loadSorted(t, l, bv.String(), p.String(), "view.sprint_id"); len(got) != 0 {
			t.Fatalf("backlog view must have no view.sprint_id: %v", got)
		}
		// Another project's view is not found through this project.
		if _, err := l.Load(context.Background(), sv.String(), fx.project().String(), []string{"view.sprint_id"}); !errors.Is(err, iam.ErrResourceNotInProject) {
			t.Fatalf("other project: %v", err)
		}
		for _, id := range []string{other.String(), "not-a-uuid"} {
			if _, err := l.Load(context.Background(), id, p.String(), []string{"view.sprint_id"}); !errors.Is(err, iam.ErrResourceNotInProject) {
				t.Fatalf("missing/malformed %s: %v", id, err)
			}
		}
	})

	t.Run("doc ancestors", func(t *testing.T) {
		abc, b, c, other := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		pgExec(t, db, `INSERT INTO doc_folders (id, project_id, parent_id, name) VALUES ($1,$2,NULL,'ABC')`, abc, p)
		pgExec(t, db, `INSERT INTO doc_folders (id, project_id, parent_id, name) VALUES ($1,$2,$3,'B')`, b, p, abc)
		pgExec(t, db, `INSERT INTO doc_folders (id, project_id, parent_id, name) VALUES ($1,$2,$3,'C')`, c, p, b)
		pgExec(t, db, `INSERT INTO doc_folders (id, project_id, parent_id, name) VALUES ($1,$2,NULL,'Other')`, other, p)
		doc, rootDoc := uuid.New(), uuid.New()
		pgExec(t, db, `INSERT INTO documents (id, project_id, folder_id) VALUES ($1,$2,$3)`, doc, p, c)
		pgExec(t, db, `INSERT INTO documents (id, project_id) VALUES ($1,$2)`, rootDoc, p)
		l := loaders["doc"]
		got := loadSorted(t, l, doc.String(), p.String(), "doc.folder_id", "doc.ancestor_folder_ids")
		anc := []string{abc.String(), b.String(), c.String()}
		sort.Strings(anc)
		want := map[string][]string{"doc.folder_id": {c.String()}, "doc.ancestor_folder_ids": anc}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
		if got = loadSorted(t, l, doc.String(), p.String(), "doc.folder_id"); !reflect.DeepEqual(got, map[string][]string{"doc.folder_id": {c.String()}}) {
			t.Fatalf("subset %v", got)
		}
		if got = loadSorted(t, l, rootDoc.String(), p.String(), "doc.folder_id", "doc.ancestor_folder_ids"); len(got) != 0 {
			t.Fatalf("root doc: %v", got)
		}
		// Expand: folder_id alone derives the ancestors
		ex, ok := l.(iam.AttributeExpander)
		if !ok {
			t.Fatal("doc loader must implement AttributeExpander")
		}
		exp, err := ex.Expand(context.Background(), doc.String(), p.String(), map[string][]string{"doc.folder_id": {b.String()}})
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(exp["doc.ancestor_folder_ids"])
		wantB := []string{abc.String(), b.String()}
		sort.Strings(wantB)
		if !reflect.DeepEqual(exp["doc.ancestor_folder_ids"], wantB) || exp["doc.folder_id"][0] != b.String() {
			t.Fatalf("expand: %v", exp)
		}
		exp, _ = ex.Expand(context.Background(), doc.String(), p.String(), map[string][]string{"doc.folder_id": {""}})
		if len(exp["doc.folder_id"]) != 0 || len(exp["doc.ancestor_folder_ids"]) != 0 {
			t.Fatalf("root expand: %v", exp)
		}
		// cycle guard: a -> b -> a
		x, y, cd := uuid.New(), uuid.New(), uuid.New()
		pgExec(t, db, `INSERT INTO doc_folders (id, project_id, name) VALUES ($1,$2,'x'), ($3,$2,'y')`, x, p, y)
		pgExec(t, db, `UPDATE doc_folders SET parent_id = $2 WHERE id = $1`, x, y)
		pgExec(t, db, `UPDATE doc_folders SET parent_id = $2 WHERE id = $1`, y, x)
		pgExec(t, db, `INSERT INTO documents (id, project_id, folder_id) VALUES ($1,$2,$3)`, cd, p, x)
		got = loadSorted(t, l, cd.String(), p.String(), "doc.ancestor_folder_ids")
		wantCyc := []string{x.String(), y.String()}
		sort.Strings(wantCyc)
		if !reflect.DeepEqual(got["doc.ancestor_folder_ids"], wantCyc) {
			t.Fatalf("cycle: %v", got)
		}
	})

	t.Run("agent environment conversation", func(t *testing.T) {
		env := uuid.New()
		pgExec(t, db, `INSERT INTO environments (id, project_id, name, slug, backend, secret_key_encrypted) VALUES ($1,$2,'E','e','docker','x')`, env, p)
		ag := fx.agent(&p, false)
		pgExec(t, db, `UPDATE agents SET default_environment_id = $2 WHERE id = $1`, ag, env)
		if got := loadSorted(t, loaders["agent"], ag.String(), p.String(), "agent.environment_id"); !reflect.DeepEqual(got, map[string][]string{"agent.environment_id": {env.String()}}) {
			t.Fatalf("agent: %v", got)
		}
		ag2 := fx.agent(&p, false)
		if got := loadSorted(t, loaders["agent"], ag2.String(), p.String(), "agent.environment_id"); len(got) != 0 {
			t.Fatalf("agent without env: %v", got)
		}
		if got := loadSorted(t, loaders["environment"], env.String(), p.String(), "environment.type"); !reflect.DeepEqual(got, map[string][]string{"environment.type": {"docker"}}) {
			t.Fatalf("environment: %v", got)
		}
		conv := uuid.New()
		pgExec(t, db, `INSERT INTO agent_conversations (id, agent_id, project_id, trigger_type, triggered_by_member_id, environment_id)
			VALUES ($1,$2,$3,'chat_message',$4,$5)`, conv, ag, p, fx.member(p, fx.user(false), false, false), env)
		if got := loadSorted(t, loaders["conversation"], conv.String(), p.String(), "conversation.environment_id"); !reflect.DeepEqual(got, map[string][]string{"conversation.environment_id": {env.String()}}) {
			t.Fatalf("conversation: %v", got)
		}
	})
}

// End to end: the Postgres store and loaders behind the Authorizer.
func TestIAMAuthorizerEndToEnd(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	p := fx.project()
	u := fx.user(false)
	fx.member(p, u, false, false)
	s5, s6, t5, t6 := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	pgExec(t, db, `INSERT INTO sprints (id, project_id, name) VALUES ($1,$3,'S5'), ($2,$3,'S6')`, s5, s6, p)
	pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, sprint_id, title) VALUES ($1,$3,1,$4,'a'), ($2,$3,2,$5,'b')`, t5, t6, p, s5, s6)
	pol := `{"version":"1","statements":[{"effect":"Allow","actions":["tasks:read"],"resources":["project/*/task/*"],
		"conditions":{"StringEquals":{"task.sprint_id":["` + s5.String() + `"]}}}]}`
	fx.attach(fx.role(pol), "user", u, &p)

	store := NewIAMStore(db)
	a := iam.NewAuthorizer(store, iam.NewRegistry(), iam.NewAttributeSchema())
	for _, l := range NewIAMAttributeLoaders(db) {
		a.RegisterLoader(l)
	}
	pr := iam.Principal{Type: "user", ID: u.String()}
	ctx := context.Background()
	res, err := a.Authorize(ctx, pr, "tasks:read", "project/"+p.String()+"/task/"+t5.String())
	if err != nil || !res.Allowed {
		t.Fatalf("S5: %+v %v", res, err)
	}
	res, err = a.Authorize(ctx, pr, "tasks:read", "project/"+p.String()+"/task/"+t6.String())
	if err != nil || res.Allowed {
		t.Fatalf("S6: %+v %v", res, err)
	}
	n, err := a.ListScope(ctx, pr, "tasks:read", p.String(), "task")
	if err != nil || n.Unrestricted() || n.DeniesAll() {
		t.Fatalf("%+v %v", n, err)
	}
}

func TestIAMAuthorizeChangeDocMoveEndToEnd(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	p, u := fx.project(), fx.user(false)
	fx.member(p, u, false, false)
	abc, sub, out, doc := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	pgExec(t, db, `INSERT INTO doc_folders (id, project_id, name) VALUES ($1,$2,'ABC'), ($3,$2,'OUT')`, abc, p, out)
	pgExec(t, db, `INSERT INTO doc_folders (id, project_id, parent_id, name) VALUES ($1,$2,$3,'sub')`, sub, p, abc)
	pgExec(t, db, `INSERT INTO documents (id, project_id, folder_id) VALUES ($1,$2,$3)`, doc, p, abc)
	pol := `{"version":"1","statements":[{"effect":"Allow","actions":["docs:write"],"resources":["project/*/doc/*"],
		"conditions":{"In":{"doc.ancestor_folder_ids":["` + abc.String() + `"]}}}]}`
	fx.attach(fx.role(pol), "user", u, &p)
	a := iam.NewAuthorizer(NewIAMStore(db), iam.NewRegistry(), iam.NewAttributeSchema())
	for _, l := range NewIAMAttributeLoaders(db) {
		a.RegisterLoader(l)
	}
	pr := iam.Principal{Type: "user", ID: u.String()}
	res := "project/" + p.String() + "/doc/" + doc.String()
	r, err := a.AuthorizeChange(context.Background(), pr, "docs:write", res, map[string][]string{"doc.folder_id": {sub.String()}})
	if err != nil || !r.Allowed {
		t.Fatalf("within subtree: %+v %v", r, err)
	}
	r, err = a.AuthorizeChange(context.Background(), pr, "docs:write", res, map[string][]string{"doc.folder_id": {out.String()}})
	if err != nil || r.Allowed {
		t.Fatalf("outside subtree: %+v %v", r, err)
	}
	// Creating a document derives its ancestor folders from the folder named.
	coll := "project/" + p.String() + "/doc/*"
	for name, c := range map[string]struct {
		folder []string
		want   bool
	}{"in a subfolder": {[]string{sub.String()}, true}, "elsewhere": {[]string{out.String()}, false}, "at the root": {nil, false}, "unknown folder": {[]string{uuid.NewString()}, false}} {
		r, err := a.AuthorizeCreate(context.Background(), pr, "docs:write", coll, map[string][]string{"doc.folder_id": c.folder})
		if err != nil || r.Allowed != c.want {
			t.Fatalf("create %s: allowed=%v err=%v, want %v", name, r.Allowed, err, c.want)
		}
	}
}

// Cross-project ids must deny (never load another project's attributes), for
// every kind, both with a positive and a NEGATED condition on the Allow.
func TestIAMCrossProjectDenied(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	p1, p2 := fx.project(), fx.project()
	u := fx.user(false)
	fx.member(p1, u, false, false)

	// Entities that live in p2 (and global agent reachable from p2 only).
	env, task, doc, folder, conv := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	pgExec(t, db, `INSERT INTO environments (id, project_id, name, slug, backend, secret_key_encrypted) VALUES ($1,$2,'E','e','docker','x')`, env, p2)
	pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1,$2,1,'t')`, task, p2)
	pgExec(t, db, `INSERT INTO doc_folders (id, project_id, name) VALUES ($1,$2,'F')`, folder, p2)
	pgExec(t, db, `INSERT INTO documents (id, project_id, folder_id) VALUES ($1,$2,$3)`, doc, p2, folder)
	agP2 := fx.agent(&p2, false)
	globalAg := fx.agent(nil, false)
	memberP2 := fx.member(p2, globalAg, true, false)
	pgExec(t, db, `UPDATE agents SET default_environment_id = $2 WHERE id IN ($1, $3)`, agP2, env, globalAg)
	pgExec(t, db, `INSERT INTO agent_conversations (id, agent_id, project_id, trigger_type, triggered_by_member_id, environment_id)
		VALUES ($1,$2,$3,'chat_message',$4,$5)`, conv, agP2, p2, memberP2, env)

	type kindCase struct{ action, resKind, id, key, other string }
	cases := []kindCase{
		{"tasks:read", "task", task.String(), "task.sprint_id", "S"},
		{"docs:read", "doc", doc.String(), "doc.folder_id", "F"},
		{"docs:read", "doc", doc.String(), "doc.ancestor_folder_ids", "F"},
		{"agents:read", "agent", agP2.String(), "agent.environment_id", "E"},
		{"environments:read", "environment", env.String(), "environment.type", "kubernetes"},
		{"conversations:read", "conversation", conv.String(), "conversation.environment_id", "E"},
	}
	ctx := context.Background()
	for _, c := range cases {
		for name, cond := range map[string]string{
			"positive": `"In":{"` + c.key + `":["` + c.other + `"]}`,
			"negated":  `"NotIn":{"` + c.key + `":["` + c.other + `"]}`,
		} {
			t.Run(c.resKind+"/"+c.key+"/"+name, func(t *testing.T) {
				pol := `{"version":"1","statements":[{"effect":"Allow","actions":["` + c.action + `"],"resources":["project/*/` + c.resKind + `/*"],"conditions":{` + cond + `}}]}`
				role := fx.role(pol)
				fx.attach(role, "user", u, &p1)
				a := iam.NewAuthorizer(NewIAMStore(db), iam.NewRegistry(), iam.NewAttributeSchema())
				for _, l := range NewIAMAttributeLoaders(db) {
					a.RegisterLoader(l)
				}
				pr := iam.Principal{Type: "user", ID: u.String()}
				// The same id is NOT allowed through project p1's resource name.
				res, err := a.Authorize(ctx, pr, c.action, "project/"+p1.String()+"/"+c.resKind+"/"+c.id)
				if err != nil || res.Allowed {
					t.Fatalf("cross-project: %+v %v", res, err)
				}
				// Not found denies too.
				res, err = a.Authorize(ctx, pr, c.action, "project/"+p1.String()+"/"+c.resKind+"/"+uuid.NewString())
				if err != nil || res.Allowed {
					t.Fatalf("not found: %+v %v", res, err)
				}
				// Loader-level: sentinel, not an empty map.
				ld := loaderFor(db, c.resKind)
				if _, err := ld.Load(ctx, c.id, p1.String(), []string{c.key}); !errors.Is(err, iam.ErrResourceNotInProject) {
					t.Fatalf("loader: %v", err)
				}
				if _, err := ld.Load(ctx, c.id, p2.String(), []string{c.key}); err != nil {
					t.Fatalf("own-project load must work: %v", err)
				}
				pgExec(t, db, `DELETE FROM role_attachments WHERE role_id = $1`, role)
			})
		}
	}

	t.Run("expand with a cross-project folder denies", func(t *testing.T) {
		ld := loaderFor(db, "doc").(iam.AttributeExpander)
		if _, err := ld.Expand(ctx, doc.String(), p1.String(), map[string][]string{"doc.folder_id": {folder.String()}}); !errors.Is(err, iam.ErrResourceNotInProject) {
			t.Fatalf("got %v", err)
		}
		if _, err := ld.Expand(ctx, doc.String(), p1.String(), map[string][]string{"doc.folder_id": {uuid.NewString()}}); !errors.Is(err, iam.ErrResourceNotInProject) {
			t.Fatalf("unknown folder: %v", err)
		}
		if got, err := ld.Expand(ctx, doc.String(), p2.String(), map[string][]string{"doc.folder_id": {folder.String()}}); err != nil || len(got["doc.ancestor_folder_ids"]) != 1 {
			t.Fatalf("own project: %v %v", got, err)
		}
	})

	t.Run("doc ancestor walk stays in project", func(t *testing.T) {
		// parent folder in p1, child folder + doc in p2: ancestors must not include the p1 parent.
		parent, child, d := uuid.New(), uuid.New(), uuid.New()
		pgExec(t, db, `INSERT INTO doc_folders (id, project_id, name) VALUES ($1,$2,'parent')`, parent, p1)
		pgExec(t, db, `INSERT INTO doc_folders (id, project_id, parent_id, name) VALUES ($1,$2,$3,'child')`, child, p2, parent)
		pgExec(t, db, `INSERT INTO documents (id, project_id, folder_id) VALUES ($1,$2,$3)`, d, p2, child)
		got := loadSorted(t, loaderFor(db, "doc"), d.String(), p2.String(), "doc.ancestor_folder_ids")
		if !reflect.DeepEqual(got["doc.ancestor_folder_ids"], []string{child.String()}) {
			t.Fatalf("walk escaped the project: %v", got)
		}
	})

	t.Run("agent reachable through membership; platform agent without project", func(t *testing.T) {
		ld := loaderFor(db, "agent")
		if got := loadSorted(t, ld, globalAg.String(), p2.String(), "agent.environment_id"); len(got["agent.environment_id"]) != 1 {
			t.Fatalf("global agent via membership: %v", got)
		}
		if _, err := ld.Load(ctx, globalAg.String(), p1.String(), []string{"agent.environment_id"}); !errors.Is(err, iam.ErrResourceNotInProject) {
			t.Fatalf("global agent in non-member project: %v", err)
		}
		if got := loadSorted(t, ld, globalAg.String(), "", "agent.environment_id"); len(got["agent.environment_id"]) != 1 {
			t.Fatalf("platform-level agent/<id>: %v", got)
		}
		// soft-deleted membership no longer reaches the project
		pgExec(t, db, `UPDATE project_members SET deleted_at = NOW() WHERE id = $1`, memberP2)
		if _, err := ld.Load(ctx, globalAg.String(), p2.String(), []string{"agent.environment_id"}); !errors.Is(err, iam.ErrResourceNotInProject) {
			t.Fatalf("deleted membership: %v", err)
		}
	})

	t.Run("task assignee key is project bound", func(t *testing.T) {
		if _, err := loaderFor(db, "task").Load(ctx, task.String(), p1.String(), []string{"task.assignee_id"}); !errors.Is(err, iam.ErrResourceNotInProject) {
			t.Fatalf("got %v", err)
		}
	})
}

func loaderFor(db *sqlx.DB, kind string) iam.AttributeLoader {
	for _, l := range NewIAMAttributeLoaders(db) {
		if l.Kind() == kind {
			return l
		}
	}
	panic("no loader " + kind)
}

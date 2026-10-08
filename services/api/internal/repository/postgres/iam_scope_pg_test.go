package postgres

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	annotationdom "github.com/Paca-AI/api/internal/domain/annotation"
	docdom "github.com/Paca-AI/api/internal/domain/doc"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// scopeFx wires a project, a member and an Authorizer over the real schema.
type scopeFx struct {
	t    *testing.T
	db   *sqlx.DB
	fx   *iamFx
	p, u uuid.UUID
	auth *iam.Authorizer
	pr   iam.Principal
}

func newScopeFx(t *testing.T) *scopeFx {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	p, u := fx.project(), fx.user(false)
	fx.member(p, u, false, false)
	a := iam.NewAuthorizer(NewIAMStore(db), iam.NewRegistry(), iam.NewAttributeSchema())
	return &scopeFx{t: t, db: db, fx: fx, p: p, u: u, auth: a, pr: iam.Principal{Type: "user", ID: u.String()}}
}

// role attaches policy to the user in the project.
func (s *scopeFx) role(policy string) { s.fx.attach(s.fx.role(policy), "user", s.u, &s.p) }

// ctx returns a context carrying the user's scope for kind/action.
func (s *scopeFx) ctx(action, kind string) context.Context {
	n, err := s.auth.ListScope(context.Background(), s.pr, action, s.p.String(), kind)
	if err != nil {
		s.t.Fatal(err)
	}
	return iam.WithScope(context.Background(), kind, n)
}

func policyJSON(effect, action, resource, cond string) string {
	if cond != "" {
		cond = `,"conditions":` + cond
	}
	return fmt.Sprintf(`{"version":"1","statements":[{"effect":%q,"actions":[%q],"resources":[%q]%s}]}`, effect, action, resource, cond)
}

func sorted(ids []string) []string { sort.Strings(ids); return ids }

// The scenario the SQL translation exists for: a role limited to one sprint's
// tasks must page, count and sum over those tasks only, with full pages.
func TestScopedTaskListPaginatesOverAllowedTasksOnly(t *testing.T) {
	f := newScopeFx(t)
	s5, s6 := uuid.New(), uuid.New()
	pgExec(t, f.db, `INSERT INTO sprints (id, project_id, name) VALUES ($1,$3,'S5'), ($2,$3,'S6')`, s5, s6, f.p)
	var want []string
	for i := 1; i <= 23; i++ {
		id, sprint := uuid.New(), s6
		if i%3 == 0 { // every third task is in S5
			sprint = s5
			want = append(want, id.String())
		}
		pgExec(t, f.db, `INSERT INTO tasks (id, project_id, task_number, sprint_id, title, story_points) VALUES ($1,$2,$3,$4,'t',2)`, id, f.p, i, sprint)
	}
	f.role(policyJSON("Allow", "tasks:read", "project/*/task/*", `{"StringEquals":{"task.sprint_id":["`+s5.String()+`"]}}`))

	repo := NewTaskRepository(f.db)
	ctx := f.ctx("tasks:read", "task")

	var got []string
	filter := taskdom.TaskFilter{}
	for page := 0; ; page++ {
		tasks, more, err := repo.ListTasks(ctx, f.p, filter, 3, taskdom.TaskSort{})
		if err != nil {
			t.Fatal(err)
		}
		if more && len(tasks) != 3 {
			t.Fatalf("page %d has %d tasks but more pages follow: a page must be full of allowed tasks", page, len(tasks))
		}
		for _, tk := range tasks {
			got = append(got, tk.ID.String())
		}
		if !more {
			break
		}
		c := taskdom.EncodeTaskCursor(tasks[len(tasks)-1], taskdom.TaskSort{})
		filter.CursorAfter = &c
		if page > 20 {
			t.Fatal("pagination did not terminate")
		}
	}
	if fmt.Sprint(sorted(got)) != fmt.Sprint(sorted(want)) {
		t.Fatalf("listed %v, want exactly the S5 tasks %v", got, want)
	}
	if n, err := repo.CountTasks(ctx, f.p, taskdom.TaskFilter{}); err != nil || n != int64(len(want)) {
		t.Fatalf("count = %d (%v), want %d", n, err, len(want))
	}
	if sum, err := repo.SumTaskField(ctx, f.p, taskdom.TaskFilter{}, "story_points"); err != nil || sum != float64(2*len(want)) {
		t.Fatalf("sum = %v (%v), want %d", sum, err, 2*len(want))
	}
	// Without a scope (workers, internal callers) nothing is filtered.
	if n, _ := repo.CountTasks(context.Background(), f.p, taskdom.TaskFilter{}); n != 23 {
		t.Fatalf("unscoped count = %d, want 23", n)
	}
}

func TestScopedTaskListDenyAssigneeAndNoAccess(t *testing.T) {
	f := newScopeFx(t)
	other := f.fx.user(false)
	otherMember := f.fx.member(f.p, other, false, false)
	mine, theirs, unassigned := uuid.New(), uuid.New(), uuid.New()
	for i, id := range []uuid.UUID{mine, theirs, unassigned} {
		pgExec(t, f.db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1,$2,$3,'t')`, id, f.p, i+1)
	}
	pgExec(t, f.db, `INSERT INTO task_assignees (task_id, member_id) VALUES ($1,$2)`, theirs, otherMember)

	repo := NewTaskRepository(f.db)
	list := func(ctx context.Context) []string {
		tasks, _, err := repo.ListTasks(ctx, f.p, taskdom.TaskFilter{}, 50, taskdom.TaskSort{})
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, tk := range tasks {
			ids = append(ids, tk.ID.String())
		}
		return sorted(ids)
	}

	// No role at all: nothing, and no query is needed.
	if got := list(f.ctx("tasks:read", "task")); len(got) != 0 {
		t.Fatalf("no access listed %v", got)
	}

	// Allow everything, Deny tasks assigned to someone else (multi-valued attribute).
	f.fx.attach(f.fx.role(`{"version":"1","statements":[
		{"effect":"Allow","actions":["tasks:read"],"resources":["project/*"]},
		{"effect":"Deny","actions":["tasks:read"],"resources":["project/*/task/*"],
		 "conditions":{"In":{"task.assignee_id":["`+other.String()+`"]}}}]}`), "user", f.u, &f.p)
	got := list(f.ctx("tasks:read", "task"))
	if fmt.Sprint(got) != fmt.Sprint(sorted([]string{mine.String(), unassigned.String()})) {
		t.Fatalf("listed %v, want the unassigned tasks only", got)
	}
}

func TestScopedDocListUsesAncestorFolders(t *testing.T) {
	f := newScopeFx(t)
	abc, sub, out := uuid.New(), uuid.New(), uuid.New()
	pgExec(t, f.db, `INSERT INTO doc_folders (id, project_id, name) VALUES ($1,$2,'ABC'), ($3,$2,'OUT')`, abc, f.p, out)
	pgExec(t, f.db, `INSERT INTO doc_folders (id, project_id, parent_id, name) VALUES ($1,$2,$3,'sub')`, sub, f.p, abc)
	inABC, inSub, inOut, atRoot := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for i, d := range []struct {
		id     uuid.UUID
		folder any
	}{{inABC, abc}, {inSub, sub}, {inOut, out}, {atRoot, nil}} {
		pgExec(t, f.db, `INSERT INTO documents (id, project_id, folder_id, title, position) VALUES ($1,$2,$3,$4,$5)`, d.id, f.p, d.folder, fmt.Sprintf("d%d", i), i)
	}
	f.role(policyJSON("Allow", "docs:read", "project/*/doc/*", `{"In":{"doc.ancestor_folder_ids":["`+abc.String()+`"]}}`))

	repo := NewDocumentRepository(f.db)
	ctx := f.ctx("docs:read", "doc")
	limit := 1
	var got []string
	var cursor *string
	for i := 0; i < 5; i++ {
		docs, more, err := repo.ListDocuments(ctx, f.p, nil, nil, cursor, &limit)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range docs {
			got = append(got, d.ID.String())
		}
		if !more {
			break
		}
		c := docdom.EncodeDocumentCursor(docs[len(docs)-1])
		cursor = &c
	}
	if fmt.Sprint(sorted(got)) != fmt.Sprint(sorted([]string{inABC.String(), inSub.String()})) {
		t.Fatalf("listed %v, want the documents in ABC and its subfolder", got)
	}
}

func TestScopedAgentEnvironmentSprintLists(t *testing.T) {
	f := newScopeFx(t)
	a1, a2 := f.fx.agent(&f.p, false), f.fx.agent(&f.p, false)
	f.fx.member(f.p, a1, true, false)
	f.fx.member(f.p, a2, true, false)
	e1, e2 := uuid.New(), uuid.New()
	pgExec(t, f.db, `INSERT INTO environments (id, project_id, name, slug, backend, secret_key_encrypted) VALUES ($1,$3,'E1','e1','docker','x'), ($2,$3,'E2','e2','kubernetes','x')`, e1, e2, f.p)
	s1, s2 := uuid.New(), uuid.New()
	pgExec(t, f.db, `INSERT INTO sprints (id, project_id, name) VALUES ($1,$3,'S1'), ($2,$3,'S2')`, s1, s2, f.p)
	pgExec(t, f.db, `UPDATE agents SET default_environment_id = $2 WHERE id = $1`, a1, e1)

	// One role per kind: all agents except a2 by id, environments of type docker, sprint s2 only.
	f.role(`{"version":"1","statements":[
		{"effect":"Allow","actions":["agents:read"],"resources":["project/*"]},
		{"effect":"Deny","actions":["agents:read"],"resources":["project/*/agent/` + a2.String() + `"]},
		{"effect":"Allow","actions":["environments:read"],"resources":["project/*/environment/*"],"conditions":{"StringEquals":{"environment.type":"docker"}}},
		{"effect":"Allow","actions":["sprints:read"],"resources":["project/*/sprint/` + s2.String() + `"]}]}`)

	agents, err := NewAgentRepository(f.db).ListAgents(f.ctx("agents:read", "agent"), f.p, "")
	if err != nil || len(agents) != 1 || agents[0].ID != a1 {
		t.Fatalf("agents = %v (%v), want only a1", agents, err)
	}
	envs, err := NewEnvironmentRepository(f.db).ListEnvironments(f.ctx("environments:read", "environment"), f.p)
	if err != nil || len(envs) != 1 || envs[0].ID != e1 {
		t.Fatalf("environments = %v (%v), want only the docker one", envs, err)
	}
	sprints, err := NewSprintRepository(f.db).ListSprints(f.ctx("sprints:read", "sprint"), f.p)
	if err != nil || len(sprints) != 1 || sprints[0].ID != s2 {
		t.Fatalf("sprints = %v (%v), want only s2", sprints, err)
	}
	// A condition on an attribute with no SQL mapping must fail, never list everything.
	bad := iam.WithScope(context.Background(), "sprint", &iam.Node{Kind: iam.NodeCond, Key: "sprint.nope", Op: "In", Values: iam.ValueList{"x"}})
	if _, err := NewSprintRepository(f.db).ListSprints(bad, f.p); err == nil {
		t.Fatal("unmapped attribute must be an error")
	}
}

// Every attribute the schema declares for a kind has SQL for it.
func TestScopeColumnsCoverTheSchema(t *testing.T) {
	cols := map[string]scopeColumns{
		"task": taskScopeColumns, "doc": docScopeColumns, "agent": agentScopeColumns,
		"environment": environmentScopeColumns, "sprint": sprintScopeColumns,
		"view": viewScopeColumns, "workflow": workflowScopeColumns, "conversation": conversationScopeColumns,
		"annotation": annotationScopeColumns,
	}
	for _, def := range iam.NewAttributeSchema().Defs() {
		c, ok := cols[def.ResourceKind]
		if !ok {
			continue // kinds with no list query yet
		}
		if _, ok := c[def.Key]; !ok {
			t.Errorf("attribute %s has no SQL mapping for kind %s", def.Key, def.ResourceKind)
		}
	}
}

func TestScopedViewWorkflowConversationLists(t *testing.T) {
	f := newScopeFx(t)
	v1, v2, a1, a2 := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, v := range []uuid.UUID{v1, v2} {
		pgExec(t, f.db, `INSERT INTO sprint_views (id, project_id, name, view_type, config, position, view_context) VALUES ($1,$2,'v','table','{}',0,'backlog')`, v, f.p)
	}
	for _, a := range []uuid.UUID{a1, a2} {
		pgExec(t, f.db, `INSERT INTO automations (id, project_id, name, status) VALUES ($1,$2,'a','active')`, a, f.p)
	}
	env1, env2 := uuid.New(), uuid.New()
	pgExec(t, f.db, `INSERT INTO environments (id, project_id, name, slug, backend, secret_key_encrypted) VALUES ($1,$3,'E1','e1','docker','x'), ($2,$3,'E2','e2','docker','x')`, env1, env2, f.p)
	ag := f.fx.agent(&f.p, false)
	member := f.fx.member(f.p, f.fx.user(false), false, false)
	c1, c2 := uuid.New(), uuid.New()
	for c, env := range map[uuid.UUID]uuid.UUID{c1: env1, c2: env2} {
		pgExec(t, f.db, `INSERT INTO agent_conversations (id, agent_id, project_id, trigger_type, triggered_by_member_id, environment_id)
			VALUES ($1,$2,$3,'chat_message',$4,$5)`, c, ag, f.p, member, env)
	}

	f.role(`{"version":"1","statements":[
		{"effect":"Allow","actions":["views:read"],"resources":["project/*/view/` + v1.String() + `"]},
		{"effect":"Allow","actions":["workflows:read"],"resources":["project/*"]},
		{"effect":"Deny","actions":["workflows:read"],"resources":["project/*/workflow/` + a2.String() + `"]},
		{"effect":"Allow","actions":["conversations:read"],"resources":["project/*/conversation/*"],
		 "conditions":{"StringEquals":{"conversation.environment_id":"` + env2.String() + `"}}}]}`)

	views, err := NewViewRepository(f.db).ListProjectViews(f.ctx("views:read", "view"), f.p, "backlog")
	if err != nil || len(views) != 1 || views[0].ID != v1 {
		t.Fatalf("views = %v (%v), want only v1", views, err)
	}
	autos, _, err := NewAutomationRepository(f.db).ListAutomations(f.ctx("workflows:read", "workflow"), f.p, nil, nil, nil, nil)
	if err != nil || len(autos) != 1 || autos[0].ID != a1 {
		t.Fatalf("automations = %v (%v), want only a1", autos, err)
	}
	filter := agentdom.ListConversationsFilter{ProjectID: &f.p}
	convs, _, err := NewAgentRepository(f.db).ListConversations(f.ctx("conversations:read", "conversation"), filter, 10)
	if err != nil || len(convs) != 1 || convs[0].ID != c2 {
		t.Fatalf("conversations = %v (%v), want only c2 (environment 2)", convs, err)
	}
}

// A role limited to one sprint's views lists only that sprint's views; the
// project-level backlog/timeline views (view.sprint_id absent) are excluded
// by the positive condition and included by the negated one.
func TestScopedViewListBySprint(t *testing.T) {
	f := newScopeFx(t)
	s5, s6 := uuid.New(), uuid.New()
	for _, s := range []uuid.UUID{s5, s6} {
		pgExec(t, f.db, `INSERT INTO sprints (id, project_id, name) VALUES ($1,$2,$3)`, s, f.p, "S-"+s.String()[:6])
	}
	a1, a2, b1, bl := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for v, s := range map[uuid.UUID]uuid.UUID{a1: s5, a2: s5, b1: s6} {
		pgExec(t, f.db, `INSERT INTO sprint_views (id, sprint_id, project_id, name, view_context, position) VALUES ($1,$2,$3,'v','sprint',0)`, v, s, f.p)
	}
	pgExec(t, f.db, `INSERT INTO sprint_views (id, project_id, name, view_context) VALUES ($1,$2,'b','backlog')`, bl, f.p)

	ids := func(vs []*sprintdom.SprintView) []string {
		out := []string{}
		for _, v := range vs {
			out = append(out, v.ID.String())
		}
		return sorted(out)
	}
	repo := NewViewRepository(f.db)

	f.role(`{"version":"1","statements":[
		{"effect":"Allow","actions":["views:read"],"resources":["project/` + f.p.String() + `/view/*"],
		 "conditions":{"StringEquals":{"view.sprint_id":"` + s5.String() + `"}}}]}`)
	ctx := f.ctx("views:read", "view")
	got, err := repo.ListViews(ctx, s5)
	if err != nil || !reflect.DeepEqual(ids(got), sorted([]string{a1.String(), a2.String()})) {
		t.Fatalf("allowed sprint = %v (%v), want a1,a2", ids(got), err)
	}
	if got, err = repo.ListViews(ctx, s6); err != nil || len(got) != 0 {
		t.Fatalf("other sprint = %v (%v), want none", ids(got), err)
	}
	if got, err = repo.ListProjectViews(ctx, f.p, "backlog"); err != nil || len(got) != 0 {
		t.Fatalf("backlog = %v (%v), want none under a positive condition", ids(got), err)
	}
}

func TestScopedViewListNegatedSprintKeepsProjectViews(t *testing.T) {
	f := newScopeFx(t)
	s5 := uuid.New()
	pgExec(t, f.db, `INSERT INTO sprints (id, project_id, name) VALUES ($1,$2,'S5')`, s5, f.p)
	sv, bl := uuid.New(), uuid.New()
	pgExec(t, f.db, `INSERT INTO sprint_views (id, sprint_id, project_id, name, view_context) VALUES ($1,$2,$3,'v','sprint')`, sv, s5, f.p)
	pgExec(t, f.db, `INSERT INTO sprint_views (id, project_id, name, view_context) VALUES ($1,$2,'b','backlog')`, bl, f.p)
	f.role(`{"version":"1","statements":[
		{"effect":"Allow","actions":["views:read"],"resources":["project/` + f.p.String() + `/view/*"],
		 "conditions":{"StringNotEquals":{"view.sprint_id":"` + s5.String() + `"}}}]}`)
	ctx := f.ctx("views:read", "view")
	repo := NewViewRepository(f.db)
	if got, err := repo.ListViews(ctx, s5); err != nil || len(got) != 0 {
		t.Fatalf("excluded sprint = %v (%v)", got, err)
	}
	if got, err := repo.ListProjectViews(ctx, f.p, "backlog"); err != nil || len(got) != 1 || got[0].ID != bl {
		t.Fatalf("backlog = %v (%v), want the absent-attribute view", got, err)
	}
}

// "My tasks" spans projects: each project contributes its own scope.
func TestScopedAssignedTasksSpanProjectsWithOwnScopes(t *testing.T) {
	f := newScopeFx(t)
	p2 := f.fx.project()
	m1 := f.fx.member(f.p, f.fx.user(false), false, false)
	m2 := f.fx.member(p2, f.fx.user(false), false, false)
	s5, s6 := uuid.New(), uuid.New()
	pgExec(t, f.db, `INSERT INTO sprints (id, project_id, name) VALUES ($1,$3,'S5'), ($2,$3,'S6')`, s5, s6, f.p)
	in5, in6, other := uuid.New(), uuid.New(), uuid.New()
	pgExec(t, f.db, `INSERT INTO tasks (id, project_id, task_number, sprint_id, title) VALUES ($1,$4,1,$2,'a'), ($3,$4,2,$5,'b')`, in5, s5, in6, f.p, s6)
	pgExec(t, f.db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1,$2,1,'c')`, other, p2)
	for _, a := range []struct{ task, member uuid.UUID }{{in5, m1}, {in6, m1}, {other, m2}} {
		pgExec(t, f.db, `INSERT INTO task_assignees (task_id, member_id) VALUES ($1,$2)`, a.task, a.member)
	}
	scope := func(n *iam.Node) *iam.Node { return n }
	onlyS5 := &iam.Node{Kind: iam.NodeCond, Key: "task.sprint_id", Op: "In", Values: iam.ValueList{s5.String()}}

	ids := func(ctx context.Context) []string {
		ts, _, err := NewTaskRepository(f.db).ListAssignedTasks(ctx, []uuid.UUID{m1, m2}, 50, nil)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, tk := range ts {
			out = append(out, tk.ID.String())
		}
		return sorted(out)
	}
	ctx := iam.WithProjectScopes(context.Background(), "task", map[string]*iam.Node{
		f.p.String(): scope(onlyS5), p2.String(): iam.True(),
	})
	if got, want := ids(ctx), sorted([]string{in5.String(), other.String()}); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("scoped = %v, want %v", got, want)
	}
	// A project with no scope, or a deny-all one, is not listed.
	ctx = iam.WithProjectScopes(context.Background(), "task", map[string]*iam.Node{p2.String(): iam.False()})
	if got := ids(ctx); len(got) != 0 {
		t.Fatalf("denied = %v, want none", got)
	}
	if got := ids(context.Background()); len(got) != 3 {
		t.Fatalf("unscoped = %v, want all 3", got)
	}
}

// annotationFx inserts n annotations on page "/p" of one port forward and
// returns their ids, oldest first.
func (s *scopeFx) annotations(n int) (env, pf uuid.UUID, ids []string) {
	env, pf = uuid.New(), uuid.New()
	pgExec(s.t, s.db, `INSERT INTO environments (id, project_id, name, slug, backend, secret_key_encrypted) VALUES ($1,$2,'E','e','docker','x')`, env, s.p)
	pgExec(s.t, s.db, `INSERT INTO environment_port_forwards (id, environment_id, label, container_port) VALUES ($1,$2,'web',3000)`, pf, env)
	for i := 0; i < n; i++ {
		id := uuid.New()
		pgExec(s.t, s.db, `INSERT INTO page_annotations (id, project_id, environment_id, port_forward_id, page_path, element_selector,
			bounding_box, element_snapshot, body, created_by, created_at)
			VALUES ($1,$2,$3,$4,'/p','#a','{}','{}',$5,$6, now() + $7 * interval '1 second')`,
			id, s.p, env, pf, fmt.Sprintf("a%d", i), s.u, i)
		ids = append(ids, id.String())
	}
	return env, pf, ids
}

// Annotation lists (per page, per port forward and project search) are
// filtered in SQL: the project search pages over allowed annotations only.
func TestScopedAnnotationLists(t *testing.T) {
	f := newScopeFx(t)
	_, pf, ids := f.annotations(7)
	blocked := map[string]bool{ids[1]: true, ids[4]: true}
	var want []string
	for _, id := range ids {
		if !blocked[id] {
			want = append(want, id)
		}
	}
	f.role(`{"version":"1","statements":[
		{"effect":"Allow","actions":["annotations:read"],"resources":["project/*"]},
		{"effect":"Deny","actions":["annotations:read"],"resources":["project/*/annotation/` + ids[1] + `","project/*/annotation/` + ids[4] + `"]}]}`)
	ctx := f.ctx("annotations:read", "annotation")
	repo := NewAnnotationRepository(f.db)
	idsOf := func(as []*annotationdom.PageAnnotation) []string {
		var out []string
		for _, a := range as {
			out = append(out, a.ID.String())
		}
		return sorted(out)
	}

	page, err := repo.ListForPage(ctx, pf, "/p")
	if err != nil || fmt.Sprint(idsOf(page)) != fmt.Sprint(sorted(append([]string(nil), want...))) {
		t.Fatalf("ListForPage = %v (%v), want %v", idsOf(page), err, want)
	}
	all, err := repo.ListForPortForward(ctx, pf)
	if err != nil || fmt.Sprint(idsOf(all)) != fmt.Sprint(sorted(append([]string(nil), want...))) {
		t.Fatalf("ListForPortForward = %v (%v), want %v", idsOf(all), err, want)
	}

	// Pagination: every page is full of allowed annotations and the cursor
	// walks exactly the allowed set.
	limit := 2
	var got []string
	filter := annotationdom.SearchFilter{Limit: &limit}
	for i := 0; ; i++ {
		as, more, err := repo.SearchInProject(ctx, f.p, filter)
		if err != nil {
			t.Fatal(err)
		}
		if more && len(as) != limit {
			t.Fatalf("page %d has %d annotations but more follow", i, len(as))
		}
		got = append(got, idsOf(as)...)
		if !more {
			break
		}
		c := annotationdom.EncodeAnnotationCursor(as[len(as)-1])
		filter.Cursor = &c
		if i > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if fmt.Sprint(sorted(got)) != fmt.Sprint(sorted(append([]string(nil), want...))) {
		t.Fatalf("search paged %v, want %v", got, want)
	}

	// Search filters compose with the scope instead of replacing it.
	status := "open"
	as, _, err := repo.SearchInProject(ctx, f.p, annotationdom.SearchFilter{Status: &status, PortForwardID: &pf})
	if err != nil || len(as) != len(want) {
		t.Fatalf("filtered search = %d (%v), want %d", len(as), err, len(want))
	}

	// No access at all: nothing. Without a scope (workers) nothing is filtered.
	f2 := f.ctx("annotations:write", "annotation") // not granted: deny-all
	if as, _, err := repo.SearchInProject(f2, f.p, annotationdom.SearchFilter{}); err != nil || len(as) != 0 {
		t.Fatalf("no-access search = %d (%v)", len(as), err)
	}
	if as, err := repo.ListForPortForward(f2, pf); err != nil || len(as) != 0 {
		t.Fatalf("no-access list = %d (%v)", len(as), err)
	}
	if as, _, err := repo.SearchInProject(context.Background(), f.p, annotationdom.SearchFilter{}); err != nil || len(as) != 7 {
		t.Fatalf("unscoped search = %d (%v), want 7", len(as), err)
	}

	// An allow limited to one annotation id lists just that one.
	f.fx.attach(f.fx.role(policyJSON("Allow", "annotations:write", "project/*/annotation/"+ids[2], "")), "user", f.u, &f.p)
	if as, err := repo.ListForPortForward(f.ctx("annotations:write", "annotation"), pf); err != nil || fmt.Sprint(idsOf(as)) != fmt.Sprint([]string{ids[2]}) {
		t.Fatalf("id-limited list = %v (%v), want only %s", idsOf(as), err, ids[2])
	}

	// A condition the annotation columns cannot express fails closed.
	bad := iam.WithScope(context.Background(), "annotation", &iam.Node{Kind: iam.NodeCond, Key: "annotation.nope", Op: "In", Values: iam.ValueList{"x"}})
	if _, err := repo.ListForPortForward(bad, pf); err == nil {
		t.Fatal("unmapped attribute must be an error")
	}
	if _, _, err := repo.SearchInProject(bad, f.p, annotationdom.SearchFilter{}); err == nil {
		t.Fatal("unmapped attribute must be an error in search")
	}
}

// The home page's open-task count spans projects: each project contributes its
// own task scope, and a project with no scope (or deny-all) contributes nothing.
func TestScopedCountOpenTasksByProjects(t *testing.T) {
	f := newScopeFx(t)
	p2, p3 := f.fx.project(), f.fx.project()
	pgExec(t, f.db, `INSERT INTO task_statuses (id, project_id, name, category) VALUES ($1,$4,'todo','todo'), ($2,$5,'todo','todo'), ($3,$6,'done','done')`,
		uuid.New(), uuid.New(), uuid.New(), f.p, p2, f.p)
	var open1, done1 uuid.UUID
	if err := f.db.Get(&open1, `SELECT id FROM task_statuses WHERE project_id = $1 AND category = 'todo'`, f.p); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Get(&done1, `SELECT id FROM task_statuses WHERE project_id = $1 AND category = 'done'`, f.p); err != nil {
		t.Fatal(err)
	}
	var open2 uuid.UUID
	if err := f.db.Get(&open2, `SELECT id FROM task_statuses WHERE project_id = $1`, p2); err != nil {
		t.Fatal(err)
	}
	s5, s6 := uuid.New(), uuid.New()
	pgExec(t, f.db, `INSERT INTO sprints (id, project_id, name) VALUES ($1,$3,'S5'), ($2,$3,'S6')`, s5, s6, f.p)
	// Project 1: 2 open tasks in S5, 3 open in S6, 1 done in S5. Project 2: 4 open. Project 3: 1 task, no status.
	n := 0
	add := func(project uuid.UUID, sprint any, status uuid.UUID, count int) {
		for i := 0; i < count; i++ {
			n++
			pgExec(t, f.db, `INSERT INTO tasks (id, project_id, task_number, sprint_id, status_id, title) VALUES ($1,$2,$3,$4,$5,'t')`, uuid.New(), project, n, sprint, status)
		}
	}
	add(f.p, s5, open1, 2)
	add(f.p, s6, open1, 3)
	add(f.p, s5, done1, 1)
	add(p2, nil, open2, 4)
	pgExec(t, f.db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1,$2,99,'nostatus')`, uuid.New(), p3)

	repo := NewTaskRepository(f.db)
	all := []uuid.UUID{f.p, p2, p3}
	count := func(ctx context.Context) int64 {
		c, err := repo.CountOpenTasksByProjects(ctx, all)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	onlyS5 := &iam.Node{Kind: iam.NodeCond, Key: "task.sprint_id", Op: "In", Values: iam.ValueList{s5.String()}}

	if got := count(context.Background()); got != 9 {
		t.Fatalf("unscoped = %d, want 9 (5 + 4 open)", got)
	}
	scoped := func(m map[string]*iam.Node) context.Context {
		return iam.WithProjectScopes(context.Background(), "task", m)
	}
	for name, c := range map[string]struct {
		ctx  context.Context
		want int64
	}{
		"restricted project and open project":      {scoped(map[string]*iam.Node{f.p.String(): onlyS5, p2.String(): iam.True()}), 6},
		"a project with no scope is not counted":   {scoped(map[string]*iam.Node{p2.String(): iam.True()}), 4},
		"deny-all projects contribute nothing":     {scoped(map[string]*iam.Node{f.p.String(): iam.False(), p2.String(): iam.False()}), 0},
		"no scopes at all":                         {scoped(map[string]*iam.Node{}), 0},
		"a scope for an unlisted project is inert": {scoped(map[string]*iam.Node{"00000000-0000-0000-0000-000000000001": iam.True()}), 0},
		"negated condition":                        {scoped(map[string]*iam.Node{f.p.String(): iam.Not(onlyS5)}), 3},
	} {
		if got := count(c.ctx); got != c.want {
			t.Errorf("%s: count = %d, want %d", name, got, c.want)
		}
	}
	bad := scoped(map[string]*iam.Node{f.p.String(): {Kind: iam.NodeCond, Key: "task.nope", Op: "In", Values: iam.ValueList{"x"}}})
	if _, err := repo.CountOpenTasksByProjects(bad, all); err == nil {
		t.Fatal("unmapped attribute must be an error")
	}

	// End to end through the authorizer: a role limited to S5 tasks in project 1
	// and nothing in project 2 counts only the S5 open tasks.
	f.role(policyJSON("Allow", "tasks:read", "project/*/task/*", `{"StringEquals":{"task.sprint_id":["`+s5.String()+`"]}}`))
	byProject, err := f.auth.ListScopes(context.Background(), f.pr, "tasks:read", []string{f.p.String(), p2.String(), p3.String()}, "task")
	if err != nil {
		t.Fatal(err)
	}
	// (the role is attached to project 1 only)
	if got := count(iam.WithProjectScopes(context.Background(), "task", byProject)); got != 2 {
		t.Fatalf("authorizer scopes = %d, want 2", got)
	}
}

// A view's manual task positions only mention tasks the caller may read.
func TestScopedListTaskPositions(t *testing.T) {
	f := newScopeFx(t)
	s5, s6 := uuid.New(), uuid.New()
	pgExec(t, f.db, `INSERT INTO sprints (id, project_id, name) VALUES ($1,$3,'S5'), ($2,$3,'S6')`, s5, s6, f.p)
	view := uuid.New()
	pgExec(t, f.db, `INSERT INTO sprint_views (id, project_id, name, view_type, config, position, view_context) VALUES ($1,$2,'v','table','{}',0,'backlog')`, view, f.p)
	var in5, in6 []string
	for i := 1; i <= 6; i++ {
		id, sprint := uuid.New(), s6
		if i%2 == 0 {
			sprint = s5
			in5 = append(in5, id.String())
		} else {
			in6 = append(in6, id.String())
		}
		pgExec(t, f.db, `INSERT INTO tasks (id, project_id, task_number, sprint_id, title) VALUES ($1,$2,$3,$4,'t')`, id, f.p, i, sprint)
		pgExec(t, f.db, `INSERT INTO view_task_positions (id, view_id, task_id, position) VALUES ($1,$2,$3,$4)`, uuid.New(), view, id, i)
	}
	f.role(policyJSON("Allow", "tasks:read", "project/*/task/*", `{"StringEquals":{"task.sprint_id":["`+s5.String()+`"]}}`))

	repo := NewViewRepository(f.db)
	taskIDs := func(ctx context.Context) []string {
		ps, err := repo.ListTaskPositions(ctx, view)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, p := range ps {
			out = append(out, p.TaskID.String())
		}
		return sorted(out)
	}
	if got := taskIDs(f.ctx("tasks:read", "task")); fmt.Sprint(got) != fmt.Sprint(sorted(in5)) {
		t.Fatalf("scoped positions = %v, want the S5 tasks %v", got, in5)
	}
	if got := taskIDs(f.ctx("tasks:write", "task")); len(got) != 0 { // no tasks:write grant: deny-all
		t.Fatalf("no-access positions = %v, want none", got)
	}
	if got := taskIDs(context.Background()); len(got) != 6 {
		t.Fatalf("unscoped positions = %d, want 6", len(got))
	}
	bad := iam.WithScope(context.Background(), "task", &iam.Node{Kind: iam.NodeCond, Key: "task.nope", Op: "In", Values: iam.ValueList{"x"}})
	if _, err := repo.ListTaskPositions(bad, view); err == nil {
		t.Fatal("unmapped attribute must be an error")
	}
}

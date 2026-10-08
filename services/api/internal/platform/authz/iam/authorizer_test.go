package iam

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sort"
	"sync"
	"testing"
)

type fakeStore struct {
	grants map[string][]Grant // by principal ID
	err    error
	calls  int
}

func (f *fakeStore) ListGrants(_ context.Context, p Principal) ([]Grant, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.grants[p.ID], nil
}

type fakeLoader struct {
	kind string
	data map[string]map[string][]string // resource id -> all attrs
	// projects, when non-nil, binds resource id -> owning project; others are not found.
	projects map[string]string
	err      error
	calls    []loadCall
	mu       sync.Mutex
}

type loadCall struct {
	ID   string
	Keys []string
}

func (f *fakeLoader) Kind() string { return f.kind }
func (f *fakeLoader) Load(_ context.Context, id, projectID string, keys []string) (map[string][]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ks := slices.Clone(keys)
	sort.Strings(ks)
	f.calls = append(f.calls, loadCall{id, ks})
	if f.err != nil {
		return nil, f.err
	}
	if f.projects != nil {
		if pj, ok := f.projects[id]; !ok || (projectID != "" && pj != projectID) {
			return nil, ErrResourceNotInProject
		}
	}
	out := map[string][]string{}
	for _, k := range keys {
		if v, ok := f.data[id][k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

func newTestAuthorizer(grants []Grant, loaders ...AttributeLoader) (*Authorizer, *fakeStore) {
	st := &fakeStore{grants: map[string][]Grant{"u1": grants}}
	a := NewAuthorizer(st, NewRegistry(), NewAttributeSchema())
	for _, l := range loaders {
		a.RegisterLoader(l)
	}
	return a, st
}

var u1 = Principal{Type: "user", ID: "u1"}

func TestParseResource(t *testing.T) {
	cases := []struct{ in, kind, id, project string }{
		{"project/P/agent/A", "agent", "A", "P"},
		{"project/P", "project", "P", "P"},
		{"agent/A", "agent", "A", ""},
		{"project/P/task/T/comments/C", "task", "T", "P"},
		{"project/P/task", "task", "", "P"},
		{"settings", "settings", "", ""},
		{"*", "", "", ""},
		{"", "", "", ""},
		{"project//task/T", "", "", ""},
		{"project/P/task/", "", "", ""},
		{"/agent/A", "", "", ""},
	}
	for _, c := range cases {
		k, id, p := ParseResource(c.in)
		if k != c.kind || id != c.id || p != c.project {
			t.Errorf("ParseResource(%q) = (%q,%q,%q), want (%q,%q,%q)", c.in, k, id, p, c.kind, c.id, c.project)
		}
	}
}

func TestAuthorizeBasics(t *testing.T) {
	a, st := newTestAuthorizer([]Grant{{RoleID: "r", Policy: pol(allow("tasks:read", "project/*", nil))}})
	res, err := a.Authorize(context.Background(), u1, "tasks:read", "project/p1/task/t1")
	if err != nil || !res.Allowed {
		t.Fatalf("want allow, got %+v %v", res, err)
	}
	res, _ = a.Authorize(context.Background(), u1, "tasks:write", "project/p1/task/t1")
	if res.Allowed {
		t.Fatal("tasks:write must be denied")
	}
	// Unknown principal / invalid principal deny without touching the store.
	st.calls = 0
	for _, p := range []Principal{{Type: "user", ID: ""}, {Type: "robot", ID: "x"}, {}} {
		res, err = a.Authorize(context.Background(), p, "tasks:read", "project/p1")
		if err != nil || res.Allowed {
			t.Fatalf("invalid principal %+v: %+v %v", p, res, err)
		}
	}
	if st.calls != 0 {
		t.Fatal("invalid principals must not reach the store")
	}
}

func TestAuthorizeStoreError(t *testing.T) {
	a, st := newTestAuthorizer(nil)
	st.err = errors.New("db down")
	res, err := a.Authorize(context.Background(), u1, "tasks:read", "project/p1")
	if err == nil || res.Allowed || len(res.Matched) != 0 {
		t.Fatalf("want (Result{}, err), got %+v %v", res, err)
	}
}

func TestNilPolicyGrantsNothing(t *testing.T) {
	a, _ := newTestAuthorizer([]Grant{{RoleID: "bad", Policy: nil}})
	res, err := a.Authorize(context.Background(), u1, "tasks:read", "project/p1")
	if err != nil || res.Allowed {
		t.Fatalf("nil policy: %+v %v", res, err)
	}
}

func TestProjectScopedGrantDoesNotCrossProjects(t *testing.T) {
	// GHSA-hjcj class: a project-scoped attachment of a "project/*" role.
	a, _ := newTestAuthorizer([]Grant{{RoleID: "r", ProjectID: "p1", Policy: pol(allow("*", "project/*", nil))}})
	ctx := context.Background()
	if res, _ := a.Authorize(ctx, u1, "tasks:write", "project/p1/task/t"); !res.Allowed {
		t.Fatal("own project must allow")
	}
	for _, r := range []string{"project/p2", "project/p2/task/t", "project/p10", "user/u2", "settings"} {
		if res, _ := a.Authorize(ctx, u1, "tasks:write", r); res.Allowed {
			t.Fatalf("must not allow %s", r)
		}
	}
}

func TestPrincipalAttrsAvailable(t *testing.T) {
	a, _ := newTestAuthorizer([]Grant{
		{RoleID: "r", Policy: pol(
			allow("*", "*", nil),
			deny("*", "project/p1/agent/*", Conditions{"NotIn": {"principal.id": {"u1"}}}),
			deny("*", "project/p2/*", Conditions{"StringEquals": {"principal.type": {"user"}}}),
		)},
	})
	ctx := context.Background()
	if res, _ := a.Authorize(ctx, u1, "agents:read", "project/p1/agent/a"); !res.Allowed {
		t.Fatal("u1 is in the NotIn list; deny must not fire")
	}
	if res, _ := a.Authorize(ctx, u1, "agents:read", "project/p2/agent/a"); res.Allowed {
		t.Fatal("principal.type=user deny must fire")
	}
	// resource.id comes from the resource name
	a2, _ := newTestAuthorizer([]Grant{{RoleID: "r", Policy: pol(allow("*", "*", Conditions{"In": {"resource.id": {"a1"}}}))}})
	if res, _ := a2.Authorize(ctx, u1, "agents:read", "project/p/agent/a1"); !res.Allowed {
		t.Fatal("resource.id a1 must match")
	}
	if res, _ := a2.Authorize(ctx, u1, "agents:read", "project/p/agent/a2"); res.Allowed {
		t.Fatal("resource.id a2 must not match")
	}
}

func taskGrant(sprint string) []Grant {
	return []Grant{{RoleID: "r", Policy: pol(allow("tasks:read", "project/p1/task/*", Conditions{"StringEquals": {"task.sprint_id": {sprint}}}))}}
}

func TestAttributeLoadingLimitedToReferencedKeys(t *testing.T) {
	tl := &fakeLoader{kind: "task", data: map[string]map[string][]string{
		"t5": {"task.sprint_id": {"S5"}, "task.status_id": {"X"}},
		"t6": {"task.sprint_id": {"S6"}},
	}}
	a, _ := newTestAuthorizer(taskGrant("S5"), tl)
	ctx := context.Background()
	if res, err := a.Authorize(ctx, u1, "tasks:read", "project/p1/task/t5"); err != nil || !res.Allowed {
		t.Fatalf("S5 task must be allowed: %+v %v", res, err)
	}
	if res, err := a.Authorize(ctx, u1, "tasks:read", "project/p1/task/t6"); err != nil || res.Allowed {
		t.Fatalf("S6 task must be denied: %+v %v", res, err)
	}
	want := []loadCall{{"t5", []string{"task.sprint_id"}}, {"t6", []string{"task.sprint_id"}}}
	if !reflect.DeepEqual(tl.calls, want) {
		t.Fatalf("loader calls = %+v, want %+v", tl.calls, want)
	}
}

func TestNoLoaderCallWithoutReferencedAttributeKeys(t *testing.T) {
	tl := &fakeLoader{kind: "task"}
	a, _ := newTestAuthorizer([]Grant{{RoleID: "r", Policy: pol(allow("tasks:read", "*", Conditions{"In": {"principal.id": {"u1"}}}))}}, tl)
	if res, err := a.Authorize(context.Background(), u1, "tasks:read", "project/p/task/t"); err != nil || !res.Allowed {
		t.Fatalf("%+v %v", res, err)
	}
	// Referenced key belongs to another kind: not loaded for a task resource.
	a2, _ := newTestAuthorizer([]Grant{{RoleID: "r", Policy: pol(allow("*", "*", Conditions{"In": {"doc.folder_id": {"f"}}}))}}, tl)
	_, _ = a2.Authorize(context.Background(), u1, "tasks:read", "project/p/task/t")
	if len(tl.calls) != 0 {
		t.Fatalf("loader must not be called, got %+v", tl.calls)
	}
}

func TestLoaderErrorFailsClosedWithError(t *testing.T) {
	tl := &fakeLoader{kind: "task", err: errors.New("boom")}
	a, _ := newTestAuthorizer(taskGrant("S5"), tl)
	res, err := a.Authorize(context.Background(), u1, "tasks:read", "project/p1/task/t5")
	if err == nil || res.Allowed {
		t.Fatalf("want error + deny, got %+v %v", res, err)
	}
}

func TestMissingLoaderIsAnError(t *testing.T) {
	a, _ := newTestAuthorizer(taskGrant("S5")) // no loader registered
	res, err := a.Authorize(context.Background(), u1, "tasks:read", "project/p1/task/t5")
	if err == nil || res.Allowed {
		t.Fatalf("want error + deny, got %+v %v", res, err)
	}
}

func TestWithAttrsOverridesLoaded(t *testing.T) {
	tl := &fakeLoader{kind: "task", data: map[string]map[string][]string{"t6": {"task.sprint_id": {"S6"}}}}
	a, _ := newTestAuthorizer(taskGrant("S5"), tl)
	res, err := a.Authorize(context.Background(), u1, "tasks:read", "project/p1/task/t6", WithAttrs(map[string][]string{"task.sprint_id": {"S5"}}))
	if err != nil || !res.Allowed {
		t.Fatalf("override must win: %+v %v", res, err)
	}
	if len(tl.calls) != 0 {
		t.Fatalf("overridden key must not be loaded: %+v", tl.calls)
	}
	// Create: resource does not exist yet (id "*"); request attrs decide.
	res, _ = a.Authorize(context.Background(), u1, "tasks:read", "project/p1/task/*", WithAttrs(map[string][]string{"task.sprint_id": {"S6"}}))
	if res.Allowed {
		t.Fatal("S6 create must be denied")
	}
	res, _ = a.Authorize(context.Background(), u1, "tasks:read", "project/p1/task/*", WithAttrs(map[string][]string{"task.sprint_id": {"S5"}}))
	if !res.Allowed {
		t.Fatal("S5 create must be allowed")
	}
	// wildcard/empty id is never sent to a loader
	_, _ = a.Authorize(context.Background(), u1, "tasks:read", "project/p1/task/*")
	if len(tl.calls) != 0 {
		t.Fatalf("loader called for wildcard id: %+v", tl.calls)
	}
}

func TestAuthorizeChange(t *testing.T) {
	tl := &fakeLoader{kind: "task", data: map[string]map[string][]string{
		"t1": {"task.sprint_id": {"S5"}},
		"t2": {"task.sprint_id": {"S6"}},
	}}
	g := []Grant{{RoleID: "r", Policy: pol(allow("tasks:write", "project/p1/task/*", Conditions{"StringEquals": {"task.sprint_id": {"S5"}}}))}}
	a, _ := newTestAuthorizer(g, tl)
	ctx := context.Background()
	move := func(id, to string) bool {
		res, err := a.AuthorizeChange(ctx, u1, "tasks:write", "project/p1/task/"+id, map[string][]string{"task.sprint_id": {to}})
		if err != nil {
			t.Fatal(err)
		}
		return res.Allowed
	}
	if !move("t1", "S5") {
		t.Error("S5 -> S5 must be allowed")
	}
	if move("t1", "S6") {
		t.Error("moving out of S5: new state does not match, must be denied")
	}
	if move("t2", "S5") {
		t.Error("moving into S5 from S6: old state does not match, must be denied")
	}
	if move("t2", "S6") {
		t.Error("S6 -> S6 must be denied")
	}
	// loader error propagates
	tl.err = errors.New("boom")
	if _, err := a.AuthorizeChange(ctx, u1, "tasks:write", "project/p1/task/t1", map[string][]string{"task.sprint_id": {"S5"}}); err == nil {
		t.Error("loader error must surface")
	}
}

func TestDocAncestorFolders(t *testing.T) {
	dl := &fakeLoader{kind: "doc", data: map[string]map[string][]string{
		"d-grand": {"doc.ancestor_folder_ids": {"C", "B", "ABC"}},
		"d-other": {"doc.ancestor_folder_ids": {"X"}},
		"d-root":  {},
	}}
	a, _ := newTestAuthorizer([]Grant{{RoleID: "r", Policy: pol(allow("docs:read", "project/p1/doc/*", Conditions{"In": {"doc.ancestor_folder_ids": {"ABC"}}}))}}, dl)
	ctx := context.Background()
	for id, want := range map[string]bool{"d-grand": true, "d-other": false, "d-root": false} {
		res, err := a.Authorize(ctx, u1, "docs:read", "project/p1/doc/"+id)
		if err != nil || res.Allowed != want {
			t.Errorf("%s: allowed=%v err=%v want %v", id, res.Allowed, err, want)
		}
	}
}

func TestAgentEnvironmentCondition(t *testing.T) {
	al := &fakeLoader{kind: "agent", data: map[string]map[string][]string{
		"a-in-A": {"agent.environment_id": {"envA"}},
		"a-in-B": {"agent.environment_id": {"envB"}},
	}}
	a, _ := newTestAuthorizer([]Grant{{RoleID: "r", Policy: pol(allow("conversations:write", "project/p1/agent/*", Conditions{"StringEquals": {"agent.environment_id": {"envA"}}}))}}, al)
	ctx := context.Background()
	if res, _ := a.Authorize(ctx, u1, "conversations:write", "project/p1/agent/a-in-A"); !res.Allowed {
		t.Error("agent in A must be allowed")
	}
	if res, _ := a.Authorize(ctx, u1, "conversations:write", "project/p1/agent/a-in-B"); res.Allowed {
		t.Error("agent in B must be denied")
	}
}

func TestListScope(t *testing.T) {
	ctx := context.Background()
	t.Run("no access", func(t *testing.T) {
		a, _ := newTestAuthorizer(nil)
		n, err := a.ListScope(ctx, u1, "tasks:read", "p1", "task")
		if err != nil || !n.DeniesAll() {
			t.Fatalf("%+v %v", n, err)
		}
	})
	t.Run("allow all", func(t *testing.T) {
		a, _ := newTestAuthorizer([]Grant{{RoleID: "r", Policy: pol(allow("tasks:read", "project/p1/*", nil))}})
		n, err := a.ListScope(ctx, u1, "tasks:read", "p1", "task")
		if err != nil || !n.Unrestricted() {
			t.Fatalf("%+v %v", n, err)
		}
	})
	t.Run("conditional allow becomes a predicate", func(t *testing.T) {
		a, _ := newTestAuthorizer(taskGrant("S5"))
		n, err := a.ListScope(ctx, u1, "tasks:read", "p1", "task")
		if err != nil || n.Unrestricted() || n.DeniesAll() {
			t.Fatalf("%+v %v", n, err)
		}
		if !n.Eval(map[string][]string{"resource.id": {"t1"}, "task.sprint_id": {"S5"}}) ||
			n.Eval(map[string][]string{"resource.id": {"t2"}, "task.sprint_id": {"S6"}}) {
			t.Fatalf("wrong predicate: %+v", n)
		}
	})
	t.Run("a store error is an error, never unrestricted", func(t *testing.T) {
		a, st := newTestAuthorizer([]Grant{{RoleID: "r", Policy: pol(allow("*", "*", nil))}})
		st.err = errors.New("db")
		n, err := a.ListScope(ctx, u1, "tasks:read", "p1", "task")
		if err == nil || (n != nil && n.Unrestricted()) {
			t.Fatalf("%+v %v", n, err)
		}
	})
	t.Run("grants fetched once", func(t *testing.T) {
		a, st := newTestAuthorizer(taskGrant("S5"))
		_, _ = a.ListScope(ctx, u1, "tasks:read", "p1", "task")
		if st.calls != 1 {
			t.Fatalf("ListGrants calls = %d", st.calls)
		}
	})
}

func TestListScopes(t *testing.T) {
	ctx := context.Background()
	t.Run("one scope per project from a single grant fetch", func(t *testing.T) {
		a, st := newTestAuthorizer([]Grant{
			{RoleID: "r1", ProjectID: "p1", Policy: pol(allow("tasks:read", "project/p1/task/*", Conditions{"StringEquals": {"task.sprint_id": {"S5"}}}))},
			{RoleID: "r2", ProjectID: "p2", Policy: pol(allow("tasks:read", "project/p2/*", nil))},
		})
		got, err := a.ListScopes(ctx, u1, "tasks:read", []string{"p1", "p2", "p3"}, "task")
		if err != nil {
			t.Fatal(err)
		}
		if st.calls != 1 {
			t.Fatalf("ListGrants calls = %d, want 1", st.calls)
		}
		if n := got["p1"]; n.Unrestricted() || n.DeniesAll() ||
			!n.Eval(map[string][]string{"resource.id": {"t"}, "task.sprint_id": {"S5"}}) ||
			n.Eval(map[string][]string{"resource.id": {"t"}, "task.sprint_id": {"S6"}}) {
			t.Fatalf("p1 scope = %+v", n)
		}
		if !got["p2"].Unrestricted() {
			t.Fatalf("p2 scope = %+v, want unrestricted", got["p2"])
		}
		if !got["p3"].DeniesAll() {
			t.Fatalf("p3 scope = %+v, want deny-all (no grant there)", got["p3"])
		}
		// Each equals what ListScope answers for that project alone.
		for _, pid := range []string{"p1", "p2", "p3"} {
			one, _ := a.ListScope(ctx, u1, "tasks:read", pid, "task")
			if one.Unrestricted() != got[pid].Unrestricted() || one.DeniesAll() != got[pid].DeniesAll() {
				t.Errorf("%s: ListScopes disagrees with ListScope", pid)
			}
		}
	})
	t.Run("an invalid principal sees nothing anywhere", func(t *testing.T) {
		a, _ := newTestAuthorizer([]Grant{{RoleID: "r", Policy: pol(allow("*", "*", nil))}})
		got, err := a.ListScopes(ctx, Principal{}, "tasks:read", []string{"p1"}, "task")
		if err != nil || !got["p1"].DeniesAll() {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("a store error is an error, never a scope", func(t *testing.T) {
		a, st := newTestAuthorizer([]Grant{{RoleID: "r", Policy: pol(allow("*", "*", nil))}})
		st.err = errors.New("db")
		got, err := a.ListScopes(ctx, u1, "tasks:read", []string{"p1"}, "task")
		if err == nil || got != nil {
			t.Fatalf("%+v %v", got, err)
		}
	})
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestEffectiveActions(t *testing.T) {
	g := []Grant{
		{RoleID: "r1", ProjectID: "p1", Policy: pol(allow("tasks:*", "project/p1/*", nil), allow("projects:read", "project/p1", nil), deny("tasks:write", "project/p1/*", nil))},
	}
	a, _ := newTestAuthorizer(g)
	got, err := a.EffectiveActions(context.Background(), u1, "p1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"projects:read", "tasks:read"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	// other project: nothing
	got, _ = a.EffectiveActions(context.Background(), u1, "p2")
	if len(got) != 0 {
		t.Fatalf("p2 got %v", got)
	}
	// platform scope: "*" holder gets every platform action
	a2, _ := newTestAuthorizer([]Grant{{RoleID: "r", Policy: pol(allow("*", "*", nil))}})
	got, _ = a2.EffectiveActions(context.Background(), u1, "")
	var wantPlat []string
	for _, act := range NewRegistry().Actions() {
		if PlatformRootFor(act) != "" {
			wantPlat = append(wantPlat, act)
		}
	}
	if len(wantPlat) == 0 || !reflect.DeepEqual(got, wantPlat) {
		t.Fatalf("platform: got %v want %v", got, wantPlat)
	}
	// named global role on a platform root
	a4, _ := newTestAuthorizer([]Grant{{RoleID: "r", Policy: pol(allow("users:read", "user/*", nil))}})
	got, _ = a4.EffectiveActions(context.Background(), u1, "")
	if !reflect.DeepEqual(got, []string{"users:read"}) {
		t.Fatalf("users:read only, got %v", got)
	}
	// project-scoped attachment contributes nothing at platform level
	a5, _ := newTestAuthorizer([]Grant{{RoleID: "r", ProjectID: "p1", Policy: pol(allow("*", "*", nil))}})
	got, _ = a5.EffectiveActions(context.Background(), u1, "")
	if len(got) != 0 {
		t.Fatalf("project-scoped must contribute nothing, got %v", got)
	}
	// store error
	a3, st := newTestAuthorizer(nil)
	st.err = errors.New("x")
	if _, err := a3.EffectiveActions(context.Background(), u1, "p1"); err == nil {
		t.Fatal("want error")
	}
}

type invStore struct {
	fakeStore
	invalidated []string
}

func (s *invStore) Invalidate(ids ...string) { s.invalidated = append(s.invalidated, ids...) }

func TestInvalidateDelegatesToStore(t *testing.T) {
	st := &invStore{}
	a := NewAuthorizer(st, NewRegistry(), NewAttributeSchema())
	a.Invalidate("r1", "r2")
	if !reflect.DeepEqual(st.invalidated, []string{"r1", "r2"}) {
		t.Fatalf("got %v", st.invalidated)
	}
	// store without Invalidator: no panic
	a2, _ := newTestAuthorizer(nil)
	a2.Invalidate("r1")
}

func TestPlatformRootFor(t *testing.T) {
	cases := map[string]string{
		"users:read": "user/*", "users:delete": "user/*", "roles:write": "role/*", "plugins:read": "plugin/*",
		"settings:write": "settings", "settings.sso:write": "sso", "agents:read": "agent/*",
		"projects:create": "project", "projects:read": "project",
		"tasks:read": "", "project.members:read": "", "project:export": "", "conversations:write": "",
		"plugin.jev:sync": "", "*": "", "": "",
	}
	for in, want := range cases {
		if got := PlatformRootFor(in); got != want {
			t.Errorf("PlatformRootFor(%q) = %q, want %q", in, got, want)
		}
	}
}

type expLoader struct {
	fakeLoader
	// folder -> ancestors incl. itself
	tree map[string][]string
}

func (e *expLoader) Expand(_ context.Context, _, _ string, n map[string][]string) (map[string][]string, error) {
	out := map[string][]string{}
	for k, v := range n {
		out[k] = v
	}
	if f, ok := n["doc.folder_id"]; ok {
		if len(f) == 0 || f[0] == "" {
			out["doc.folder_id"], out["doc.ancestor_folder_ids"] = nil, nil
		} else {
			out["doc.ancestor_folder_ids"] = e.tree[f[0]]
		}
	}
	return out, nil
}

func TestAuthorizeChangeExpandsDocAncestors(t *testing.T) {
	dl := &expLoader{
		fakeLoader: fakeLoader{kind: "doc", data: map[string]map[string][]string{
			"d": {"doc.ancestor_folder_ids": {"C", "B", "ABC"}},
		}},
		tree: map[string][]string{"B": {"B", "ABC"}, "OUT": {"OUT"}},
	}
	g := []Grant{{RoleID: "r", Policy: pol(allow("docs:write", "project/p1/doc/*", Conditions{"In": {"doc.ancestor_folder_ids": {"ABC"}}}))}}
	a, _ := newTestAuthorizer(g, dl)
	ctx := context.Background()
	move := func(to string) bool {
		res, err := a.AuthorizeChange(ctx, u1, "docs:write", "project/p1/doc/d", map[string][]string{"doc.folder_id": {to}})
		if err != nil {
			t.Fatal(err)
		}
		return res.Allowed
	}
	if !move("B") {
		t.Error("move within subtree must be allowed")
	}
	if move("OUT") {
		t.Error("move outside subtree must be denied")
	}
	if move("") {
		t.Error("move to root must be denied")
	}
}

func TestWithAttrsCannotSpoofPrincipal(t *testing.T) {
	// Allow only for principal u2; the caller (u1) tries to override principal.id/type/resource.id.
	g := []Grant{{RoleID: "r", Policy: pol(allow("*", "*", Conditions{"In": {"principal.id": {"u2"}}}))}}
	a, _ := newTestAuthorizer(g)
	res, err := a.Authorize(context.Background(), u1, "tasks:read", "project/p/task/t",
		WithAttrs(map[string][]string{"principal.id": {"u2"}}))
	if err != nil || res.Allowed {
		t.Fatalf("principal.id override must be ignored: %+v %v", res, err)
	}
	g = []Grant{{RoleID: "r", Policy: pol(allow("*", "*", Conditions{"In": {"resource.id": {"other"}}, "StringEquals": {"principal.type": {"agent"}}}))}}
	a, _ = newTestAuthorizer(g)
	res, _ = a.Authorize(context.Background(), u1, "tasks:read", "project/p/task/t",
		WithAttrs(map[string][]string{"resource.id": {"other"}, "principal.type": {"agent"}}))
	if res.Allowed {
		t.Fatal("resource.id / principal.type overrides must be ignored")
	}
	// An attribute of another kind is ignored as well.
	g = []Grant{{RoleID: "r", Policy: pol(allow("*", "*", Conditions{"In": {"doc.folder_id": {"f"}}}))}}
	a, _ = newTestAuthorizer(g, &fakeLoader{kind: "task"})
	res, _ = a.Authorize(context.Background(), u1, "tasks:read", "project/p/task/*",
		WithAttrs(map[string][]string{"doc.folder_id": {"f"}}))
	if res.Allowed {
		t.Fatal("other-kind override must be ignored")
	}
}

func TestAuthorizeChangeFetchesGrantsOnce(t *testing.T) {
	tl := &fakeLoader{kind: "task", data: map[string]map[string][]string{"t1": {"task.sprint_id": {"S5"}}}}
	g := []Grant{{RoleID: "r", Policy: pol(allow("tasks:write", "project/p1/task/*", Conditions{"StringEquals": {"task.sprint_id": {"S5"}}}))}}
	a, st := newTestAuthorizer(g, tl)
	if res, err := a.AuthorizeChange(context.Background(), u1, "tasks:write", "project/p1/task/t1", map[string][]string{"task.sprint_id": {"S5"}}); err != nil || !res.Allowed {
		t.Fatalf("%+v %v", res, err)
	}
	if st.calls != 1 {
		t.Fatalf("ListGrants calls = %d, want 1", st.calls)
	}
}

// Cross-project resource ids must deny whether the Allow's condition is
// positive or negated (a negated one would be satisfied by absent attributes).
func TestCrossProjectResourceDenied(t *testing.T) {
	tl := &fakeLoader{kind: "task",
		data:     map[string]map[string][]string{"tP1": {"task.sprint_id": {"S5"}}, "tP2": {"task.sprint_id": {"S9"}}},
		projects: map[string]string{"tP1": "p1", "tP2": "p2"}}
	positive := Conditions{"StringEquals": {"task.sprint_id": {"S9"}}}
	negated := Conditions{"StringNotEquals": {"task.sprint_id": {"S5"}}}
	ctx := context.Background()
	for name, cond := range map[string]Conditions{"positive": positive, "negated": negated} {
		g := []Grant{{RoleID: "r", ProjectID: "p1", Policy: pol(allow("tasks:read", "project/p1/task/*", cond))}}
		a, _ := newTestAuthorizer(g, tl)
		res, err := a.Authorize(ctx, u1, "tasks:read", "project/p1/task/tP2")
		if err != nil || res.Allowed {
			t.Errorf("%s: cross-project id must deny without error: %+v %v", name, res, err)
		}
		res, err = a.Authorize(ctx, u1, "tasks:read", "project/p1/task/missing")
		if err != nil || res.Allowed {
			t.Errorf("%s: not-found id must deny without error: %+v %v", name, res, err)
		}
		res, err = a.AuthorizeChange(ctx, u1, "tasks:read", "project/p1/task/tP2", map[string][]string{"task.sprint_id": {"S9"}})
		if err != nil || res.Allowed {
			t.Errorf("%s: change cross-project: %+v %v", name, res, err)
		}
	}
	// Other loader errors still propagate.
	bad := &fakeLoader{kind: "task", err: errors.New("db down")}
	a, _ := newTestAuthorizer([]Grant{{RoleID: "r", Policy: pol(allow("tasks:read", "*", negated))}}, bad)
	if res, err := a.Authorize(ctx, u1, "tasks:read", "project/p1/task/tP1"); err == nil || res.Allowed {
		t.Fatalf("non-sentinel error must propagate: %+v %v", res, err)
	}
}

type errExpander struct {
	fakeLoader
	err error
}

func (e *errExpander) Expand(context.Context, string, string, map[string][]string) (map[string][]string, error) {
	return nil, e.err
}

func TestExpandErrors(t *testing.T) {
	g := []Grant{{RoleID: "r", Policy: pol(allow("docs:write", "project/p1/doc/*", nil))}}
	ctx := context.Background()
	a, _ := newTestAuthorizer(g, &errExpander{fakeLoader: fakeLoader{kind: "doc"}, err: ErrResourceNotInProject})
	if res, err := a.AuthorizeChange(ctx, u1, "docs:write", "project/p1/doc/d", map[string][]string{"doc.folder_id": {"f"}}); err != nil || res.Allowed {
		t.Fatalf("cross-project folder must deny: %+v %v", res, err)
	}
	a, _ = newTestAuthorizer(g, &errExpander{fakeLoader: fakeLoader{kind: "doc"}, err: errors.New("db")})
	if res, err := a.AuthorizeChange(ctx, u1, "docs:write", "project/p1/doc/d", map[string][]string{"doc.folder_id": {"f"}}); err == nil || res.Allowed {
		t.Fatalf("other expand error must propagate: %+v %v", res, err)
	}
}

func TestPlatformAgentWithoutProject(t *testing.T) {
	al := &fakeLoader{kind: "agent",
		data:     map[string]map[string][]string{"ga": {"agent.environment_id": {"envA"}}},
		projects: map[string]string{"ga": ""}}
	g := []Grant{{RoleID: "r", Policy: pol(allow("agents:read", "agent/*", Conditions{"StringEquals": {"agent.environment_id": {"envA"}}}))}}
	a, _ := newTestAuthorizer(g, al)
	res, err := a.Authorize(context.Background(), u1, "agents:read", "agent/ga")
	if err != nil || !res.Allowed {
		t.Fatalf("%+v %v", res, err)
	}
	if len(al.calls) != 1 {
		t.Fatalf("calls %+v", al.calls)
	}
}

func TestPossiblyAllowed(t *testing.T) {
	cond := Conditions{"StringEquals": {"task.sprint_id": {"S5"}}}
	cases := []struct {
		name     string
		grants   []Grant
		action   string
		resource string
		want     bool
	}{
		{"conditional allow counts", []Grant{{Policy: pol(allow("tasks:read", "project/P/task/*", cond))}}, "tasks:read", "project/P/task/*", true},
		{"specific id under wildcard", []Grant{{Policy: pol(allow("agents:read", "project/P/agent/A", nil))}}, "agents:read", "project/P/agent/*", true},
		{"specific role id under wildcard", []Grant{{Policy: pol(allow("roles:assign", "project/P/role/EDITOR", nil))}}, "roles:assign", "project/P/role/*", true},
		{"platform role id under wildcard", []Grant{{Policy: pol(allow("roles:assign", "role/VIEWER", nil))}}, "roles:assign", "role/*", true},
		{"specific id with extra segments", []Grant{{Policy: pol(allow("agents:read", "project/P/agent/A/sub", nil))}}, "agents:read", "project/P/agent/*", false},
		{"wildcard in policy", []Grant{{Policy: pol(allow("agents:read", "project/P/agent/*", nil))}}, "agents:read", "project/P/agent/*", true},
		{"unconditional deny hides", []Grant{{Policy: pol(allow("tasks:read", "project/P/task/*", cond), deny("tasks:*", "project/P/*", nil))}}, "tasks:read", "project/P/task/*", false},
		{"conditional deny does not hide", []Grant{{Policy: pol(allow("tasks:read", "project/P/task/*", nil), deny("tasks:read", "project/P/task/*", cond))}}, "tasks:read", "project/P/task/*", true},
		{"other project scope", []Grant{{ProjectID: "Q", Policy: pol(allow("*", "*", nil))}}, "tasks:read", "project/P/task/*", false},
		{"own project scope", []Grant{{ProjectID: "P", Policy: pol(allow("*", "project/*", nil))}}, "tasks:read", "project/P/task/*", true},
		{"wrong action", []Grant{{Policy: pol(allow("tasks:write", "project/P/task/*", nil))}}, "tasks:read", "project/P/task/*", false},
		{"nil policy", []Grant{{Policy: nil}}, "tasks:read", "project/P/task/*", false},
		{"deny elsewhere", []Grant{{Policy: pol(allow("tasks:read", "project/P/task/*", nil), deny("tasks:read", "project/Q/*", nil))}}, "tasks:read", "project/P/task/*", true},
	}
	for _, c := range cases {
		if got := PossiblyAllowed(c.grants, c.action, c.resource); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestEffectiveActionsKindScoped(t *testing.T) {
	ctx := context.Background()
	cond := Conditions{"StringEquals": {"task.sprint_id": {"S5"}}}
	eff := func(grants []Grant, proj string) []string {
		a, _ := newTestAuthorizer(grants)
		got, err := a.EffectiveActions(ctx, u1, proj)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := eff([]Grant{{ProjectID: "P", Policy: pol(allow("tasks:read", "project/P/task/*", cond))}}, "P"); !reflect.DeepEqual(got, []string{"tasks:read"}) {
		t.Errorf("conditional kind-scoped: %v", got)
	}
	if got := eff([]Grant{{ProjectID: "P", Policy: pol(allow("agents:read", "project/P/agent/A", nil))}}, "P"); !reflect.DeepEqual(got, []string{"agents:read"}) {
		t.Errorf("specific resource ID should be reported as possibly allowed: %v", got)
	}
	if got := eff([]Grant{{ProjectID: "P", Policy: pol(allow("tasks:read", "project/P/task/*", nil), deny("tasks:read", "project/P/*", nil))}}, "P"); len(got) != 0 {
		t.Errorf("unconditional deny hides: %v", got)
	}
	if got := eff([]Grant{{ProjectID: "P", Policy: pol(allow("tasks:read", "project/P/task/*", nil), deny("tasks:read", "project/P/task/*", cond))}}, "P"); !reflect.DeepEqual(got, []string{"tasks:read"}) {
		t.Errorf("conditional deny must not hide: %v", got)
	}
	if got := eff([]Grant{{ProjectID: "Q", Policy: pol(allow("tasks:read", "project/Q/task/*", nil))}}, "P"); len(got) != 0 {
		t.Errorf("other project contributes nothing: %v", got)
	}
}

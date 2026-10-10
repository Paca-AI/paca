package iam

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func allowStmt(actions []string, resources []string, cond Conditions) Statement {
	return Statement{Effect: EffectAllow, Actions: actions, Resources: resources, Conditions: cond}
}

func denyStmt(actions []string, resources []string, cond Conditions) Statement {
	return Statement{Effect: EffectDeny, Actions: actions, Resources: resources, Conditions: cond}
}

func grantOf(project string, sts ...Statement) Grant {
	return Grant{RoleID: "r", ProjectID: project, Policy: &Policy{Statements: sts}}
}

func candidate(sts ...Statement) *Policy { return &Policy{Statements: sts} }

func TestAuthorizerSimulate(t *testing.T) {
	ctx := context.Background()
	deny := Policy{Statements: []Statement{denyStmt([]string{"tasks:write"}, []string{"project/P/task/*"}, nil)}}
	store := &fakeStore{grants: map[string][]Grant{
		"u1": {{RoleID: "role-1", Policy: &deny}, {RoleID: "role-2", Policy: &Policy{Statements: []Statement{
			allowStmt([]string{"tasks:read"}, []string{"project/P/*"}, nil)}}}},
	}}
	a := NewAuthorizer(store, NewRegistry(), NewAttributeSchema())
	pol := &Policy{Statements: []Statement{{Sid: "S", Effect: EffectAllow, Actions: []string{"tasks:*"}, Resources: []string{"project/P/task/*"},
		Conditions: Conditions{"StringEquals": {"task.sprint_id": {"s1"}}}}}}

	// The policy alone.
	res, err := a.Simulate(ctx, nil, pol, "tasks:write", "project/P/task/t1", map[string][]string{"task.sprint_id": {"s1"}})
	if err != nil || !res.Allowed || len(res.Matched) != 1 || res.Matched[0].RoleID != PolicyMatchID || res.Matched[0].Sid != "S" {
		t.Fatalf("policy alone: %+v err=%v", res, err)
	}
	// The condition is evaluated on the supplied attributes only.
	if res, _ = a.Simulate(ctx, nil, pol, "tasks:write", "project/P/task/t1", map[string][]string{"task.sprint_id": {"s2"}}); res.Allowed {
		t.Fatalf("condition mismatch must not allow: %+v", res)
	}
	if res, _ = a.Simulate(ctx, nil, pol, "tasks:write", "project/P/task/t1", nil); res.Allowed {
		t.Fatalf("absent attribute must not satisfy a positive operator: %+v", res)
	}
	// With the principal's grants: its Deny wins over the policy's Allow.
	u1 := User("u1")
	res, err = a.Simulate(ctx, &u1, pol, "tasks:write", "project/P/task/t1", map[string][]string{"task.sprint_id": {"s1"}})
	if err != nil || res.Allowed {
		t.Fatalf("principal deny must win: %+v err=%v", res, err)
	}
	var sawDeny bool
	for _, m := range res.Matched {
		sawDeny = sawDeny || (m.RoleID == "role-1" && m.Effect == EffectDeny)
	}
	if !sawDeny {
		t.Fatalf("matched should list the principal's Deny: %+v", res.Matched)
	}
	// principal.id is taken from the principal, not from attrs.
	idPol := &Policy{Statements: []Statement{allowStmt([]string{"tasks:read"}, []string{"*"}, Conditions{"StringEquals": {"principal.id": {"u1"}}})}}
	if res, _ = a.Simulate(ctx, &u1, idPol, "tasks:read", "project/P/task/t1", map[string][]string{"principal.id": {"evil"}}); !res.Allowed {
		t.Fatalf("principal.id must come from the principal: %+v", res)
	}
	// Errors.
	store.err = errors.New("db down")
	if _, err = a.Simulate(ctx, &u1, pol, "tasks:read", "project/P", nil); err == nil {
		t.Fatal("store error must be returned")
	}
	bad := Principal{Type: "user"}
	if _, err = a.Simulate(ctx, &bad, pol, "tasks:read", "project/P", nil); err == nil {
		t.Fatal("invalid principal must be an error")
	}
}

func TestNarrowToProject(t *testing.T) {
	const P = "p1"
	cond := Conditions{"StringEquals": {"task.sprint_id": {"s1"}}}
	mk := func(effect Effect, sid string, actions []string, cond Conditions, res ...string) Statement {
		return Statement{Sid: sid, Effect: effect, Actions: actions, Resources: res, Conditions: cond}
	}
	tests := []struct {
		name string
		in   []Statement
		want []Statement
	}{
		{"star becomes the project", []Statement{mk(EffectAllow, "a", []string{"*"}, nil, "*")},
			[]Statement{mk(EffectAllow, "a", []string{"*"}, nil, "project/p1/*")}},
		{"project/* becomes the project", []Statement{mk(EffectAllow, "", []string{"tasks:read"}, nil, "project/*")},
			[]Statement{mk(EffectAllow, "", []string{"tasks:read"}, nil, "project/p1/*")}},
		{"mid wildcard project segment replaced", []Statement{mk(EffectAllow, "", []string{"tasks:read"}, nil, "project/*/task/*", "project/*/doc/d1")},
			[]Statement{mk(EffectAllow, "", []string{"tasks:read"}, nil, "project/p1/task/*", "project/p1/doc/d1")}},
		{"resources inside the project are kept", []Statement{mk(EffectAllow, "", []string{"tasks:read"}, nil, "project/p1", "project/p1/*", "project/p1/task/t")},
			[]Statement{mk(EffectAllow, "", []string{"tasks:read"}, nil, "project/p1", "project/p1/*", "project/p1/task/t")}},
		{"platform roots and other projects are dropped", []Statement{
			mk(EffectAllow, "", []string{"users:write"}, nil, "user/*", "role/*", "settings", "project", "project/p2/*"),
			mk(EffectAllow, "keep", []string{"tasks:read"}, nil, "user/*", "project/p1/task/*", "project/p2/task/*")},
			[]Statement{mk(EffectAllow, "keep", []string{"tasks:read"}, nil, "project/p1/task/*")}},
		{"deny narrowed the same way, a deny that does not reach is dropped", []Statement{
			mk(EffectDeny, "d1", []string{"tasks:delete"}, cond, "project/*/task/*"),
			mk(EffectDeny, "d2", []string{"tasks:delete"}, nil, "project/p2/task/*"),
			mk(EffectDeny, "d3", []string{"users:write"}, nil, "user/*")},
			[]Statement{mk(EffectDeny, "d1", []string{"tasks:delete"}, cond, "project/p1/task/*")}},
		{"conditions preserved", []Statement{mk(EffectAllow, "", []string{"tasks:read"}, cond, "*")},
			[]Statement{mk(EffectAllow, "", []string{"tasks:read"}, cond, "project/p1/*")}},
		{"duplicates collapse", []Statement{mk(EffectAllow, "", []string{"tasks:read"}, nil, "*", "project/*")},
			[]Statement{mk(EffectAllow, "", []string{"tasks:read"}, nil, "project/p1/*")}},
		{"nothing reaches the project: empty result", []Statement{mk(EffectAllow, "", []string{"users:write"}, nil, "user/*")},
			[]Statement{}},
		{"no statements", nil, []Statement{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := &Policy{Version: "v", Statements: tc.in}
			got := NarrowToProject(orig, P)
			if got.Version != "v" || len(got.Statements) != len(tc.want) {
				t.Fatalf("got %+v, want %+v", got.Statements, tc.want)
			}
			for i, w := range tc.want {
				g := got.Statements[i]
				if g.Sid != w.Sid || g.Effect != w.Effect || strings.Join(g.Resources, ",") != strings.Join(w.Resources, ",") ||
					strings.Join(g.Actions, ",") != strings.Join(w.Actions, ",") || len(g.Conditions) != len(w.Conditions) {
					t.Errorf("statement %d = %+v, want %+v", i, g, w)
				}
			}
		})
	}
	// the input is not modified
	in := &Policy{Statements: []Statement{{Effect: EffectAllow, Actions: []string{"a:b"}, Resources: []string{"*", "user/*"}}}}
	NarrowToProject(in, P)
	if strings.Join(in.Statements[0].Resources, ",") != "*,user/*" {
		t.Fatalf("input modified: %v", in.Statements[0].Resources)
	}
	if NarrowToProject(nil, P) != nil {
		t.Fatal("nil in, nil out")
	}
}

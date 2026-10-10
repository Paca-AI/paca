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

func TestGrantsCover(t *testing.T) {
	cond := Conditions{"StringEquals": {"task.sprint_id": {"s1"}}}
	tests := []struct {
		name   string
		grants []Grant
		cand   *Policy
		want   bool
	}{
		{"star on star grants anything",
			[]Grant{grantOf("", allowStmt([]string{"*"}, []string{"*"}, nil))},
			candidate(allowStmt([]string{"users:write", "tasks:*"}, []string{"project/P/task/*", "user/*"}, cond)), true},
		{"domain wildcard covers verb",
			[]Grant{grantOf("", allowStmt([]string{"tasks:*"}, []string{"*"}, nil))},
			candidate(allowStmt([]string{"tasks:read"}, []string{"project/P/*"}, nil)), true},
		{"domain wildcard covers itself",
			[]Grant{grantOf("", allowStmt([]string{"tasks:*"}, []string{"*"}, nil))},
			candidate(allowStmt([]string{"tasks:*"}, []string{"project/P/*"}, nil)), true},
		{"verb does not cover domain wildcard",
			[]Grant{grantOf("", allowStmt([]string{"tasks:read"}, []string{"*"}, nil))},
			candidate(allowStmt([]string{"tasks:*"}, []string{"project/P/*"}, nil)), false},
		{"domain wildcard does not cover star",
			[]Grant{grantOf("", allowStmt([]string{"tasks:*"}, []string{"*"}, nil))},
			candidate(allowStmt([]string{"*"}, []string{"project/P/*"}, nil)), false},
		{"other domain not covered",
			[]Grant{grantOf("", allowStmt([]string{"tasks:*"}, []string{"*"}, nil))},
			candidate(allowStmt([]string{"users:write"}, []string{"user/*"}, nil)), false},
		{"project wildcard covers task kind",
			[]Grant{grantOf("", allowStmt([]string{"tasks:*"}, []string{"project/P/*"}, nil))},
			candidate(allowStmt([]string{"tasks:read"}, []string{"project/P/task/*"}, nil)), true},
		{"specific task does not cover task wildcard",
			[]Grant{grantOf("", allowStmt([]string{"tasks:*"}, []string{"project/P/task/t1"}, nil))},
			candidate(allowStmt([]string{"tasks:read"}, []string{"project/P/task/*"}, nil)), false},
		{"task wildcard covers specific task",
			[]Grant{grantOf("", allowStmt([]string{"tasks:*"}, []string{"project/P/task/*"}, nil))},
			candidate(allowStmt([]string{"tasks:read"}, []string{"project/P/task/t1"}, nil)), true},
		{"other project not covered",
			[]Grant{grantOf("", allowStmt([]string{"tasks:*"}, []string{"project/P/*"}, nil))},
			candidate(allowStmt([]string{"tasks:read"}, []string{"project/Q/task/*"}, nil)), false},
		{"wildcard project not covered by one project",
			[]Grant{grantOf("", allowStmt([]string{"tasks:*"}, []string{"project/P/*"}, nil))},
			candidate(allowStmt([]string{"tasks:read"}, []string{"project/*/task/*"}, nil)), false},
		{"conditional caller allow does not cover",
			[]Grant{grantOf("", allowStmt([]string{"tasks:*"}, []string{"project/P/*"}, cond))},
			candidate(allowStmt([]string{"tasks:read"}, []string{"project/P/task/*"}, nil)), false},
		{"candidate conditions need the same coverage",
			[]Grant{grantOf("", allowStmt([]string{"tasks:read"}, []string{"project/P/*"}, nil))},
			candidate(allowStmt([]string{"tasks:write"}, []string{"project/P/task/*"}, cond)), false},
		{"candidate conditions do not widen the need",
			[]Grant{grantOf("", allowStmt([]string{"tasks:read"}, []string{"project/P/*"}, nil))},
			candidate(allowStmt([]string{"tasks:read"}, []string{"project/P/task/*"}, cond)), true},
		{"every pair must be covered",
			[]Grant{grantOf("", allowStmt([]string{"tasks:*"}, []string{"project/P/*"}, nil))},
			candidate(allowStmt([]string{"tasks:read"}, []string{"project/P/task/*", "project/Q/task/*"}, nil)), false},
		{"every statement must be covered",
			[]Grant{grantOf("", allowStmt([]string{"tasks:*"}, []string{"project/P/*"}, nil))},
			candidate(
				allowStmt([]string{"tasks:read"}, []string{"project/P/task/*"}, nil),
				allowStmt([]string{"users:write"}, []string{"project/P/task/*"}, nil)), false},
		{"candidate deny is always grantable",
			nil,
			candidate(denyStmt([]string{"*"}, []string{"*"}, nil)), true},
		{"policy without statements is grantable",
			nil, candidate(), true},
		{"nothing held, nothing grantable",
			nil, candidate(allowStmt([]string{"tasks:read"}, []string{"*"}, nil)), false},
		{"unparsable caller policy holds nothing",
			[]Grant{{RoleID: "x", Policy: nil}},
			candidate(allowStmt([]string{"tasks:read"}, []string{"*"}, nil)), false},
		{"nil candidate",
			[]Grant{grantOf("", allowStmt([]string{"*"}, []string{"*"}, nil))}, nil, false},

		// Denies of the caller.
		{"unconditional deny of the pair blocks",
			[]Grant{grantOf("",
				allowStmt([]string{"*"}, []string{"*"}, nil),
				denyStmt([]string{"tasks:write"}, []string{"project/P/task/*"}, nil))},
			candidate(allowStmt([]string{"tasks:write"}, []string{"project/P/task/*"}, nil)), false},
		{"conditional deny of the pair blocks too",
			[]Grant{grantOf("",
				allowStmt([]string{"*"}, []string{"*"}, nil),
				denyStmt([]string{"tasks:write"}, []string{"project/P/task/*"}, cond))},
			candidate(allowStmt([]string{"tasks:write"}, []string{"project/P/task/*"}, nil)), false},
		{"deny of one verb blocks the domain wildcard",
			[]Grant{grantOf("",
				allowStmt([]string{"*"}, []string{"*"}, nil),
				denyStmt([]string{"tasks:delete"}, []string{"*"}, nil))},
			candidate(allowStmt([]string{"tasks:*"}, []string{"project/P/task/*"}, nil)), false},
		{"deny of the domain blocks one verb",
			[]Grant{grantOf("",
				allowStmt([]string{"*"}, []string{"*"}, nil),
				denyStmt([]string{"tasks:*"}, []string{"*"}, nil))},
			candidate(allowStmt([]string{"tasks:read"}, []string{"project/P/task/*"}, nil)), false},
		{"deny of a specific task blocks the task wildcard",
			[]Grant{grantOf("",
				allowStmt([]string{"*"}, []string{"*"}, nil),
				denyStmt([]string{"tasks:read"}, []string{"project/P/task/t1"}, nil))},
			candidate(allowStmt([]string{"tasks:read"}, []string{"project/P/task/*"}, nil)), false},
		{"deny elsewhere does not block",
			[]Grant{grantOf("",
				allowStmt([]string{"*"}, []string{"*"}, nil),
				denyStmt([]string{"tasks:write"}, []string{"project/Q/task/*"}, nil),
				denyStmt([]string{"users:write"}, []string{"project/P/task/*"}, nil))},
			candidate(allowStmt([]string{"tasks:write"}, []string{"project/P/task/*"}, nil)), true},
		{"deny from another role blocks",
			[]Grant{
				grantOf("", allowStmt([]string{"*"}, []string{"*"}, nil)),
				grantOf("", denyStmt([]string{"environments:connect"}, []string{"project/*/environment/*"}, cond)),
			},
			candidate(allowStmt([]string{"environments:*"}, []string{"project/P/*"}, nil)), false},

		// Project-scoped callers.
		{"project-scoped allow covers inside its project",
			[]Grant{grantOf("P", allowStmt([]string{"*"}, []string{"project/P/*"}, nil))},
			candidate(allowStmt([]string{"tasks:*"}, []string{"project/P/task/*"}, nil)), true},
		{"project-scoped allow does not cover another project",
			[]Grant{grantOf("P", allowStmt([]string{"*"}, []string{"project/*/*"}, nil))},
			candidate(allowStmt([]string{"tasks:*"}, []string{"project/Q/task/*"}, nil)), false},
		{"project-scoped star on star stays inside its project",
			[]Grant{grantOf("P", allowStmt([]string{"*"}, []string{"*"}, nil))},
			candidate(allowStmt([]string{"tasks:*"}, []string{"project/Q/task/*"}, nil)), false},
		{"project-scoped allow does not cover platform resources",
			[]Grant{grantOf("P", allowStmt([]string{"*"}, []string{"*"}, nil))},
			candidate(allowStmt([]string{"users:write"}, []string{"user/*"}, nil)), false},
		{"project-scoped allow does not cover a wildcard project",
			[]Grant{grantOf("P", allowStmt([]string{"*"}, []string{"*"}, nil))},
			candidate(allowStmt([]string{"tasks:read"}, []string{"project/*/task/*"}, nil)), false},
		{"project-scoped deny blocks inside its project",
			[]Grant{
				grantOf("", allowStmt([]string{"tasks:*"}, []string{"*"}, nil)),
				grantOf("P", denyStmt([]string{"tasks:write"}, []string{"project/P/task/*"}, nil)),
			},
			candidate(allowStmt([]string{"tasks:write"}, []string{"project/P/task/*"}, nil)), false},
		{"project-scoped deny the caller can rewrite does not block",
			[]Grant{
				grantOf("", allowStmt([]string{"*"}, []string{"*"}, nil)),
				grantOf("P", denyStmt([]string{"tasks:write"}, []string{"project/P/task/*"}, nil)),
			},
			candidate(allowStmt([]string{"tasks:write"}, []string{"project/P/task/*"}, nil)), true},
		{"project-scoped deny does not apply in another project",
			[]Grant{
				grantOf("", allowStmt([]string{"*"}, []string{"*"}, nil)),
				grantOf("P", denyStmt([]string{"tasks:write"}, []string{"*"}, nil)),
			},
			candidate(allowStmt([]string{"tasks:write"}, []string{"project/Q/task/*"}, nil)), true},
		{"project-scoped deny blocks a pattern that could reach its project",
			[]Grant{
				grantOf("", allowStmt([]string{"tasks:*"}, []string{"*"}, nil)),
				grantOf("P", denyStmt([]string{"tasks:write"}, []string{"*"}, nil)),
			},
			candidate(allowStmt([]string{"tasks:write"}, []string{"project/*/task/*"}, nil)), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := GrantsCover(tc.grants, tc.cand); got != tc.want {
				t.Fatalf("GrantsCover = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResourcesOverlap(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"*", "project/P", true},
		{"project/P", "project/P", true},
		{"project/P/*", "project/P", true},
		{"project/P", "project/P/*", true},
		{"project/P/*", "project/P/task/*", true},
		{"project/P/task/*", "project/P/task/t1", true},
		{"project/P/task/t1", "project/P/task/t2", false},
		{"project/P/*", "project/Q/*", false},
		{"project/*/task/*", "project/P/*", true},
		{"project/*/task/*", "project/P/sprint/*", false},
		{"project/P", "project/P/task/t1", false},
		{"user/*", "role/*", false},
		{"project/*", "project/P/task/t1", true},
		{"project/*/task", "project/P/task/t1", false},
	}
	for _, tc := range tests {
		if got := resourcesOverlap(tc.a, tc.b); got != tc.want {
			t.Errorf("resourcesOverlap(%q,%q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
		if got := resourcesOverlap(tc.b, tc.a); got != tc.want {
			t.Errorf("resourcesOverlap(%q,%q) = %v, want %v (symmetry)", tc.b, tc.a, got, tc.want)
		}
	}
}

func TestAuthorizerCanGrant(t *testing.T) {
	ctx := context.Background()
	star := Policy{Statements: []Statement{allowStmt([]string{"*"}, []string{"*"}, nil)}}
	store := &fakeStore{grants: map[string][]Grant{
		"admin": {{RoleID: "r", Policy: &star}},
	}}
	a := NewAuthorizer(store, NewRegistry(), NewAttributeSchema())
	cand := candidate(allowStmt([]string{"users:write"}, []string{"user/*"}, nil))

	if ok, err := a.CanGrant(ctx, User("admin"), cand); err != nil || !ok {
		t.Fatalf("star caller: ok=%v err=%v, want true", ok, err)
	}
	if ok, err := a.CanGrant(ctx, User("nobody"), cand); err != nil || ok {
		t.Fatalf("caller with no grants: ok=%v err=%v, want false", ok, err)
	}
	if ok, err := a.CanGrant(ctx, Principal{}, cand); err != nil || ok {
		t.Fatalf("invalid principal: ok=%v err=%v, want false", ok, err)
	}
	if ok, err := a.CanGrant(ctx, User("admin"), nil); err != nil || ok {
		t.Fatalf("nil policy: ok=%v err=%v, want false", ok, err)
	}
	// Fail closed: a store error is an error and never true.
	store.err = errors.New("db down")
	if ok, err := a.CanGrant(ctx, User("admin"), cand); err == nil || ok {
		t.Fatalf("store error: ok=%v err=%v, want false and an error", ok, err)
	}
}

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

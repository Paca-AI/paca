package iam

import (
	"context"
	"math/rand"
	"testing"
)

// TestCompileScopeMatchesEvaluate checks that the compiled predicate agrees
// with Evaluate for randomly generated policies, so the SQL a repository
// derives from it filters exactly what a per-resource check would allow.
func TestCompileScopeMatchesEvaluate(t *testing.T) {
	schema := NewAttributeSchema()
	rng := rand.New(rand.NewSource(1))
	pick := func(xs ...string) string { return xs[rng.Intn(len(xs))] }

	ids := []string{"t1", "t2", "t3"}
	sprints := []string{"s1", "s2"}
	projects := []string{"p1", "p2"}
	patterns := []string{
		"*", "project/*", "project/p1", "project/p1/*", "project/p2/*",
		"project/*/task/*", "project/p1/task/*", "project/p1/task/t1", "project/p1/task/t2",
		"project/p1/doc/*", "project/*/task/t3", "project/p1/task/t1/x", "user/*", "project/p1/task",
		"project/p1/task/t1/*", "project/p1/task/*/*", "project/*/task/t2/*",
	}
	conds := func() Conditions {
		switch rng.Intn(7) {
		case 0:
			return nil
		case 1:
			return Conditions{"StringEquals": {"task.sprint_id": {pick(sprints...)}}}
		case 2:
			return Conditions{"StringNotEquals": {"task.sprint_id": {pick(sprints...)}}}
		case 3:
			return Conditions{"In": {"task.assignee_id": {"u1", "u2"}}}
		case 4:
			return Conditions{"NotIn": {"principal.id": {"u1"}}}
		case 5:
			return Conditions{"StringEquals": {"doc.folder_id": {"f1"}}}
		default:
			return Conditions{"Bogus": {"task.sprint_id": {"s1"}}}
		}
	}

	for iter := 0; iter < 3000; iter++ {
		var grants []Grant
		for g := 0; g < 1+rng.Intn(3); g++ {
			var sts []Statement
			for s := 0; s < 1+rng.Intn(3); s++ {
				sts = append(sts, Statement{
					Effect:     Effect(pick("Allow", "Allow", "Deny")),
					Actions:    []string{pick("tasks:read", "tasks:*", "*", "docs:read")},
					Resources:  []string{pick(patterns...), pick(patterns...)},
					Conditions: conds(),
				})
			}
			gr := Grant{RoleID: "r", Policy: &Policy{Statements: sts}}
			if rng.Intn(3) == 0 {
				gr.ProjectID = pick(projects...)
			}
			grants = append(grants, gr)
		}
		p := Principal{Type: "user", ID: pick("u1", "u2")}
		projectID := pick(projects...)
		node := CompileScope(grants, p, "tasks:read", projectID, "task", schema)

		for _, id := range ids {
			for _, sprint := range append([]string{""}, sprints...) {
				for _, assignees := range [][]string{nil, {"u1"}, {"u3"}} {
					attrs := map[string][]string{
						"principal.id": {p.ID}, "principal.type": {p.Type}, "resource.id": {id},
						"task.assignee_id": assignees,
					}
					if sprint != "" {
						attrs["task.sprint_id"] = []string{sprint}
					}
					want := Evaluate(grants, Request{
						Action: "tasks:read", Resource: "project/" + projectID + "/task/" + id, Attrs: attrs,
					}).Allowed
					if got := node.Eval(attrs); got != want {
						t.Fatalf("iter %d: id=%s sprint=%q assignees=%v: scope=%v evaluate=%v\ngrants=%+v",
							iter, id, sprint, assignees, got, want, grants)
					}
				}
			}
		}
	}
}

func TestCompileScopeShapes(t *testing.T) {
	schema := NewAttributeSchema()
	u := Principal{Type: "user", ID: "u1"}
	allow := func(res string, c Conditions) Grant {
		return Grant{Policy: &Policy{Statements: []Statement{{Effect: EffectAllow, Actions: []string{"agents:read"}, Resources: []string{res}, Conditions: c}}}}
	}

	if n := CompileScope(nil, u, "agents:read", "p1", "agent", schema); !n.DeniesAll() {
		t.Fatal("no grants must deny all")
	}
	if n := CompileScope([]Grant{allow("*", nil)}, u, "agents:read", "p1", "agent", schema); !n.Unrestricted() {
		t.Fatal("an unconditional Allow on * must be unrestricted")
	}
	// A platform-wide project/* role attached with project_id = p1 never reaches p2.
	g := allow("project/*", nil)
	g.ProjectID = "p1"
	if n := CompileScope([]Grant{g}, u, "agents:read", "p2", "agent", schema); !n.DeniesAll() {
		t.Fatal("a grant scoped to p1 must not reach p2")
	}
	// A condition on another kind's attribute is absent: positive fails.
	if n := CompileScope([]Grant{allow("*", Conditions{"StringEquals": {"task.sprint_id": {"s1"}}})}, u, "agents:read", "p1", "agent", schema); !n.DeniesAll() {
		t.Fatal("positive condition on an absent attribute must deny")
	}
	n := CompileScope([]Grant{allow("project/p1/agent/a1", nil)}, u, "agents:read", "p1", "agent", schema)
	if n.Unrestricted() || n.DeniesAll() || !n.Eval(map[string][]string{"resource.id": {"a1"}}) || n.Eval(map[string][]string{"resource.id": {"a2"}}) {
		t.Fatalf("single-id allow compiled to %+v", n)
	}
}

func TestScopeContext(t *testing.T) {
	ctx := context.Background()
	if _, ok := ScopeFrom(ctx, "task"); ok {
		t.Fatal("no scope expected")
	}
	ctx = WithScope(ctx, "task", False())
	if n, ok := ScopeFrom(ctx, "task"); !ok || !n.DeniesAll() {
		t.Fatal("scope not carried")
	}
	if _, ok := ScopeFrom(ctx, "doc"); ok {
		t.Fatal("scope must be per kind")
	}
}

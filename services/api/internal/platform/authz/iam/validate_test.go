package iam

import (
	"strings"
	"testing"
)

func okStmt() Statement {
	return Statement{Effect: EffectAllow, Actions: []string{"tasks:read"}, Resources: []string{"project/1"}}
}

func TestEveryConditionOperatorIsImplemented(t *testing.T) {
	for _, op := range ConditionOperators {
		for _, got := range [][]string{{"x"}, nil} {
			if _, known := evalOp(op, got, ValueList{"x"}); !known {
				t.Errorf("operator %q is listed in ConditionOperators but evalOp does not implement it", op)
			}
		}
		if !knownOperator(op) {
			t.Errorf("knownOperator(%q) = false", op)
		}
	}
}

func TestKnownOperatorRejectsNonMembers(t *testing.T) {
	for _, op := range []string{"Nope", "", "stringequals", "StringEquals "} {
		if knownOperator(op) {
			t.Errorf("knownOperator(%q) = true, want false", op)
		}
	}
}

func TestValidate(t *testing.T) {
	r := NewRegistry()
	mk := func(f func(s *Statement)) *Policy {
		s := okStmt()
		f(&s)
		return &Policy{Version: "1", Statements: []Statement{s}}
	}
	many := &Policy{}
	for i := 0; i < 101; i++ {
		many.Statements = append(many.Statements, okStmt())
	}
	exactly100 := &Policy{}
	for i := 0; i < 100; i++ {
		exactly100.Statements = append(exactly100.Statements, okStmt())
	}

	cases := []struct {
		name     string
		p        *Policy
		wantPath string // "" = no issues
	}{
		{"valid", mk(func(s *Statement) {}), ""},
		{"valid wildcards", mk(func(s *Statement) { s.Actions = []string{"*", "tasks:*"}; s.Resources = []string{"*", "project/*/x"} }), ""},
		{"valid conditions", mk(func(s *Statement) {
			s.Conditions = Conditions{"StringEquals": {"principal.id": {"a"}}, "Bool": {"resource.id": {"true"}}}
		}), ""},
		{"empty statements", &Policy{Version: "1"}, ""},
		{"exactly 100", exactly100, ""},
		{"nil policy", nil, "policy"},
		{"too many", many, "statements"},
		{"bad effect", mk(func(s *Statement) { s.Effect = "x" }), "statements[0].effect"},
		{"unknown action", mk(func(s *Statement) { s.Actions = []string{"tasks:read", "nope:write"} }), "statements[0].actions[1]"},
		{"unknown domain wildcard", mk(func(s *Statement) { s.Actions = []string{"nope:*"} }), "statements[0].actions[0]"},
		{"empty actions", mk(func(s *Statement) { s.Actions = nil }), "statements[0].actions"},
		{"empty resources", mk(func(s *Statement) { s.Resources = nil }), "statements[0].resources"},
		{"empty segment", mk(func(s *Statement) { s.Resources = []string{"project//x"} }), "statements[0].resources[0]"},
		{"unknown root", mk(func(s *Statement) { s.Resources = []string{"bogus/1"} }), "statements[0].resources[0]"},
		{"empty resource", mk(func(s *Statement) { s.Resources = []string{""} }), "statements[0].resources[0]"},
		{"trailing slash", mk(func(s *Statement) { s.Resources = []string{"project/"} }), "statements[0].resources[0]"},
		{"unknown operator", mk(func(s *Statement) { s.Conditions = Conditions{"Nope": {"principal.id": {"a"}}} }), "statements[0].conditions.Nope.principal.id"},
		{"unknown key", mk(func(s *Statement) { s.Conditions = Conditions{"StringEquals": {"bogus.key": {"a"}}} }), "statements[0].conditions.StringEquals.bogus.key"},
		{"bool bad operand", mk(func(s *Statement) { s.Conditions = Conditions{"Bool": {"principal.id": {"yes"}}} }), "statements[0].conditions.Bool.principal.id"},
		{"empty operand", mk(func(s *Statement) { s.Conditions = Conditions{"In": {"principal.id": {}}} }), "statements[0].conditions.In.principal.id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := Validate(tc.p, r, NewAttributeSchema())
			if tc.wantPath == "" {
				if len(issues) != 0 {
					t.Fatalf("want no issues, got %v", issues)
				}
				return
			}
			for _, is := range issues {
				if is.Path == tc.wantPath && is.Message != "" {
					return
				}
			}
			t.Fatalf("want issue at %q, got %v", tc.wantPath, issues)
		})
	}
}

func TestValidateNilPolicyMessage(t *testing.T) {
	is := Validate(nil, NewRegistry(), NewAttributeSchema())
	if len(is) != 1 || !strings.Contains(is[0].Message, "policy is required") {
		t.Fatalf("got %v", is)
	}
}

func TestValidateNilRegistryFailsClosed(t *testing.T) {
	if len(Validate(&Policy{Statements: []Statement{okStmt()}}, nil, NewAttributeSchema())) == 0 {
		t.Fatal("nil registry must not validate actions as OK")
	}
}

func TestValidateReportsAllIssues(t *testing.T) {
	p := &Policy{Statements: []Statement{
		{Effect: "x", Actions: []string{"bad:a"}, Resources: []string{"bogus"}},
	}}
	if n := len(Validate(p, NewRegistry(), NewAttributeSchema())); n != 3 {
		t.Fatalf("want 3 issues, got %d", n)
	}
}

func TestValidateNilSchemaFailsClosed(t *testing.T) {
	s := okStmt()
	s.Conditions = Conditions{"StringEquals": {"principal.id": {"a"}}}
	issues := Validate(&Policy{Statements: []Statement{s}}, NewRegistry(), nil)
	if len(issues) != 1 || issues[0].Path != "statements[0].conditions.StringEquals.principal.id" {
		t.Fatalf("got %v", issues)
	}
	// no conditions: nil schema is fine
	if is := Validate(&Policy{Statements: []Statement{okStmt()}}, NewRegistry(), nil); len(is) != 0 {
		t.Fatalf("got %v", is)
	}
}

func TestValidateAttributeKinds(t *testing.T) {
	cond := func(key string) Conditions { return Conditions{"StringEquals": {key: {"v"}}} }
	mk := func(key string, resources ...string) *Policy {
		return &Policy{Statements: []Statement{{
			Effect: EffectAllow, Actions: []string{"tasks:read"}, Resources: resources, Conditions: cond(key),
		}}}
	}
	path := "statements[0].conditions.StringEquals."
	cases := []struct {
		name      string
		p         *Policy
		wantIssue string // "" = valid
	}{
		{"task key on task resources", mk("task.sprint_id", "project/P/task/*"), ""},
		{"task key on star", mk("task.sprint_id", "*"), ""},
		{"task key on star among others", mk("task.sprint_id", "project/P/agent/*", "*"), ""},
		{"task key on project wildcard", mk("task.sprint_id", "project/*"), ""},
		{"task key on project/P/*", mk("task.sprint_id", "project/P/*"), ""},
		{"task key on mixed kinds (one matches)", mk("task.sprint_id", "project/P/agent/*", "project/P/task/t1"), ""},
		{"task key on agent only", mk("task.sprint_id", "project/*/agent/*"), path + "task.sprint_id"},
		{"task key on project itself", mk("task.sprint_id", "project/P"), path + "task.sprint_id"},
		{"task key on user", mk("task.sprint_id", "user/u1"), path + "task.sprint_id"},
		{"generic key anywhere", mk("principal.id", "project/*/agent/*"), ""},
		{"generic resource key on user", mk("resource.id", "user/*"), ""},
		{"doc multi key", mk("doc.ancestor_folder_ids", "project/P/doc/*"), ""},
		{"doc key on task", mk("doc.folder_id", "project/P/task/*"), path + "doc.folder_id"},
		{"view key on view resources", mk("view.sprint_id", "project/P/view/*"), ""},
		{"view key on project/P/*", mk("view.sprint_id", "project/P/*"), ""},
		{"view key on task resources is rejected", mk("view.sprint_id", "project/P/task/*"), path + "view.sprint_id"},
		{"task key on view resources is rejected", mk("task.sprint_id", "project/P/view/*"), path + "task.sprint_id"},
		{"unknown key", mk("task.bogus", "*"), path + "task.bogus"},
		{"removed key resource.owner_id (unknown attribute)", mk("resource.owner_id", "*"), path + "resource.owner_id"},
		{"removed key resource.type", mk("resource.type", "*"), path + "resource.type"},
		{"agent.environment_id on agents", mk("agent.environment_id", "project/P/agent/*"), ""},
		{"environment.type on environments", mk("environment.type", "project/P/environment/e"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := Validate(tc.p, NewRegistry(), NewAttributeSchema())
			if tc.wantIssue == "" {
				if len(issues) != 0 {
					t.Fatalf("want none, got %v", issues)
				}
				return
			}
			if len(issues) != 1 || issues[0].Path != tc.wantIssue {
				t.Fatalf("want one issue at %q, got %v", tc.wantIssue, issues)
			}
		})
	}
}

func TestConditionKeysVarRemoved(t *testing.T) {
	// ConditionKeys is deleted (a reference would not compile); the attribute
	// schema is the only source of keys. ConditionOperators must remain.
	if len(ConditionOperators) == 0 {
		t.Fatal("ConditionOperators must stay")
	}
}

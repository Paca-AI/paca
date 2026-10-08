package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

func condNode(key, op string, vals ...string) *iam.Node {
	return &iam.Node{Kind: iam.NodeCond, Key: key, Op: op, Values: iam.ValueList(vals)}
}

func TestAnnotationScopeColumnsExpressOnlyResourceID(t *testing.T) {
	sink := &argList{}
	got, err := nodeSQL(condNode("resource.id", "In", "a1", "a2"), annotationScopeColumns, sink)
	if err != nil {
		t.Fatal(err)
	}
	if want := "COALESCE((pa.id)::text = ANY($1::text[]), FALSE)"; got != want {
		t.Fatalf("sql = %q, want %q", got, want)
	}
	if len(sink.args) != 1 {
		t.Fatalf("args = %v", sink.args)
	}
	// Negated forms keep NOT over a non-NULL expression.
	got, err = nodeSQL(iam.Not(condNode("resource.id", "StringEquals", "a1")), annotationScopeColumns, &argList{})
	if err != nil || !strings.HasPrefix(got, "NOT (") {
		t.Fatalf("sql = %q (%v)", got, err)
	}
	// Annotations declare no other attribute: a policy naming one cannot be
	// pushed down, which must be an error rather than an unfiltered list.
	if _, err := nodeSQL(condNode("annotation.environment_id", "In", "e"), annotationScopeColumns, &argList{}); err == nil {
		t.Fatal("an attribute with no SQL mapping must be an error")
	}
}

func TestAnnotationScopeAnd(t *testing.T) {
	ctx := func(n *iam.Node) context.Context { return iam.WithScope(context.Background(), "annotation", n) }
	for name, c := range map[string]struct {
		ctx        context.Context
		wantClause bool
		wantNone   bool
		wantErr    bool
	}{
		"no scope attached (workers)": {context.Background(), false, false, false},
		"unrestricted":                {ctx(iam.True()), false, false, false},
		"deny all":                    {ctx(iam.False()), false, true, false},
		"restricted":                  {ctx(condNode("resource.id", "In", "a")), true, false, false},
		"a scope for another kind":    {iam.WithScope(context.Background(), "task", iam.False()), false, false, false},
		"unmappable":                  {ctx(condNode("annotation.nope", "In", "a")), false, false, true},
	} {
		sink := &argList{args: []any{"base"}}
		clause, none, err := annotationScopeAnd(c.ctx, sink)
		if (err != nil) != c.wantErr || none != c.wantNone || (clause != "") != c.wantClause {
			t.Errorf("%s: clause=%q none=%v err=%v", name, clause, none, err)
		}
		if c.wantClause && (!strings.HasPrefix(clause, " AND ") || !strings.Contains(clause, "$2")) {
			t.Errorf("%s: clause %q must be an AND suffix numbered after the existing argument", name, clause)
		}
	}
}

func TestProjectScopesSQL(t *testing.T) {
	const p1, p2, p3, p4 = "p1", "p2", "p3", "p4"
	scoped := func(m map[string]*iam.Node) context.Context {
		return iam.WithProjectScopes(context.Background(), "task", m)
	}

	t.Run("no per-project scopes attached means unrestricted", func(t *testing.T) {
		clause, none, err := projectScopesSQL(context.Background(), "task", taskScopeColumns, "tasks.project_id", &argList{})
		if clause != "" || none || err != nil {
			t.Fatalf("clause=%q none=%v err=%v", clause, none, err)
		}
	})
	t.Run("a project absent from the map, or deny-all, matches nothing", func(t *testing.T) {
		for _, m := range []map[string]*iam.Node{{}, {p1: iam.False()}, {p1: iam.False(), p2: iam.False()}} {
			clause, none, err := projectScopesSQL(scoped(m), "task", taskScopeColumns, "tasks.project_id", &argList{})
			if clause != "" || !none || err != nil {
				t.Fatalf("%v: clause=%q none=%v err=%v", m, clause, none, err)
			}
		}
	})
	t.Run("unrestricted projects share one IN list; restricted ones get their own predicate", func(t *testing.T) {
		sink := &argList{}
		clause, none, err := projectScopesSQL(scoped(map[string]*iam.Node{
			p4: iam.True(), p3: condNode("task.sprint_id", "In", "s5"), p1: iam.True(), p2: iam.False(),
		}), "task", taskScopeColumns, "tasks.project_id", sink)
		if none || err != nil {
			t.Fatalf("none=%v err=%v", none, err)
		}
		// Sorted by project id: p1 (open), p3 (restricted: id then sprint), p4 (open).
		if got, want := sink.args[0], any("p1"); got != want {
			t.Fatalf("first arg = %v", got)
		}
		if !strings.HasPrefix(clause, "(tasks.project_id IN (") || !strings.Contains(clause, "tasks.project_id = $") ||
			!strings.Contains(clause, "tasks.sprint_id") || strings.Count(clause, " OR ") != 1 {
			t.Fatalf("clause = %q", clause)
		}
		for _, a := range sink.args {
			if a == "p2" {
				t.Fatalf("deny-all project p2 leaked into the arguments: %v", sink.args)
			}
		}
	})
	t.Run("the statement is deterministic", func(t *testing.T) {
		m := map[string]*iam.Node{p1: condNode("task.sprint_id", "In", "a"), p2: condNode("task.sprint_id", "In", "b"), p3: iam.True()}
		first, _, _ := projectScopesSQL(scoped(m), "task", taskScopeColumns, "tasks.project_id", &argList{})
		for i := 0; i < 20; i++ {
			again, _, _ := projectScopesSQL(scoped(m), "task", taskScopeColumns, "tasks.project_id", &argList{})
			if again != first {
				t.Fatalf("clause changed between calls:\n%s\n%s", first, again)
			}
		}
	})
	t.Run("an unmappable condition is an error, not an unfiltered project", func(t *testing.T) {
		_, _, err := projectScopesSQL(scoped(map[string]*iam.Node{p1: condNode("task.nope", "In", "x")}), "task", taskScopeColumns, "tasks.project_id", &argList{})
		if err == nil {
			t.Fatal("expected an error")
		}
	})
}

package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// scopeAttr is the SQL behind one condition attribute of a resource kind: a
// scalar expression (NULL means the attribute is absent) or an expression
// yielding a text[] (empty means absent).
type scopeAttr struct{ scalar, multi string }

// scopeColumns maps condition keys, including "resource.id", to their SQL for
// one list query. Every attribute the schema declares for the kind must be
// present: a policy that references one the query cannot express is an error
// (the request fails closed), never an unfiltered list.
type scopeColumns map[string]scopeAttr

// argSink collects the bind arguments of a query being built.
type argSink interface{ addArg(v any) string }

func (b *queryBuilder) addArg(v any) string {
	p := b.placeholder()
	b.args = append(b.args, v)
	return p
}

// argList is an argSink for queries built from a plain argument slice whose
// existing placeholders are $1..$len(args).
type argList struct{ args []any }

func (a *argList) addArg(v any) string {
	a.args = append(a.args, v)
	return fmt.Sprintf("$%d", len(a.args))
}

// Per-kind SQL for the built-in attributes. Keep in step with
// NewIAMAttributeLoaders: a loader answers one resource, these answer all of
// them in one query, and the two must agree.
var (
	taskScopeColumns = scopeColumns{
		"resource.id":    {scalar: "tasks.id"},
		"task.sprint_id": {scalar: "tasks.sprint_id"},
		"task.status_id": {scalar: "tasks.status_id"},
		"task.type_id":   {scalar: "tasks.task_type_id"},
		// The member's principal id (user or agent), comparable with principal.id.
		"task.assignee_id": {multi: `ARRAY(SELECT COALESCE(pm.user_id, pm.agent_id)::text
			FROM task_assignees ta JOIN project_members pm ON pm.id = ta.member_id
			WHERE ta.task_id = tasks.id AND pm.deleted_at IS NULL)`},
	}
	docScopeColumns = scopeColumns{
		"resource.id":   {scalar: "documents.id"},
		"doc.folder_id": {scalar: "documents.folder_id"},
		// The folder plus every ancestor; UNION ends a corrupt parent cycle.
		"doc.ancestor_folder_ids": {multi: `ARRAY(WITH RECURSIVE anc(id, parent_id) AS (
				SELECT f.id, f.parent_id FROM doc_folders f WHERE f.id = documents.folder_id
			  UNION
				SELECT f.id, f.parent_id FROM doc_folders f JOIN anc a ON f.id = a.parent_id
			) SELECT id::text FROM anc)`},
	}
	agentScopeColumns = scopeColumns{
		"resource.id":          {scalar: "a.id"},
		"agent.environment_id": {scalar: "a.default_environment_id"},
	}
	environmentScopeColumns = scopeColumns{
		"resource.id":      {scalar: "environments.id"},
		"environment.type": {scalar: "environments.backend"},
	}
	sprintScopeColumns = scopeColumns{
		"resource.id": {scalar: "sprints.id"},
	}
	viewScopeColumns = scopeColumns{
		"resource.id":    {scalar: "sprint_views.id"},
		"view.sprint_id": {scalar: "sprint_views.sprint_id"},
	}
	workflowScopeColumns = scopeColumns{
		"resource.id": {scalar: "automations.id"},
	}
	// Annotations declare no attributes of their own in the schema, so only
	// the generic resource.id is expressible (an Allow/Deny naming one
	// annotation id); a condition on any other key fails the request closed.
	annotationScopeColumns = scopeColumns{
		"resource.id": {scalar: "pa.id"},
	}
	conversationScopeColumns = scopeColumns{
		"resource.id":                 {scalar: "agent_conversations.id"},
		"conversation.environment_id": {scalar: "agent_conversations.environment_id"},
	}
)

// scopeFromContext returns the scope attached to ctx for kind. ok is false
// when none is attached (workers and internal callers), meaning unrestricted.
func scopeFromContext(ctx context.Context, kind string) (*iam.Node, bool) {
	return iam.ScopeFrom(ctx, kind)
}

// scopeSQL translates the scope attached to ctx for kind into a boolean SQL
// expression over cols, appending its bind arguments to sink. none is true when
// the scope allows nothing, so the caller returns an empty result without
// querying. An empty clause with none == false means unrestricted.
func scopeSQL(ctx context.Context, kind string, cols scopeColumns, sink argSink) (clause string, none bool, err error) {
	n, ok := scopeFromContext(ctx, kind)
	if !ok || n.Unrestricted() {
		return "", false, nil
	}
	if n.DeniesAll() {
		return "", true, nil
	}
	clause, err = nodeSQL(n, cols, sink)
	if err != nil {
		return "", false, fmt.Errorf("iam scope for %s: %w", kind, err)
	}
	return clause, false, nil
}

// projectScopesSQL translates the per-project scopes attached to ctx for kind
// (iam.WithProjectScopes) into a boolean expression over projectCol and cols,
// for a query that spans projects: one alternative per project, OR-ed, where a
// project without a scope, or with a deny-all one, contributes nothing and so
// is not matched. Projects whose scope is unrestricted share one IN list.
// none is true when no project is matched, so the caller returns an empty
// result without querying; an empty clause with none == false means no
// per-project scopes are attached (workers and internal callers).
func projectScopesSQL(ctx context.Context, kind string, cols scopeColumns, projectCol string, sink argSink) (clause string, none bool, err error) {
	byProject, ok := iam.ProjectScopesFrom(ctx, kind)
	if !ok {
		return "", false, nil
	}
	pids := make([]string, 0, len(byProject))
	for pid := range byProject {
		pids = append(pids, pid)
	}
	sort.Strings(pids) // a stable statement text
	var open, alts []string
	for _, pid := range pids {
		node := byProject[pid]
		if node.DeniesAll() {
			continue
		}
		pp := sink.addArg(pid)
		if node.Unrestricted() {
			open = append(open, pp)
			continue
		}
		cond, err := nodeSQL(node, cols, sink)
		if err != nil {
			return "", false, fmt.Errorf("iam scope for %s: %w", kind, err)
		}
		alts = append(alts, "("+projectCol+" = "+pp+" AND "+cond+")")
	}
	if len(open) > 0 {
		alts = append([]string{projectCol + " IN (" + strings.Join(open, ",") + ")"}, alts...)
	}
	if len(alts) == 0 {
		return "", true, nil
	}
	return "(" + strings.Join(alts, " OR ") + ")", false, nil
}

func nodeSQL(n *iam.Node, cols scopeColumns, sink argSink) (string, error) {
	switch n.Kind {
	case iam.NodeTrue:
		return "TRUE", nil
	case iam.NodeFalse:
		return "FALSE", nil
	case iam.NodeNot:
		inner, err := nodeSQL(n.Kids[0], cols, sink)
		return "NOT (" + inner + ")", err
	case iam.NodeAnd, iam.NodeOr:
		sep := " AND "
		if n.Kind == iam.NodeOr {
			sep = " OR "
		}
		parts := make([]string, len(n.Kids))
		for i, k := range n.Kids {
			p, err := nodeSQL(k, cols, sink)
			if err != nil {
				return "", err
			}
			parts[i] = p
		}
		return "(" + strings.Join(parts, sep) + ")", nil
	default:
		return condSQL(n, cols, sink)
	}
}

// condSQL renders one condition. Every form yields TRUE or FALSE, never NULL,
// so a NOT above it behaves like the evaluator's: an absent attribute fails a
// positive operator and satisfies a negated one.
func condSQL(n *iam.Node, cols scopeColumns, sink argSink) (string, error) {
	attr, ok := cols[n.Key]
	if !ok {
		return "", fmt.Errorf("no SQL mapping for attribute %q", n.Key)
	}
	vals := []string(n.Values)
	if vals == nil {
		vals = []string{}
	}
	switch n.Op {
	case "StringEquals", "In", "StringNotEquals", "NotIn":
		p := sink.addArg(vals) + "::text[]"
		var positive string
		if attr.scalar != "" {
			positive = fmt.Sprintf("COALESCE((%s)::text = ANY(%s), FALSE)", attr.scalar, p)
		} else {
			positive = fmt.Sprintf("COALESCE((%s) && %s, FALSE)", attr.multi, p)
		}
		if n.Op == "StringNotEquals" || n.Op == "NotIn" {
			return "NOT " + positive, nil
		}
		return positive, nil
	case "StringLike":
		likes := make([]string, len(vals))
		for i, v := range vals {
			likes[i] = globToLike(v)
		}
		p := sink.addArg(likes) + "::text[]"
		if attr.scalar != "" {
			return fmt.Sprintf("COALESCE((%s)::text LIKE ANY(%s), FALSE)", attr.scalar, p), nil
		}
		return fmt.Sprintf("EXISTS (SELECT 1 FROM unnest(%s) AS v WHERE v LIKE ANY(%s))", attr.multi, p), nil
	case "Bool":
		if len(vals) != 1 {
			return "FALSE", nil
		}
		p := sink.addArg(vals[0])
		if attr.scalar != "" {
			return fmt.Sprintf("COALESCE((%s)::text = %s::text, FALSE)", attr.scalar, p), nil
		}
		return fmt.Sprintf("COALESCE(%s::text = ANY(%s), FALSE)", p, attr.multi), nil
	}
	return "", fmt.Errorf("unsupported operator %q", n.Op)
}

// globToLike converts a StringLike pattern ("*" any run, "?" one character)
// to a LIKE pattern, escaping LIKE's own wildcards.
func globToLike(glob string) string {
	var sb strings.Builder
	for _, r := range glob {
		switch r {
		case '*':
			sb.WriteByte('%')
		case '?':
			sb.WriteByte('_')
		case '%', '_', '\\':
			sb.WriteByte('\\')
			sb.WriteRune(r)
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

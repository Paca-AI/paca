package iam

import (
	"context"
	"strings"
)

// NodeKind is the shape of a Node in a compiled list scope.
type NodeKind int

const (
	// NodeTrue holds for every resource.
	NodeTrue NodeKind = iota
	// NodeFalse holds for none.
	NodeFalse
	// NodeAnd holds when every child holds.
	NodeAnd
	// NodeOr holds when any child holds.
	NodeOr
	// NodeNot holds when its single child does not.
	NodeNot
	// NodeCond is one attribute condition: Op over the values of Key.
	NodeCond
)

// Node is a boolean predicate over one resource kind's attributes. A
// repository translates it to SQL so a list endpoint is filtered by the
// database before pagination, counts and sums are computed — never after.
type Node struct {
	Kind   NodeKind
	Kids   []*Node
	Key    string    // NodeCond: attribute key, e.g. "task.sprint_id" or "resource.id"
	Op     string    // NodeCond: one of ConditionOperators
	Values ValueList // NodeCond: operand
}

var (
	nodeTrue  = &Node{Kind: NodeTrue}
	nodeFalse = &Node{Kind: NodeFalse}
)

// True returns the predicate that holds for every resource.
func True() *Node { return nodeTrue }

// False returns the predicate that holds for no resource.
func False() *Node { return nodeFalse }

// And combines predicates, folding constants.
func And(kids ...*Node) *Node {
	out := make([]*Node, 0, len(kids))
	for _, k := range kids {
		switch k.Kind {
		case NodeTrue:
		case NodeFalse:
			return nodeFalse
		default:
			out = append(out, k)
		}
	}
	switch len(out) {
	case 0:
		return nodeTrue
	case 1:
		return out[0]
	}
	return &Node{Kind: NodeAnd, Kids: out}
}

// Or combines predicates, folding constants.
func Or(kids ...*Node) *Node {
	out := make([]*Node, 0, len(kids))
	for _, k := range kids {
		switch k.Kind {
		case NodeFalse:
		case NodeTrue:
			return nodeTrue
		default:
			out = append(out, k)
		}
	}
	switch len(out) {
	case 0:
		return nodeFalse
	case 1:
		return out[0]
	}
	return &Node{Kind: NodeOr, Kids: out}
}

// Not negates a predicate, folding constants.
func Not(k *Node) *Node {
	switch k.Kind {
	case NodeTrue:
		return nodeFalse
	case NodeFalse:
		return nodeTrue
	case NodeNot:
		return k.Kids[0]
	case NodeAnd, NodeOr, NodeCond:
		// Composite and condition nodes cannot be folded further.
	}
	return &Node{Kind: NodeNot, Kids: []*Node{k}}
}

// Unrestricted reports whether the predicate holds for every resource, so a
// repository may skip the clause entirely.
func (n *Node) Unrestricted() bool { return n == nil || n.Kind == NodeTrue }

// DeniesAll reports whether the predicate holds for no resource, so a
// repository may return an empty result without querying.
func (n *Node) DeniesAll() bool { return n != nil && n.Kind == NodeFalse }

// Keys returns every attribute key the predicate tests.
func (n *Node) Keys() map[string]struct{} {
	keys := map[string]struct{}{}
	var walk func(*Node)
	walk = func(m *Node) {
		if m == nil {
			return
		}
		if m.Kind == NodeCond {
			keys[m.Key] = struct{}{}
		}
		for _, k := range m.Kids {
			walk(k)
		}
	}
	walk(n)
	return keys
}

// Eval evaluates the predicate against one resource's attributes with the
// same operator semantics as Evaluate. Repositories do not use it for
// filtering; it defines the meaning the SQL translation must preserve.
func (n *Node) Eval(attrs map[string][]string) bool {
	if n == nil {
		return true
	}
	switch n.Kind {
	case NodeTrue:
		return true
	case NodeFalse:
		return false
	case NodeAnd:
		for _, k := range n.Kids {
			if !k.Eval(attrs) {
				return false
			}
		}
		return true
	case NodeOr:
		for _, k := range n.Kids {
			if k.Eval(attrs) {
				return true
			}
		}
		return false
	case NodeNot:
		return !n.Kids[0].Eval(attrs)
	default:
		ok, _ := evalOp(n.Op, attrs[n.Key], n.Values)
		return ok
	}
}

// CompileScope compiles the grants into a predicate over resources of kind
// directly under projectID (resource "project/<projectID>/<kind>/<id>") that
// holds exactly when Evaluate would allow action on them: default deny, any
// matching Deny wins, otherwise any matching Allow allows.
//
// Conditions on principal.* are decided now. A condition on an attribute of
// another resource kind sees no such attribute, as Evaluate does: positive
// operators fail and negated ones hold. A statement with an operator this
// package does not implement never grants and, as a Deny, always applies.
// schema classifies the keys; one it does not know is treated as absent.
func CompileScope(grants []Grant, p Principal, action, projectID, kind string, schema *AttributeSchema) *Node {
	principalAttrs := map[string][]string{
		"principal.id":   {p.ID},
		"principal.type": {p.Type},
	}
	var allows, denies []*Node
	for _, g := range grants {
		if g.Policy == nil || (g.ProjectID != "" && g.ProjectID != projectID) {
			continue
		}
		for _, st := range g.Policy.Statements {
			if !anyMatch(st.Actions, action, MatchAction) {
				continue
			}
			ids := statementIDs(st.Resources, projectID, kind)
			if ids.Kind == NodeFalse {
				continue
			}
			cond, known := compileConditions(st.Conditions, principalAttrs, kind, schema)
			if !known {
				if st.Effect == EffectDeny {
					denies = append(denies, ids)
				}
				continue
			}
			node := And(ids, cond)
			switch st.Effect {
			case EffectDeny:
				denies = append(denies, node)
			case EffectAllow:
				allows = append(allows, node)
			}
		}
	}
	return And(Or(allows...), Not(Or(denies...)))
}

// statementIDs returns the predicate over resource.id that the statement's
// resource patterns impose on "project/<projectID>/<kind>/<id>".
func statementIDs(patterns []string, projectID, kind string) *Node {
	var literal []string
	for _, pat := range patterns {
		all, id, ok := patternID(pat, projectID, kind)
		switch {
		case !ok:
		case all:
			return nodeTrue
		default:
			literal = append(literal, id)
		}
	}
	if len(literal) == 0 {
		return nodeFalse
	}
	return &Node{Kind: NodeCond, Key: "resource.id", Op: "In", Values: literal}
}

// patternID decides whether a resource pattern can match a resource of kind
// under projectID: all means every id matches; otherwise id is the one id.
func patternID(pattern, projectID, kind string) (all bool, id string, ok bool) {
	if pattern == "*" {
		return true, "", true
	}
	want := [3]string{"project", projectID, kind}
	segs := strings.Split(pattern, "/")
	for i, s := range segs {
		if s == "*" && i == len(segs)-1 {
			return true, "", true // a trailing "*" spans the rest, including zero segments
		}
		switch {
		case i < 3:
			if s != "*" && s != want[i] {
				return false, "", false
			}
		case i == 3:
			// "<kind>/<id>/*" also matches "<kind>/<id>": a trailing "*" spans
			// zero segments, so it names the same single resource.
			if len(segs) != 4 && (len(segs) != 5 || segs[4] != "*") {
				return false, "", false // deeper than a direct child
			}
			if s == "*" {
				return true, "", true
			}
			return false, s, true
		}
	}
	return false, "", false // shorter than a direct child and no trailing "*"
}

func compileConditions(c Conditions, principalAttrs map[string][]string, kind string, schema *AttributeSchema) (*Node, bool) {
	for op := range c {
		if !knownOperator(op) {
			return nil, false
		}
	}
	var parts []*Node
	for op, kv := range c {
		for key, want := range kv {
			if vals, ok := principalAttrs[key]; ok {
				if ok, _ := evalOp(op, vals, want); !ok {
					return nodeFalse, true
				}
				continue
			}
			if key != "resource.id" {
				def, ok := schema.Lookup(key)
				if !ok || def.ResourceKind != kind {
					// Another kind's attribute is absent here.
					if ok, _ := evalOp(op, nil, want); !ok {
						return nodeFalse, true
					}
					continue
				}
			}
			parts = append(parts, &Node{Kind: NodeCond, Key: key, Op: op, Values: want})
		}
	}
	return And(parts...), true
}

type scopeCtxKey string

// WithScope returns a context carrying the scope a repository must apply to
// list queries of kind. Handlers attach it from Authorizer.ListScope; the
// repository applies it inside the query, before pagination.
func WithScope(ctx context.Context, kind string, n *Node) context.Context {
	return context.WithValue(ctx, scopeCtxKey(kind), n)
}

// ScopeFrom returns the scope attached for kind. ok is false when none was
// attached, which callers outside a request (workers, internal services)
// treat as unrestricted.
func ScopeFrom(ctx context.Context, kind string) (n *Node, ok bool) {
	n, ok = ctx.Value(scopeCtxKey(kind)).(*Node)
	return n, ok
}

// Restricted reports whether ctx carries a scope for kind that does not allow
// everything. Services that cache list results by project must bypass the
// cache then, or one caller's filtered list would be served to another.
func Restricted(ctx context.Context, kind string) bool {
	n, ok := ScopeFrom(ctx, kind)
	return ok && !n.Unrestricted()
}

type projectScopesKey string

// WithProjectScopes attaches one scope per project for a list that spans
// projects (a user's "my tasks"). A project missing from the map is not
// listed at all.
func WithProjectScopes(ctx context.Context, kind string, byProject map[string]*Node) context.Context {
	return context.WithValue(ctx, projectScopesKey(kind), byProject)
}

// ProjectScopesFrom returns the per-project scopes attached for kind; ok is
// false when none were attached (unrestricted).
func ProjectScopesFrom(ctx context.Context, kind string) (map[string]*Node, bool) {
	m, ok := ctx.Value(projectScopesKey(kind)).(map[string]*Node)
	return m, ok
}

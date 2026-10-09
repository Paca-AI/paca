package iam

import (
	"fmt"
	"sort"
	"strings"
)

// ConditionOperators is the single list of supported operators; knownOperator
// in eval.go derives from it and a test checks evalOp implements each name.
var ConditionOperators = []string{"StringEquals", "StringNotEquals", "StringLike", "In", "NotIn", "Bool"}

const maxStatements = 100

var resourceRoots = map[string]struct{}{
	"project": {}, "user": {}, "role": {}, "plugin": {}, "agent": {}, "settings": {}, "sso": {},
}

// Issue is one validation problem; Path locates it (e.g.
// "statements[0].actions[1]") so editors can map it to a line.
type Issue struct{ Path, Message string }

// Validate checks p against r and returns every problem found (nil/empty
// means valid). A nil policy yields one "policy is required" issue; a nil
// registry makes every action unknown (fail closed). Condition keys must be
// declared in schema (a nil schema makes every key unknown) and a key bound to
// a resource kind must fit at least one of the statement's resources.
func Validate(p *Policy, r *Registry, schema *AttributeSchema) []Issue {
	if p == nil {
		return []Issue{{Path: "policy", Message: "policy is required"}}
	}
	var out []Issue
	add := func(path, format string, a ...any) {
		out = append(out, Issue{Path: path, Message: fmt.Sprintf(format, a...)})
	}
	if len(p.Statements) > maxStatements {
		add("statements", "too many statements (%d); the maximum is %d", len(p.Statements), maxStatements)
	}
	for i, s := range p.Statements {
		base := fmt.Sprintf("statements[%d]", i)
		if s.Effect != EffectAllow && s.Effect != EffectDeny {
			add(base+".effect", "effect must be %q or %q", EffectAllow, EffectDeny)
		}
		if len(s.Actions) == 0 {
			add(base+".actions", "at least one action is required")
		}
		for j, a := range s.Actions {
			if r == nil || !r.HasAction(a) {
				add(fmt.Sprintf("%s.actions[%d]", base, j), "unknown action %q", a)
			}
		}
		if len(s.Resources) == 0 {
			add(base+".resources", "at least one resource is required")
		}
		for j, res := range s.Resources {
			if msg := checkResource(res); msg != "" {
				add(fmt.Sprintf("%s.resources[%d]", base, j), "%s", msg)
			}
		}
		validateConditions(s.Conditions, s.Resources, schema, base+".conditions", add)
	}
	return out
}

func checkResource(res string) string {
	if res == "*" {
		return ""
	}
	segs := strings.Split(res, "/")
	for _, seg := range segs {
		if seg == "" {
			return fmt.Sprintf("invalid resource %q: empty segment", res)
		}
	}
	if _, ok := resourceRoots[segs[0]]; !ok {
		return fmt.Sprintf("invalid resource %q: unknown root %q", res, segs[0])
	}
	return ""
}

func validateConditions(c Conditions, resources []string, schema *AttributeSchema, base string, add func(path, format string, a ...any)) {
	ops := make([]string, 0, len(c))
	for op := range c {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	for _, op := range ops {
		known := knownOperator(op)
		keys := make([]string, 0, len(c[op]))
		for k := range c[op] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) == 0 && !known {
			add(base+"."+op, "unknown operator %q", op)
		}
		for _, k := range keys {
			path := base + "." + op + "." + k
			if !known {
				add(path, "unknown operator %q", op)
			}
			if def, ok := lookupAttr(schema, k); !ok {
				add(path, "unknown condition key %q", k)
			} else if def.ResourceKind != "" && !kindFitsResources(def.ResourceKind, resources) {
				add(path, "condition key %q applies to %q resources, which this statement's resources do not cover", k, def.ResourceKind)
			}
			vals := c[op][k]
			if len(vals) == 0 {
				add(path, "operand must not be empty")
			}
			if op == "Bool" {
				for _, v := range vals {
					if v != "true" && v != "false" {
						add(path, "Bool operand must be true or false, got %q", v)
					}
				}
			}
		}
	}
}

func lookupAttr(schema *AttributeSchema, key string) (AttributeDef, bool) {
	if schema == nil {
		return AttributeDef{}, false
	}
	return schema.Lookup(key)
}

// kindFitsResources reports whether kind is the kind of at least one resource
// pattern. A pattern of unconstrained kind ("*", "project/*", "project/p/*")
// fits any kind.
func kindFitsResources(kind string, resources []string) bool {
	for _, res := range resources {
		if k := ResourceKindOf(res); k == "" || k == kind {
			return true
		}
	}
	return false
}

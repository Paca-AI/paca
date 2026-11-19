package iam

import (
	"slices"
	"strings"
)

// Grant is one role attachment's policy. ProjectID == "" means platform-wide;
// otherwise the policy only ever applies to resources inside that project.
type Grant struct {
	RoleID    string
	ProjectID string
	Policy    *Policy
}

// Request is one authorization question. Attrs holds the condition-key
// values (principal.*, resource.*, typed keys); an attribute may be
// multi-valued, and an absent key or empty list means "no such attribute".
type Request struct {
	Action   string
	Resource string
	Attrs    map[string][]string
}

// MatchedStatement identifies a statement that matched a request.
type MatchedStatement struct {
	RoleID string
	Sid    string
	Effect Effect
	Index  int
}

// Result is the outcome of Evaluate.
type Result struct {
	Allowed bool
	Matched []MatchedStatement
}

func inProject(projectID, resource string) bool {
	p := "project/" + projectID
	return resource == p || strings.HasPrefix(resource, p+"/")
}

// Evaluate applies default-deny, explicit-Deny-wins evaluation.
func Evaluate(grants []Grant, req Request) Result {
	var res Result
	allow, deny := false, false
	for _, g := range grants {
		if g.Policy == nil {
			continue
		}
		if g.ProjectID != "" && !inProject(g.ProjectID, req.Resource) {
			continue
		}
		for i, st := range g.Policy.Statements {
			if !anyMatch(st.Actions, req.Action, MatchAction) || !anyMatch(st.Resources, req.Resource, MatchResource) {
				continue
			}
			holds, known := conditionsHold(st.Conditions, req.Attrs)
			if !known {
				// Unknown operator: fail closed — never grants, always denies.
				if st.Effect == EffectDeny {
					deny = true
					res.Matched = append(res.Matched, MatchedStatement{RoleID: g.RoleID, Sid: st.Sid, Effect: st.Effect, Index: i})
				}
				continue
			}
			if !holds {
				continue
			}
			res.Matched = append(res.Matched, MatchedStatement{RoleID: g.RoleID, Sid: st.Sid, Effect: st.Effect, Index: i})
			switch st.Effect {
			case EffectDeny:
				deny = true
			case EffectAllow:
				allow = true
			}
		}
	}
	res.Allowed = allow && !deny
	return res
}

func anyMatch(patterns []string, v string, m func(p, v string) bool) bool {
	for _, p := range patterns {
		if m(p, v) {
			return true
		}
	}
	return false
}

// knownOperator reports whether op is a supported condition operator.
// ConditionOperators (validate.go) is the single list of names; evalOp's
// switch is the implementation and a test ensures every listed name is handled.
func knownOperator(op string) bool {
	return slices.Contains(ConditionOperators, op)
}

// conditionsHold returns (holds, known). known is false when a statement uses
// an operator this evaluator does not implement. Every operator name is
// checked first (even with an empty key map) so the result never depends on
// map iteration order.
func conditionsHold(c Conditions, attrs map[string][]string) (bool, bool) {
	for op := range c {
		if !knownOperator(op) {
			return false, false
		}
	}
	for op, kv := range c {
		for key, want := range kv {
			if ok, _ := evalOp(op, attrs[key], ExpandVars(want, attrs)); !ok {
				return false, true
			}
		}
	}
	return true, true
}

// evalOp evaluates one operator against an attribute's values. An empty
// (or nil) got means the attribute is absent. Positive operators hold if ANY
// value satisfies; negated operators hold only if NO value is in want.
func evalOp(op string, got []string, want ValueList) (ok, known bool) {
	switch op {
	case "StringEquals", "In":
		return anyValue(got, func(v string) bool { return slices.Contains(want, v) }), true
	case "StringNotEquals", "NotIn":
		return !anyValue(got, func(v string) bool { return slices.Contains(want, v) }), true
	case "StringLike":
		return anyValue(got, func(v string) bool {
			for _, w := range want {
				if globMatch(w, v) {
					return true
				}
			}
			return false
		}), true
	case "Bool":
		return len(want) == 1 && anyValue(got, func(v string) bool { return v == want[0] }), true
	default:
		return false, false
	}
}

func anyValue(vals []string, pred func(string) bool) bool {
	for _, v := range vals {
		if pred(v) {
			return true
		}
	}
	return false
}

// ReferencedConditionKeys returns every condition key the grants mention, so a
// caller can load exactly those attributes (lazily) before evaluating.
func ReferencedConditionKeys(grants []Grant) map[string]struct{} {
	keys := map[string]struct{}{}
	for _, g := range grants {
		if g.Policy == nil {
			continue
		}
		for _, st := range g.Policy.Statements {
			for _, kv := range st.Conditions {
				for k := range kv {
					keys[k] = struct{}{}
				}
			}
		}
	}
	return keys
}

// varPrefix and varSuffix delimit a policy variable operand: "${principal.id}".
const (
	varPrefix = "${"
	varSuffix = "}"
)

// VariableKey returns the attribute key of a whole-operand variable such as
// "${principal.id}" and true; any other operand yields "", false.
func VariableKey(operand string) (string, bool) {
	if !strings.HasPrefix(operand, varPrefix) || !strings.HasSuffix(operand, varSuffix) {
		return "", false
	}
	return operand[len(varPrefix) : len(operand)-len(varSuffix)], true
}

// IsPrincipalVariable reports whether operand is a variable over a principal.*
// attribute, the only kind of variable the engine accepts.
func IsPrincipalVariable(operand string) bool {
	k, ok := VariableKey(operand)
	return ok && strings.HasPrefix(k, "principal.") && validKeyPart(strings.TrimPrefix(k, "principal."))
}

// ExpandVars replaces each "${principal.<name>}" operand with the request's
// values for that key. An absent attribute expands to nothing, so a positive
// operator cannot match it (fail closed). Operands that are not principal
// variables are kept as literals. The input is never modified.
func ExpandVars(want ValueList, attrs map[string][]string) ValueList {
	has := false
	for _, w := range want {
		if IsPrincipalVariable(w) {
			has = true
			break
		}
	}
	if !has {
		return want
	}
	out := make(ValueList, 0, len(want))
	for _, w := range want {
		if IsPrincipalVariable(w) {
			k, _ := VariableKey(w)
			out = append(out, attrs[k]...)
			continue
		}
		out = append(out, w)
	}
	return out
}

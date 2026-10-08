package iam

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ValueType is the declared type of an attribute's values.
type ValueType string

const (
	// TypeString declares a string-valued attribute.
	TypeString ValueType = "string"
	// TypeBool declares a boolean-valued attribute.
	TypeBool ValueType = "bool"
)

// AttributeDef declares one attribute conditions may reference.
type AttributeDef struct {
	Key          string // "task.sprint_id"
	ResourceKind string // "task"; "" for generic principal.*/resource.* keys
	Type         ValueType
	MultiValued  bool
	LabelKey     string // i18n key for the UI
}

// AttributeSchema is the registry of declared condition attributes. It is safe
// for concurrent use.
type AttributeSchema struct {
	mu   sync.RWMutex
	defs map[string]AttributeDef
}

// NewAttributeSchema returns a schema seeded with the built-in attributes.
func NewAttributeSchema() *AttributeSchema {
	s := &AttributeSchema{defs: map[string]AttributeDef{}}
	builtin := []struct {
		key, kind string
		multi     bool
	}{
		{"principal.id", "", false},
		{"principal.type", "", false},
		{"resource.id", "", false},
		{"task.sprint_id", "task", false},
		{"task.status_id", "task", false},
		{"task.type_id", "task", false},
		{"task.assignee_id", "task", true},
		{"view.sprint_id", "view", false},
		{"doc.folder_id", "doc", false},
		{"doc.ancestor_folder_ids", "doc", true},
		{"agent.environment_id", "agent", false},
		{"environment.type", "environment", false},
		{"conversation.environment_id", "conversation", false},
	}
	for _, b := range builtin {
		s.defs[b.key] = AttributeDef{
			Key: b.key, ResourceKind: b.kind, Type: TypeString, MultiValued: b.multi,
			LabelKey: "roles.attributes." + b.key,
		}
	}
	return s
}

// validKeyPart reports whether p is a non-empty run of [a-z0-9_].
func validKeyPart(p string) bool {
	if p == "" {
		return false
	}
	for _, r := range p {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

// Register adds def. It rejects duplicate keys, keys not of the form
// "<kind>.<name>" (lowercase letters, digits, underscore), an unknown value
// type and an empty LabelKey.
func (s *AttributeSchema) Register(def AttributeDef) error {
	parts := strings.Split(def.Key, ".")
	if len(parts) != 2 || !validKeyPart(parts[0]) || !validKeyPart(def.Key[len(parts[0])+1:]) {
		return fmt.Errorf("iam: attribute key %q must be of the form <kind>.<name>", def.Key)
	}
	if def.Type != TypeString && def.Type != TypeBool {
		return fmt.Errorf("iam: attribute %q has unknown type %q", def.Key, def.Type)
	}
	if def.LabelKey == "" {
		return fmt.Errorf("iam: attribute %q requires a label key", def.Key)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.defs[def.Key]; dup {
		return fmt.Errorf("iam: attribute %q is already registered", def.Key)
	}
	s.defs[def.Key] = def
	return nil
}

// Lookup returns the definition registered for key.
func (s *AttributeSchema) Lookup(key string) (AttributeDef, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.defs[key]
	return d, ok
}

// Defs returns every definition sorted by key.
func (s *AttributeSchema) Defs() []AttributeDef {
	s.mu.RLock()
	out := make([]AttributeDef, 0, len(s.defs))
	for _, d := range s.defs {
		out = append(out, d)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// ResourceKindOf returns the resource kind a resource name or pattern denotes,
// or "" when it is unconstrained (any kind).
//
//   - "project/<id>/<kind>/..." -> <kind>; "project/<id>" and "project" -> "project"
//   - platform roots ("user/x", "role/x", "plugin/x", "agent/x", "settings",
//     "sso") -> the root name
//   - "*" and "" -> ""
//
// For patterns the same positional rule applies, with wildcards in the kind
// position meaning "any kind" (""): a trailing "*" covers many segments, so
// "project/*" and "project/p1/*" can match every project-child kind, and a
// single-segment "*" in the kind position ("project/p/*/x") matches any kind.
// A "*" in the project-id position followed by a kind ("project/*/agent/*")
// does not widen the kind.
func ResourceKindOf(resource string) string {
	if resource == "" || resource == "*" {
		return ""
	}
	segs := strings.Split(resource, "/")
	root := segs[0]
	if root == "*" {
		return ""
	}
	if root != "project" {
		return root
	}
	switch {
	case len(segs) == 1:
		return "project"
	case len(segs) == 2:
		if segs[1] == "*" {
			return "" // project itself and every child kind
		}
		return "project"
	case segs[2] == "*":
		return ""
	default:
		return segs[2]
	}
}

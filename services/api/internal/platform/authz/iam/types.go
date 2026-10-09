// Package iam implements Paca's IAM-style policy language: JSON policy
// documents (effect, actions, resources, conditions), their validation and
// their evaluation. It is pure — no database or HTTP — so the same rules can
// be reasoned about and tested in isolation.
package iam

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// Effect is a statement's outcome when it matches.
type Effect string

const (
	// EffectAllow grants access when a statement matches.
	EffectAllow Effect = "Allow"
	// EffectDeny rejects access when a statement matches.
	EffectDeny Effect = "Deny"
)

// ValueList is a condition operand. JSON may supply a string, a bool or a
// list of strings; all normalize to a list of strings.
type ValueList []string

// UnmarshalJSON accepts a string, a bool or a list of strings.
func (v *ValueList) UnmarshalJSON(b []byte) error {
	// Reject null values
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return fmt.Errorf("iam: condition value must be a string, a bool or a list of strings")
	}

	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*v = ValueList{s}
		return nil
	}
	var bl bool
	if err := json.Unmarshal(b, &bl); err == nil {
		*v = ValueList{strconv.FormatBool(bl)}
		return nil
	}
	var l []*string
	if err := json.Unmarshal(b, &l); err != nil {
		return fmt.Errorf("iam: condition value must be a string, a bool or a list of strings")
	}
	// Check for nil elements in the list
	for _, elem := range l {
		if elem == nil {
			return fmt.Errorf("iam: condition value must be a string, a bool or a list of strings")
		}
	}
	// Convert []*string to []string
	result := make([]string, len(l))
	for i, elem := range l {
		result[i] = *elem
	}
	*v = result
	return nil
}

// Conditions maps operator -> condition key -> operand.
type Conditions map[string]map[string]ValueList

// Statement is one allow/deny rule.
type Statement struct {
	Sid        string     `json:"sid,omitempty"`
	Effect     Effect     `json:"effect"`
	Actions    []string   `json:"actions"`
	Resources  []string   `json:"resources"`
	Conditions Conditions `json:"conditions,omitempty"`
}

// Policy is a role's policy document.
type Policy struct {
	Version    string      `json:"version"`
	Statements []Statement `json:"statements"`
}

// ParsePolicy strictly decodes a policy document. Unknown fields and effects
// other than Allow/Deny are errors; a stored policy that fails to parse must
// be treated as granting nothing by callers.
func ParsePolicy(raw []byte) (*Policy, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var p Policy
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("iam: parse policy: %w", err)
	}

	// Reject top-level null document
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return nil, fmt.Errorf("iam: parse policy: top-level null is not a valid policy")
	}

	// Check for trailing data by attempting to read the next token
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("iam: parse policy: unexpected trailing data")
	}

	for i, s := range p.Statements {
		if s.Effect != EffectAllow && s.Effect != EffectDeny {
			return nil, fmt.Errorf("iam: statements[%d]: effect must be Allow or Deny", i)
		}
	}
	return &p, nil
}

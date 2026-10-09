// Package roledom defines the IAM role domain: roles carrying an IAM policy
// document, the attachments that bind them to users and agents, and the
// contracts of the role service and repository.
package roledom

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Principal types an attachment can name.
const (
	PrincipalUser  = "user"
	PrincipalAgent = "agent"
)

// Role is a named IAM policy. ProjectID is nil for a platform role and the
// owning project's id for a role defined by (and attachable only inside) one
// project.
type Role struct {
	ID          uuid.UUID
	Name        string
	Description string
	// Policy is the stored policy document, verbatim JSON.
	Policy    json.RawMessage
	ProjectID *uuid.UUID
	// IsSystem roles are managed by Paca and cannot be edited or deleted.
	IsSystem bool
	// IsDefault marks the role new users (and new global agents) start with.
	// At most one role is the default; it cannot be deleted.
	IsDefault bool
	// AttachmentCount is how many attachments reference the role (all scopes
	// for platform listings; inside the project for project listings).
	AttachmentCount int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Summary is the identifying part of a role that user, member and agent
// responses show for the roles attached to them.
type Summary struct {
	ID   uuid.UUID
	Name string
}

// Member is the principal behind a project_members row.
type Member struct {
	ID            uuid.UUID
	ProjectID     uuid.UUID
	PrincipalType string // PrincipalUser | PrincipalAgent
	PrincipalID   uuid.UUID
}

// Issue is one policy validation problem; Path locates it
// (e.g. "statements[0].actions[1]").
type Issue struct {
	Path    string
	Message string
}

// AttributeDef describes one condition attribute the policy editor may offer.
type AttributeDef struct {
	Key          string
	ResourceKind string
	Type         string
	MultiValued  bool
	LabelKey     string
}

// PrincipalRef names a user or agent in a simulation.
type PrincipalRef struct {
	Type string
	ID   string
}

// Matched is a statement that matched a simulated request. RoleID is
// "policy" for a statement of the policy under test.
type Matched struct {
	RoleID string
	Sid    string
	Effect string
	Index  int
}

// SimulationResult is the outcome of a simulated authorization request.
type SimulationResult struct {
	Allowed bool
	Matched []Matched
}

// PolicyIsProjectTemplate reports whether a stored policy is a project-role
// template: it has statements and every resource it names has a wildcard in
// the project position ("project/*", "project/*/task/*"). Such a role is
// written to be attached per project, where the attachment limits it to that
// project, so it may not be attached platform-wide: there it would reach every
// project. A role naming specific projects ("project/<id>/*") is not a
// template; platform-wide it reaches exactly those projects.
func PolicyIsProjectTemplate(raw json.RawMessage) bool {
	var doc struct {
		Statements []struct {
			Resources []string `json:"resources"`
		} `json:"statements"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc.Statements) == 0 {
		return false
	}
	for _, st := range doc.Statements {
		if len(st.Resources) == 0 {
			return false
		}
		for _, res := range st.Resources {
			if res != "project/*" && !strings.HasPrefix(res, "project/*/") {
				return false
			}
		}
	}
	return true
}

// IsProjectTemplate reports whether the role is a project-role template that
// no project owns (see PolicyIsProjectTemplate).
func (r *Role) IsProjectTemplate() bool {
	return r.ProjectID == nil && PolicyIsProjectTemplate(r.Policy)
}
